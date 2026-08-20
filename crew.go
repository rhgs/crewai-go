package crewai

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"sync"
	"time"
)

// Crew groups agents and tasks and orchestrates them according to a Process.
type Crew struct {
	// Agents are the team members.
	Agents []*Agent
	// Tasks are the tasks to execute.
	Tasks []*Task
	// Stages groups tasks into stages for the Staged process. Stages run in
	// sequence; the tasks within a single stage run concurrently. When
	// Process is Staged, Stages takes precedence over Tasks.
	Stages []Stage
	// Process defines the orchestration strategy. Default: Sequential.
	Process Process

	// Verbose enables detailed execution logs. When true and no logger
	// has been injected via WithLogger, the auto-created logger is set
	// to LevelDebug; otherwise LevelError (effectively silent for
	// info/debug). Ignored when a custom logger is provided via
	// WithLogger.
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

	// OutputDir, when set, jails Task.OutputFile writes for tasks that do
	// not set their own OutputDir. Same symlink-aware rules as Task.OutputDir.
	OutputDir string

	// logger is the structured logger used during Kickoff. Set via
	// WithLogger before Kickoff. NOT CONCURRENT-SAFE: must be set before
	// Kickoff starts and not mutated while Kickoff is running.
	logger *slog.Logger
	mem    *Memory
	// progress is invoked during Kickoff to surface execution events.
	// Set via WithProgress before Kickoff. NOT CONCURRENT-SAFE in the
	// same sense as logger (set once before Kickoff). The callback
	// itself MAY be called from multiple goroutines and MUST be
	// thread-safe. Panics inside the callback are recovered.
	progress ProgressFunc
}

// WithProgress registers a progress callback invoked during Kickoff.
// The callback is called from multiple goroutines when stages run in
// parallel, so it MUST be safe for concurrent use, like an
// slog.Handler. Passing nil disables progress reporting. The callback
// receives Progress values that contain metadata only (stage, task,
// agent, event, tool, duration, err) — never prompt bodies, LLM
// outputs, or tool inputs. Panics inside the callback are recovered
// and logged via slog.Default(); Kickoff is not affected.
//
// Set BEFORE Kickoff is called. Mutating this field directly while
// Kickoff is running is undefined.
func (c *Crew) WithProgress(fn ProgressFunc) *Crew {
	c.progress = fn
	return c
}

// Stage is a step of the Staged pipeline. Stages run in sequence, but the
// tasks within a single stage run concurrently. The output of each stage is
// available as context to the tasks of the following stages.
type Stage struct {
	// Name is a short identifier used in logs and observability.
	Name string
	// Tasks are the tasks that run concurrently within this stage.
	Tasks []*Task
	// Optional, when true, means a failure in this stage does not abort the
	// Kickoff: it is logged as a warning and the pipeline continues. When
	// false (the default), the first failure aborts the Kickoff.
	Optional bool
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
	// Warnings is the concatenation of every task's Warnings, in
	// execution order (declaration order for sequential, stage-then-task
	// order for staged). Includes only warnings from successful tasks
	// — failed tasks contribute no warnings because their context is
	// discarded along with their output.
	Warnings []string
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
	// Warnings holds non-fatal diagnostics recorded during this task's
	// execution (partial successes, e.g. a secondary source was down).
	// Warnings are additive to Output: the task SUCCEEDED, but a
	// downstream step was unavailable. Distinct from stage failures
	// gated by Stage.Optional. In insertion order.
	Warnings []string
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

// WithLogger injects a structured *slog.Logger used for execution logs
// during Kickoff. When not called, Kickoff creates a default text logger
// on stderr whose level depends on Verbose.
//
// The injected logger is used as-is — the caller is responsible for
// configuring its level, handler, and output.
//
// NOT CONCURRENT-SAFE: must be called before Kickoff starts and not
// mutated while Kickoff is running. Multiple calls are idempotent (last
// wins); passing nil is allowed and equivalent to not calling WithLogger
// (Kickoff will fall back to the default logger).
func (c *Crew) WithLogger(l *slog.Logger) *Crew {
	c.logger = l
	return c
}

// defaultLogger returns the fallback logger created by Kickoff when no
// logger has been injected via WithLogger. The level is LevelError when
// Verbose is false (matching the legacy stdLogger behavior, which only
// logged when verbose was true), and LevelDebug when Verbose is true.
func defaultLogger(verbose bool) *slog.Logger {
	level := slog.LevelError
	if verbose {
		level = slog.LevelDebug
	}
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: level,
	}))
}

