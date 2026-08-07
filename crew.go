package crewai

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// Crew groups agents and tasks and orchestrates them according to a Process.
type Crew struct {
	// Agents are the team members.
	Agents []*Agent
	// Tasks are the tasks to execute.
	Tasks []*Task
	// Process defines the orchestration strategy. Default: Sequential.
	Process Process

	// Verbose enables detailed execution logs.
	Verbose bool

	// Memory, when enabled, stores task outputs for later reference.
	Memory bool

	// ManagerLLM is the model used by the manager agent in the hierarchical
	// process.
	ManagerLLM LLM
	// ManagerAgent is an explicit manager agent for the hierarchical process.
	// When set, it takes precedence over ManagerLLM.
	ManagerAgent *Agent

	// Guardrails, when set, are run after all tasks complete successfully,
	// in order, against the full CrewOutput. If any guardrail returns a
	// non-nil error, Kickoff returns ErrBlockedByGuardrail.
	Guardrails []Guardrail

	logger Logger
	mem    *Memory
}

// CrewOutput is the result of a crew's execution.
type CrewOutput struct {
	// Final is the output of the last executed task.
	Final string
	// TasksOutput contains the output of each task, in execution order.
	TasksOutput []TaskOutput
	// Duration is the total execution time.
	Duration time.Duration
	// Facts holds all facts collected across all tasks from FactSource
	// tools, deduplicated by PayloadHash. Populated by the executor,
	// never by the LLM.
	Facts []Fact
}

// TaskOutput is the output of a single task.
type TaskOutput struct {
	Task   string
	Agent  string
	Output string
	// Facts holds the facts collected from FactSource tools during this
	// task's execution. Populated by the executor, never by the LLM.
	Facts []Fact
	// ToolTraces holds the native tool call traces (empty when using ReAct).
	// Each entry is a tool invocation: name, arguments, result, duration.
	ToolTraces []ToolTrace
}

// String returns the crew's final output.
func (o *CrewOutput) String() string { return o.Final }

// NewCrew creates a sequential crew with the given agents and tasks.
func NewCrew(agents []*Agent, tasks []*Task) *Crew {
	return &Crew{
		Agents:  agents,
		Tasks:   tasks,
		Process: Sequential,
	}
}

// Kickoff executes all the crew's tasks and returns the consolidated result.
//
// inputs is an optional map of variables interpolated into {key} in the tasks'
// descriptions and expected outputs.
func (c *Crew) Kickoff(ctx context.Context, inputs map[string]string) (*CrewOutput, error) {
	if len(c.Tasks) == 0 {
		return nil, ErrNoTasks
	}
	if c.Process == "" {
		c.Process = Sequential
	}
	if !c.Process.valid() {
		return nil, fmt.Errorf("crewai: invalid process %q", c.Process)
	}

	c.logger = newStdLogger(c.Verbose)
	if c.Memory {
		c.mem = NewMemory()
	}

	// Interpolate inputs into all tasks.
	for _, t := range c.Tasks {
		t.interpolate(inputs)
	}

	start := time.Now()
	var err error
	out := &CrewOutput{}

	switch c.Process {
	case Sequential:
		out, err = c.runSequential(ctx)
	case Hierarchical:
		out, err = c.runHierarchical(ctx)
	}
	if err != nil {
		return nil, err
	}
	out.Duration = time.Since(start)

	// Run crew-level guardrails after all tasks complete.
	if err := runCrewGuardrails(ctx, c.Guardrails, out); err != nil {
		return nil, err
	}

	return out, nil
}

// runSequential executes the tasks in order.
func (c *Crew) runSequential(ctx context.Context) (*CrewOutput, error) {
	out := &CrewOutput{}
	for i, task := range c.Tasks {
		agent := task.Agent
		if agent == nil {
			agent = c.agentForIndex(i)
		}
		if agent == nil {
			return nil, ErrNoAgent
		}

		result, facts, err := c.execute(ctx, agent, task)
		if err != nil {
			return nil, fmt.Errorf("task %d: %w", i+1, err)
		}
		out.TasksOutput = append(out.TasksOutput, TaskOutput{
			Task:       taskLabel(task, i),
			Agent:      agent.Role,
			Output:     result,
			Facts:      facts,
			ToolTraces: task.ToolTraces(),
		})
		out.Facts = dedupFacts(out.Facts, facts)
		out.Final = result
	}
	return out, nil
}

