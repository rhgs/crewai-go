package crewai

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
)

// MaxCrewConfigBytes caps LoadCrew / LoadCrewFile input (D-Y9). Default 1 MiB.
const MaxCrewConfigBytes = 1 << 20

// CrewConfig is the declarative JSON-subset document (D-Y1, D-Y7).
// Secrets are never interpolated from the file (D-Y8).
type CrewConfig struct {
	Agents []AgentConfig   `json:"agents"`
	Tasks  []TaskConfig    `json:"tasks"`
	Crew   CrewMetaConfig  `json:"crew"`
	raw    json.RawMessage // original bytes for schema validation
}

// AgentConfig is the documented subset of Agent (D-Y7).
type AgentConfig struct {
	Name            string   `json:"name,omitempty"`
	Role            string   `json:"role"`
	Goal            string   `json:"goal,omitempty"`
	Backstory       string   `json:"backstory,omitempty"`
	LLM             string   `json:"llm,omitempty"`
	Tools           []string `json:"tools,omitempty"`
	MaxIterations   int      `json:"max_iterations,omitempty"`
	AllowDelegation bool     `json:"allow_delegation,omitempty"`
	ToolMode        string   `json:"tool_mode,omitempty"`
}

// TaskConfig is the documented subset of Task (D-Y7).
type TaskConfig struct {
	Name           string       `json:"name,omitempty"`
	Description    string       `json:"description"`
	ExpectedOutput string       `json:"expected_output,omitempty"`
	Agent          string       `json:"agent,omitempty"`
	Tools          []string     `json:"tools,omitempty"`
	Context        []ContextRef `json:"context,omitempty"`
	Async          bool         `json:"async,omitempty"`
	OutputFile     string       `json:"output_file,omitempty"`
	Guardrail      string       `json:"guardrail,omitempty"`
}

// CrewMetaConfig is the documented subset of Crew (D-Y7). Stages are out of
// v1 (use Process sequential/hierarchical).
type CrewMetaConfig struct {
	Name                 string   `json:"name,omitempty"`
	Process              string   `json:"process,omitempty"`
	Verbose              bool     `json:"verbose,omitempty"`
	Memory               bool     `json:"memory,omitempty"`
	ManagerLLM           string   `json:"manager_llm,omitempty"`
	ManagerAgent         string   `json:"manager_agent,omitempty"`
	OutputDir            string   `json:"output_dir,omitempty"`
	EnableDelegationTool bool     `json:"enable_delegation_tool,omitempty"`
	AsyncMaxWorkers      *int     `json:"async_max_workers,omitempty"`
	AsyncFailFast        *bool    `json:"async_fail_fast,omitempty"`
	Guardrails           []string `json:"guardrails,omitempty"`
}

// ContextRef is a task Context entry: a name (D-Y6 name-first) or a 0-based
// index (fallback). JSON accepts a string or a number.
type ContextRef struct {
	Name  string
	Index *int
}

// UnmarshalJSON accepts a JSON string (name) or number (index).
func (c *ContextRef) UnmarshalJSON(b []byte) error {
	b = bytes.TrimSpace(b)
	if len(b) == 0 || string(b) == "null" {
		return fmt.Errorf("context ref: empty")
	}
	if b[0] == '"' {
		var s string
		if err := json.Unmarshal(b, &s); err != nil {
			return err
		}
		c.Name = s
		c.Index = nil
		return nil
	}
	var n json.Number
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.UseNumber()
	if err := dec.Decode(&n); err != nil {
		return fmt.Errorf("context ref: want string or number")
	}
	i, err := strconv.Atoi(string(n))
	if err != nil {
		return fmt.Errorf("context ref: %w", err)
	}
	c.Index = &i
	c.Name = ""
	return nil
}

// LoadCrew parses a JSON-subset crew document from r (D-Y2). The reader is
// capped at MaxCrewConfigBytes. Full YAML (anchors, block scalars) is not
// supported — see docs/declarative.md.
func LoadCrew(r io.Reader) (*CrewConfig, error) {
	if r == nil {
		return nil, fmt.Errorf("crewai: LoadCrew nil reader")
	}
	limited := io.LimitReader(r, int64(MaxCrewConfigBytes)+1)
	data, err := io.ReadAll(limited)
	if err != nil {
		return nil, fmt.Errorf("crewai: LoadCrew: %w", err)
	}
	if len(data) > MaxCrewConfigBytes {
		return nil, ErrCrewConfigTooLarge
	}
	return parseCrewConfig(data)
}

// LoadCrewFile reads path and calls LoadCrew (D-Y2).
func LoadCrewFile(path string) (*CrewConfig, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("crewai: LoadCrewFile: %w", err)
	}
	defer f.Close()
	return LoadCrew(f)
}