// Kickoff executes all the crew's tasks and returns the consolidated result.
//
// inputs is an optional map of variables interpolated into {key} in the tasks'
// descriptions and expected outputs.
func (c *Crew) Kickoff(ctx context.Context, inputs map[string]string) (*CrewOutput, error) {
	if c.Process == "" {
		c.Process = Sequential
	}
	if !c.Process.valid() {
		return nil, fmt.Errorf("crewai: invalid process %q", c.Process)
	}

	// Validate the task set according to the process. In the staged process
	// the tasks live in Stages (not Tasks), so the no-tasks check is skipped
	// and replaced by a no-stages check.
	if c.Process == Staged {
		if len(c.Stages) == 0 {
			return nil, ErrNoStages
		}
	} else if len(c.Tasks) == 0 {
		return nil, ErrNoTasks
	}

	// Initialize logger if not injected.
	if c.logger == nil {
		c.logger = defaultLogger(c.Verbose)
	}
	if c.Memory {
		c.mem = NewMemory()
	}

	// Interpolate inputs into all tasks. In the staged process the tasks
	// live inside the stages.
	if c.Process == Staged {
		for i := range c.Stages {
			for _, t := range c.Stages[i].Tasks {
				t.interpolate(inputs)
			}
		}
	} else {
		for _, t := range c.Tasks {
			t.interpolate(inputs)
		}
	}

	start := time.Now()
	var err error
	out := &CrewOutput{}

	// Inject the progress callback into ctx so every executor sees it.
	ctx = ContextWithProgress(ctx, c.progress)

	switch c.Process {
	case Sequential:
		out, err = c.runSequential(ctx)
	case Hierarchical:
		out, err = c.runHierarchical(ctx)
	case Staged:
		out, err = c.runStaged(ctx)
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

		emitProgress(ctx, Progress{
			Task:  taskLabel(task, i),
			Agent: agent.Role,
			Event: "task_started",
		})

		result, facts, err := c.execute(ctx, agent, task)
		if err != nil {
			emitProgress(ctx, Progress{
				Task:  taskLabel(task, i),
				Agent: agent.Role,
				Event: "task_completed",
				Err:   redactError(err),
			})
			return nil, fmt.Errorf("task %d: %w", i+1, err)
		}
		emitProgress(ctx, Progress{
			Task:  taskLabel(task, i),
			Agent: agent.Role,
			Event: "task_completed",
		})
		warnings := task.Warnings()
		out.TasksOutput = append(out.TasksOutput, TaskOutput{
			Task:       taskLabel(task, i),
			Agent:      agent.Role,
			Output:     result,
			Facts:      facts,
			ToolTraces: task.ToolTraces(),
			Warnings:   warnings,
		})
		out.Facts = dedupFacts(out.Facts, facts)
		out.Warnings = append(out.Warnings, warnings...)
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
	c.logger.InfoContext(ctx, "manager resolved", "manager", manager.Role)

	out := &CrewOutput{}
	for i, task := range c.Tasks {
		agent := task.Agent
		if agent == nil {
			agent = c.delegate(ctx, manager, task)
		}
		if agent == nil {
			return nil, ErrNoAgent
		}
		c.logger.InfoContext(ctx, "task delegated", "task_index", i+1, "agent", agent.Role)

		emitProgress(ctx, Progress{
			Task:  taskLabel(task, i),
			Agent: agent.Role,
			Event: "task_started",
		})

		result, facts, err := c.execute(ctx, agent, task)
		if err != nil {
			emitProgress(ctx, Progress{
				Task:  taskLabel(task, i),
				Agent: agent.Role,
				Event: "task_completed",
				Err:   redactError(err),
			})
			return nil, fmt.Errorf("task %d: %w", i+1, err)
		}
		emitProgress(ctx, Progress{
			Task:  taskLabel(task, i),
			Agent: agent.Role,
			Event: "task_completed",
		})
		warnings := task.Warnings()
		out.TasksOutput = append(out.TasksOutput, TaskOutput{
			Task:       taskLabel(task, i),
			Agent:      agent.Role,
			Output:     result,
			Facts:      facts,
			ToolTraces: task.ToolTraces(),
			Warnings:   warnings,
		})
		out.Facts = dedupFacts(out.Facts, facts)
		out.Warnings = append(out.Warnings, warnings...)
		out.Final = result
	}
	return out, nil
}

// runStaged executes the crew's stages in order, running the tasks of each
// stage concurrently. The output of each stage is available as context to the
// tasks of the following stages (via Task.Context, resolved by contextText).
func (c *Crew) runStaged(ctx context.Context) (*CrewOutput, error) {
	out := &CrewOutput{}

	for si, stage := range c.Stages {
		stageName := stage.Name
		if stageName == "" {
			stageName = fmt.Sprintf("stage %d", si+1)
		}

		emitProgress(ctx, Progress{
			Stage: stageName,
			Event: "stage_started",
		})

		// A single derived context for the whole stage: cancelling it stops
		// every sibling goroutine (on parent cancellation or a non-optional
		// failure).
		stageCtx, cancel := context.WithCancel(ctx)

		results := make([]stageResult, len(stage.Tasks))
		var wg sync.WaitGroup

		// firstErr records the chronologically first failure (and its index)
		// so a non-optional stage reports the real cause, not a sibling's
		// context.Canceled that was triggered by the cancellation.
		var (
			errMu    sync.Mutex
			firstErr error
			firstIdx int
		)
		recordErr := func(ti int, err error) {
			errMu.Lock()
			if firstErr == nil {
				firstErr = err
				firstIdx = ti
			}
			errMu.Unlock()
			// Interrupt siblings as soon as a non-optional task fails.
			if !stage.Optional {
				cancel()
			}
		}

		for ti, task := range stage.Tasks {
			agent := task.Agent
			if agent == nil {
				agent = c.agentForIndex(ti)
			}
			if agent == nil {
				cancel()
				wg.Wait()
				return nil, fmt.Errorf("stage %q: task %d: %w", stageName, ti+1, ErrNoAgent)
			}

			wg.Add(1)
			go func(ti int, task *Task, agent *Agent) {
				defer wg.Done()
				// Recover from a panic in the task goroutine so a single
				// panicking task cannot crash the whole process.
				defer func() {
					if r := recover(); r != nil {
						err := fmt.Errorf("panic: %v", r)
						results[ti] = stageResult{err: err}
						recordErr(ti, err)
					}
				}()

				c.logger.InfoContext(stageCtx, "task started",
					"stage", stageName, "task_index", ti+1, "agent", agent.Role)

				emitProgress(stageCtx, Progress{
					Stage: stageName,
					Task:  taskLabel(task, ti),
					Agent: agent.Role,
					Event: "task_started",
				})

				result, facts, err := c.execute(stageCtx, agent, task)
				results[ti] = stageResult{
					task:  task,
					agent: agent,
					out:   result,
					facts: facts,
					err:   err,
				}
				emitProgress(stageCtx, Progress{
					Stage: stageName,
					Task:  taskLabel(task, ti),
					Agent: agent.Role,
					Event: "task_completed",
					Err:   redactError(err),
				})
				if err != nil {
					recordErr(ti, err)
				}
			}(ti, task, agent)
		}
		wg.Wait()
		cancel()

		emitProgress(ctx, Progress{
			Stage: stageName,
			Event: "stage_completed",
		})

		// A non-optional stage aborts on the first failure.
		if firstErr != nil && !stage.Optional {
			return nil, fmt.Errorf("stage %q: task %d: %w", stageName, firstIdx+1, firstErr)
		}

		// Aggregate results in declaration order (not completion order).
		for ti, res := range results {
			if res.err != nil {
				// Only reachable for optional stages (non-optional already
				// returned above).
				c.logger.WarnContext(ctx, "task failed in optional stage",
					"stage", stageName, "task_index", ti+1, "error", redactError(res.err))
				continue
			}
			warnings := res.task.Warnings()
			out.TasksOutput = append(out.TasksOutput, TaskOutput{
				Task:       taskLabel(res.task, ti),
				Agent:      res.agent.Role,
				Output:     res.out,
				Facts:      res.facts,
				ToolTraces: res.task.ToolTraces(),
				Warnings:   warnings,
			})
			out.Facts = dedupFacts(out.Facts, res.facts)
			out.Warnings = append(out.Warnings, warnings...)
			out.Final = res.out
		}
	}

	return out, nil
}

// stageResult is the outcome of a single task within a stage, collected by
// index so the final output preserves declaration order.
type stageResult struct {
	task  *Task
	agent *Agent
	out   string
	facts []Fact
	err   error
}

// execute runs a task, assembles the context, and persists the output/memory.
// It returns the task result string, collected facts, and an error.
//
// If a task is supplied, it is attached to ctx as a WarningSink so tools
// (including adapters from mcp/, tools/, etc.) can record non-fatal
// diagnostics via AddWarningFromCtx or by retrieving the sink from ctx
// themselves.
func (c *Crew) execute(ctx context.Context, agent *Agent, task *Task) (string, []Fact, error) {
	if task != nil {
		ctx = ContextWithWarningSink(ctx, task)
	}
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
	jail := task.OutputDir
	if jail == "" {
		jail = c.OutputDir
	}
	if err := task.setOutputWithJail(result, jail); err != nil {
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
		c.logger.WarnContext(ctx, "delegation failed, using first agent", "error", redactError(err))
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