// runHierarchical uses a manager to assign each task to the best agent.
func (c *Crew) runHierarchical(ctx context.Context) (*CrewOutput, error) {
	manager, err := c.resolveManager()
	if err != nil {
		return nil, err
	}
	c.logger.Infof("manager: %s", manager.Role)

	out := &CrewOutput{}
	for i, task := range c.Tasks {
		agent := task.Agent
		if agent == nil {
			agent = c.delegate(ctx, manager, task)
		}
		if agent == nil {
			return nil, ErrNoAgent
		}
		c.logger.Infof("manager delegated task %d to %q", i+1, agent.Role)

		result, facts, err := c.execute(ctx, agent, task)
		if err != nil {
			return nil, fmt.Errorf("task %d: %w", i+1, err)
		}
		out.TasksOutput = append(out.TasksOutput, TaskOutput{
			Task:       taskLabel(task, i),
			Agent:      agent.Role,
			Output:     result,
			Facts:      facts,
			ToolTraces: task.ToolTraces(),
		})
		out.Facts = dedupFacts(out.Facts, facts)
		out.Final = result
	}
	return out, nil
}

// execute runs a task, assembles the context, and persists the output/memory.
// It returns the task result string, collected facts, and an error.
func (c *Crew) execute(ctx context.Context, agent *Agent, task *Task) (string, []Fact, error) {
	contextText := task.contextText()
	if c.mem != nil && contextText == "" {
		// With no explicit context, inject the accumulated memory.
		if mem := strings.TrimSpace(c.mem.String()); mem != "" {
			contextText = mem
		}
	}

	result, facts, err := executeTask(ctx, agent, task, contextText, c.logger)
	if err != nil {
		return "", nil, err
	}
	if err := task.setOutput(result); err != nil {
		return "", nil, fmt.Errorf("writing task output: %w", err)
	}
	if c.mem != nil {
		c.mem.Save(MemoryRecord{
			Agent:   agent.Role,
			Task:    task.Name,
			Content: result,
		})
	}

	// Run task-level guardrail (if any) after the output is stored.
	label := task.Name
	if label == "" {
		label = "task"
	}
	if err := runTaskGuardrail(ctx, task, label, result); err != nil {
		return "", nil, err
	}

	return result, facts, nil
}

// resolveManager returns the manager agent for the hierarchical process.
func (c *Crew) resolveManager() (*Agent, error) {
	if c.ManagerAgent != nil {
		return c.ManagerAgent, nil
	}
	if c.ManagerLLM != nil {
		return &Agent{
			Role:      "Team Manager",
			Goal:      "Coordinate the team to complete the tasks with excellence.",
			Backstory: "You are an experienced manager, skilled at delegating work to the most suitable team member.",
			LLM:       c.ManagerLLM,
		}, nil
	}
	return nil, ErrNoManager
}

// delegate asks the manager to choose the best agent for the task.
func (c *Crew) delegate(ctx context.Context, manager *Agent, task *Task) *Agent {
	if len(c.Agents) == 0 {
		return nil
	}
	if len(c.Agents) == 1 {
		return c.Agents[0]
	}

	var b strings.Builder
	b.WriteString("You must choose which team member is the most suitable for the following task.\n\n")
	b.WriteString("Available members:\n")
	for _, a := range c.Agents {
		fmt.Fprintf(&b, "- %s: %s\n", a.Role, a.Goal)
	}
	fmt.Fprintf(&b, "\nTask: %s\n", task.Description)
	b.WriteString("\nRespond ONLY with the exact role of the chosen member, with no other text.")

	resp, err := manager.LLM.Call(ctx, []Message{
		SystemMessage(buildSystemPrompt(manager)),
		UserMessage(b.String()),
	})
	if err != nil {
		c.logger.Infof("delegation failed, using first agent: %v", err)
		return c.Agents[0]
	}

	resp = strings.TrimSpace(resp)
	// Exact match, and on failure, substring match.
	for _, a := range c.Agents {
		if strings.EqualFold(strings.TrimSpace(resp), a.Role) {
			return a
		}
	}
	for _, a := range c.Agents {
		if strings.Contains(strings.ToLower(resp), strings.ToLower(a.Role)) {
			return a
		}
	}
	return c.Agents[0]
}

// agentForIndex chooses an agent for task i when it has no agent.
func (c *Crew) agentForIndex(i int) *Agent {
	if len(c.Agents) == 0 {
		return nil
	}
	if i < len(c.Agents) {
		return c.Agents[i]
	}
	return c.Agents[len(c.Agents)-1]
}

// MemorySnapshot returns the accumulated memory (nil if memory is disabled).
func (c *Crew) MemorySnapshot() *Memory { return c.mem }

func taskLabel(t *Task, i int) string {
	if t.Name != "" {
		return t.Name
	}
	return fmt.Sprintf("Task %d", i+1)
}