func parseCrewConfig(data []byte) (*CrewConfig, error) {
	if err := validateSchema(data, json.RawMessage(crewConfigSchema)); err != nil {
		return nil, err
	}
	var cfg CrewConfig
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&cfg); err != nil {
		return nil, &ValidationError{Path: "", Message: err.Error()}
	}
	cfg.raw = append(json.RawMessage(nil), data...)
	return &cfg, nil
}

type buildOpts struct {
	llms       map[string]LLM
	tools      map[string]Tool
	guardrails map[string]Guardrail
}

// BuildOption configures CrewConfig.Build (D-Y3).
type BuildOption func(*buildOpts)

// WithLLMMap supplies named LLM implementations. AgentConfig.LLM and
// CrewMetaConfig.ManagerLLM are looked up here. Unknown names fail closed.
func WithLLMMap(m map[string]LLM) BuildOption {
	return func(o *buildOpts) { o.llms = m }
}

// WithToolMap supplies named tools for agent/task tool lists.
func WithToolMap(m map[string]Tool) BuildOption {
	return func(o *buildOpts) { o.tools = m }
}

// WithGuardrailMap supplies named guardrails for task- and crew-level refs.
func WithGuardrailMap(m map[string]Guardrail) BuildOption {
	return func(o *buildOpts) { o.guardrails = m }
}

// Build materializes a Crew. llm/tools/guardrail are string references into
// the maps; unknown refs fail closed (D-Y5). Task.Context is name-first with
// index fallback (D-Y6). The resulting graph is checked with planWaves.
func (cfg *CrewConfig) Build(opts ...BuildOption) (*Crew, error) {
	if cfg == nil {
		return nil, fmt.Errorf("crewai: Build nil config")
	}
	o := &buildOpts{}
	for _, fn := range opts {
		fn(o)
	}
	if o.llms == nil {
		o.llms = map[string]LLM{}
	}
	if o.tools == nil {
		o.tools = map[string]Tool{}
	}
	if o.guardrails == nil {
		o.guardrails = map[string]Guardrail{}
	}

	agents, byKey, err := cfg.buildAgents(o)
	if err != nil {
		return nil, err
	}
	tasks, err := cfg.buildTasks(o, agents, byKey)
	if err != nil {
		return nil, err
	}

	crew := NewCrew(agents, tasks)
	meta := cfg.Crew
	crew.Name = meta.Name
	crew.Verbose = meta.Verbose
	crew.Memory = meta.Memory
	crew.OutputDir = meta.OutputDir
	crew.EnableDelegationTool = meta.EnableDelegationTool
	if meta.Process != "" {
		p := Process(meta.Process)
		if !p.valid() {
			return nil, &ValidationError{Path: "/crew/process", Message: "invalid process"}
		}
		if p == Staged {
			return nil, &ValidationError{Path: "/crew/process", Message: "staged is out of v1 subset"}
		}
		crew.Process = p
	}
	if meta.AsyncMaxWorkers != nil {
		crew.AsyncMaxWorkers = *meta.AsyncMaxWorkers
	}
	if meta.AsyncFailFast != nil {
		crew.AsyncFailFast = *meta.AsyncFailFast
	}
	if meta.ManagerLLM != "" {
		llm, ok := o.llms[meta.ManagerLLM]
		if !ok {
			return nil, fmt.Errorf("%w: llm %q", ErrUnknownRef, meta.ManagerLLM)
		}
		crew.ManagerLLM = llm
	}
	if meta.ManagerAgent != "" {
		a, ok := byKey[meta.ManagerAgent]
		if !ok {
			return nil, fmt.Errorf("%w: manager_agent %q", ErrUnknownRef, meta.ManagerAgent)
		}
		crew.ManagerAgent = a
	}
	for _, name := range meta.Guardrails {
		g, ok := o.guardrails[name]
		if !ok {
			return nil, fmt.Errorf("%w: guardrail %q", ErrUnknownRef, name)
		}
		crew.Guardrails = append(crew.Guardrails, g)
	}

	if _, err := planWaves(crew.Tasks); err != nil {
		return nil, err
	}
	return crew, nil
}

func (cfg *CrewConfig) buildAgents(o *buildOpts) ([]*Agent, map[string]*Agent, error) {
	agents := make([]*Agent, 0, len(cfg.Agents))
	byKey := map[string]*Agent{}
	for i, ac := range cfg.Agents {
		path := fmt.Sprintf("/agents/%d", i)
		if strings.TrimSpace(ac.Role) == "" {
			return nil, nil, &ValidationError{Path: path + "/role", Message: "required"}
		}
		var llm LLM
		if ac.LLM != "" {
			var ok bool
			llm, ok = o.llms[ac.LLM]
			if !ok {
				return nil, nil, fmt.Errorf("%w: llm %q", ErrUnknownRef, ac.LLM)
			}
		}
		a := NewAgent(ac.Role, ac.Goal, ac.Backstory, llm)
		a.MaxIterations = ac.MaxIterations
		a.AllowDelegation = ac.AllowDelegation
		if ac.ToolMode != "" {
			a.ToolMode = ToolMode(ac.ToolMode)
		}
		tools, err := resolveTools(ac.Tools, o.tools)
		if err != nil {
			return nil, nil, err
		}
		if len(tools) > 0 {
			a.WithTools(tools...)
		}
		if ac.Name != "" {
			if _, dup := byKey[ac.Name]; dup {
				return nil, nil, &ValidationError{Path: path + "/name", Message: "duplicate agent name"}
			}
			byKey[ac.Name] = a
		}
		if ac.Name == "" {
			if _, dup := byKey[ac.Role]; dup {
				return nil, nil, &ValidationError{Path: path + "/role", Message: "duplicate agent role"}
			}
			byKey[ac.Role] = a
		} else if _, exists := byKey[ac.Role]; !exists {
			byKey[ac.Role] = a
		}
		agents = append(agents, a)
	}
	return agents, byKey, nil
}

func (cfg *CrewConfig) buildTasks(o *buildOpts, agents []*Agent, byKey map[string]*Agent) ([]*Task, error) {
	tasks := make([]*Task, len(cfg.Tasks))
	byName := map[string]*Task{}
	for i, tc := range cfg.Tasks {
		path := fmt.Sprintf("/tasks/%d", i)
		if strings.TrimSpace(tc.Description) == "" {
			return nil, &ValidationError{Path: path + "/description", Message: "required"}
		}
		var agent *Agent
		if tc.Agent != "" {
			var ok bool
			agent, ok = byKey[tc.Agent]
			if !ok {
				return nil, fmt.Errorf("%w: agent %q", ErrUnknownRef, tc.Agent)
			}
		} else if len(agents) > 0 {
			agent = agents[0]
		}
		t := NewTask(tc.Description, tc.ExpectedOutput, agent)
		t.Name = tc.Name
		t.Async = tc.Async
		t.OutputFile = tc.OutputFile
		if tc.Guardrail != "" {
			g, ok := o.guardrails[tc.Guardrail]
			if !ok {
				return nil, fmt.Errorf("%w: guardrail %q", ErrUnknownRef, tc.Guardrail)
			}
			t.Guardrail = g
		}
		tools, err := resolveTools(tc.Tools, o.tools)
		if err != nil {
			return nil, err
		}
		if len(tools) > 0 {
			t.Tools = tools
		}
		if tc.Name != "" {
			if _, dup := byName[tc.Name]; dup {
				return nil, &ValidationError{Path: path + "/name", Message: "duplicate task name"}
			}
			byName[tc.Name] = t
		}
		tasks[i] = t
	}
	for i, tc := range cfg.Tasks {
		t := tasks[i]
		for _, ref := range tc.Context {
			dep, err := resolveContext(ref, tasks, byName)
			if err != nil {
				return nil, err
			}
			t.Context = append(t.Context, dep)
		}
	}
	return tasks, nil
}

func resolveTools(names []string, m map[string]Tool) ([]Tool, error) {
	if len(names) == 0 {
		return nil, nil
	}
	out := make([]Tool, 0, len(names))
	for _, n := range names {
		t, ok := m[n]
		if !ok {
			return nil, fmt.Errorf("%w: tool %q", ErrUnknownRef, n)
		}
		out = append(out, t)
	}
	return out, nil
}

func resolveContext(ref ContextRef, tasks []*Task, byName map[string]*Task) (*Task, error) {
	if ref.Name != "" {
		if t, ok := byName[ref.Name]; ok {
			return t, nil
		}
		// Numeric string as index fallback (D-Y6).
		if i, err := strconv.Atoi(ref.Name); err == nil {
			if i < 0 || i >= len(tasks) {
				return nil, fmt.Errorf("%w: context index %d", ErrUnknownRef, i)
			}
			return tasks[i], nil
		}
		return nil, fmt.Errorf("%w: context %q", ErrUnknownRef, ref.Name)
	}
	if ref.Index != nil {
		i := *ref.Index
		if i < 0 || i >= len(tasks) {
			return nil, fmt.Errorf("%w: context index %d", ErrUnknownRef, i)
		}
		return tasks[i], nil
	}
	return nil, fmt.Errorf("%w: empty context ref", ErrUnknownRef)
}
