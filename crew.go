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

	// Name is an optional human-readable identifier for the crew. When set,
	// it is the default MemoryPolicy.Scope for this Kickoff (G3).
	Name string

	// Memory, when enabled, ensures an in-memory MemoryStore for this
	// Kickoff (D-M1 / G4 permanent v0.x alias). Existing short-term
	// MemorySnapshot behavior is preserved via the same *Memory.
	Memory bool

	// MemoryStore is an optional long-term memory backend. When nil and
	// Memory is true, Kickoff installs an InMemory *Memory (D-M1). When set,
	// the Crew uses it for AutoSave / inject under MemoryPolicy; the app
	// owns Close (D-M6) unless the store was created by Kickoff.
	MemoryStore MemoryStore

	// MemoryPolicy configures automatic save/inject. Nil at Kickoff means
	// NewMemoryPolicy() defaults. A literal MemoryPolicy{} is NOT those
	// defaults — use NewMemoryPolicy and override fields.
	MemoryPolicy *MemoryPolicy

	// Embed is an optional application-provided embedding function (M4).
	// When MemoryPolicy.AutoEmbed is true and Embed is non-nil, the Crew
	// embeds each AutoSave entry serially at the commit barrier (G8) —
	// never inside parallel workers. The core never bundles an embedder.
	Embed EmbeddingFunc

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

	// EnableDelegationTool, when true, auto-attaches the delegate_to_coworker
	// tool to each crew agent (and ManagerAgent, if set) at the start of
	// Kickoff, unless the agent already has a tool with that name.
	// Targets of delegation must still have AllowDelegation == true.
	// Default false (no surprise tool surface). Explicit
	// agent.WithTools(NewDelegationTool(crew)) always works regardless.
	EnableDelegationTool bool

	// AsyncMaxWorkers caps how many Async tasks may run concurrently in a
	// wave under the sequential and hierarchical processes (D-A4). The
	// default set by NewCrew and by WithAsyncMaxWorkers is
	// DefaultAsyncMaxWorkers (8). Setting it explicitly via
	// WithAsyncMaxWorkers(0) disables the cap (unlimited, bounded by the
	// ready set). Ignored under Staged (stage batches are uncapped; they
	// already own their own fan-out).
	AsyncMaxWorkers int

	// AsyncFailFast controls what happens after the first task failure in an
	// async wave (D-A3). Default true: the failure cancels the remaining
	// siblings and aborts Kickoff. When false, independent branches continue
	// and only the failed task's dependents are skipped (with an error).
	// Ignored under Staged (Stage.Optional owns that axis).
	AsyncFailFast bool

	// logger is the structured logger used during Kickoff. Set via
	// WithLogger before Kickoff. NOT CONCURRENT-SAFE: must be set before
	// Kickoff starts and not mutated while Kickoff is running.
	logger *slog.Logger
	// mem is the short-term *Memory snapshot used by MemorySnapshot and,
	// when Memory=true with no external store, also the MemoryStore.
	mem *Memory
	// store is the effective MemoryStore for this Kickoff (external or mem).
	store MemoryStore
	// policy is the resolved MemoryPolicy for this Kickoff.
	policy *MemoryPolicy
	// storeOwner is true when Kickoff created the default InMemory store;
	// the Crew may Close it at the end of Kickoff (D-M6).
	storeOwner bool

	// memBuf / buffering implement the D-M7 per-group AutoSave buffer.
	// Workers stash successful outputs; the barrier commits them in
	// declaration order. Fail-fast abort discards the buffer.
	memBufMu  sync.Mutex
	memBuf    []bufferEntry
	buffering bool

	// progress is invoked during Kickoff to surface execution events.
	// Set via WithProgress before Kickoff. NOT CONCURRENT-SAFE in the
	// same sense as logger (set once before Kickoff). The callback
	// itself MAY be called from multiple goroutines and MUST be
	// thread-safe. Panics inside the callback are recovered.
	progress ProgressFunc

	// runMu enforces a single in-flight Kickoff per Crew value. Concurrent
	// Kickoff calls return ErrCrewRunning (fail fast; they do not queue).
	runMu sync.Mutex

	// failedTasks tracks tasks that failed (or whose upstream failed under
	// AsyncFailFast=false) so dependents can be skipped (D-A3). Reset at
	// each Kickoff start. Keyed by task pointer.
	failedTasks map[*Task]error
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

// DefaultAsyncMaxWorkers is the default cap on concurrent Async tasks in a
// wave (D-A4). NewCrew uses it. Explicitly setting Crew.AsyncMaxWorkers=0
// means unlimited, not this default.
const DefaultAsyncMaxWorkers = 8

// NewCrew creates a sequential crew with the given agents and tasks. It
// applies the library defaults for async scheduling (AsyncMaxWorkers =
// DefaultAsyncMaxWorkers, AsyncFailFast = true); override them on the
// returned Crew if needed. A composite literal Crew{} (no NewCrew) has
// AsyncMaxWorkers = 0 = unlimited by design until Kickoff applies the
// default for the unset path.
func NewCrew(agents []*Agent, tasks []*Task) *Crew {
	return &Crew{
		Agents:          agents,
		Tasks:           tasks,
		Process:         Sequential,
		AsyncMaxWorkers: DefaultAsyncMaxWorkers,
		AsyncFailFast:   true,
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
//
// Only one Kickoff may run at a time on a given *Crew. A concurrent call
// returns ErrCrewRunning immediately (it does not wait). Create separate
// Crew values for parallel runs. Sequential reuse of the same Crew is OK.
func (c *Crew) Kickoff(ctx context.Context, inputs map[string]string) (*CrewOutput, error) {
	if !c.runMu.TryLock() {
		return nil, ErrCrewRunning
	}
	defer c.runMu.Unlock()

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

	// Resolve memory: Memory bool is a permanent v0.x alias (G4) that ensures
	// an InMemory store when no external MemoryStore is set (D-M1).
	c.policy = resolvePolicy(c.MemoryPolicy)
	c.storeOwner = false
	c.store = c.MemoryStore
	c.mem = nil
	c.failedTasks = make(map[*Task]error)
	c.discardMemoryBuffer()

	if c.store == nil && c.Memory {
		c.mem = NewMemory()
		c.store = c.mem
		c.storeOwner = true
	} else if mem, ok := c.store.(*Memory); ok {
		// External *Memory: keep MemorySnapshot working against it.
		c.mem = mem
	}

	// D-A4: AsyncMaxWorkers 0 is always unlimited. NewCrew sets the safe
	// default of DefaultAsyncMaxWorkers (8); a composite literal Crew{} that
	// leaves the field at 0 is unlimited by design (documented risk).

	// G12: validate the async DAG before any task runs.
	if c.Process != Staged {
		if _, err := planWaves(c.Tasks); err != nil {
			c.logger.ErrorContext(ctx, "invalid async task graph", "error", redactError(err))
			return nil, err
		}
	} else {
		// G5: Async is ignored under Staged; surface it once when set.
		for _, stage := range c.Stages {
			for _, t := range stage.Tasks {
				if t.Async {
					c.logger.WarnContext(ctx, "Task.Async is ignored under the Staged process",
						"task", taskLabel(t, 0))
					break
				}
			}
		}
	}

	if c.EnableDelegationTool {
		c.attachDelegationTools()
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
		out, err = c.runSequentialWithPlan(ctx, nil)
	case Hierarchical:
		out, err = c.runHierarchical(ctx)
	case Staged:
		out, err = c.runStaged(ctx)
	}
	if err != nil {
		c.discardMemoryBuffer()
		return nil, err
	}
	out.Duration = time.Since(start)

	// Run crew-level guardrails after all tasks complete.
	if err := runCrewGuardrails(ctx, c.Guardrails, out); err != nil {
		return nil, err
	}

	return out, nil
}

// WithAsyncMaxWorkers sets the cap on concurrent Async tasks per wave.
// Pass DefaultAsyncMaxWorkers for the default, or 0 for unlimited (the
// explicit "unlimited" escape hatch of D-A4). NewCrew already sets 8;
// calling this is only needed to change that value.
func (c *Crew) WithAsyncMaxWorkers(n int) *Crew {
	c.AsyncMaxWorkers = n
	return c
}

// runSequential executes the tasks in order.
func (c *Crew) runSequential(ctx context.Context) (*CrewOutput, error) {
	return c.runSequentialWithPlan(ctx, nil)
}

// runSequentialWithPlan runs the sequential process. When no task is Async
// the behavior matches the pre-async serial path. When any task is marked
// Async the crew schedules ready Async tasks per wave (already validated at
// Kickoff by planWaves) and only folds results into the output after each
// wave barrier, in declaration order.
func (c *Crew) runSequentialWithPlan(ctx context.Context, plan *asyncPlan) (*CrewOutput, error) {
	hasAsync := false
	for _, t := range c.Tasks {
		if t.Async {
			hasAsync = true
			break
		}
	}
	if !hasAsync {
		return c.runSequentialSerial(ctx)
	}
	if plan == nil {
		var err error
		plan, err = planWaves(c.Tasks)
		if err != nil {
			return nil, err // already validated at Kickoff; never reached
		}
	}
	return c.runAsyncWaves(ctx, plan, nil)
}

// runSequentialSerial is the pre-async sequential executor. Memory AutoSave
// commits immediately (no open wave barrier) so serial behavior matches
// today's Memory.Save path for single-task-at-a-time runs.
func (c *Crew) runSequentialSerial(ctx context.Context) (*CrewOutput, error) {
	out := &CrewOutput{}
	for i, task := range c.Tasks {
		if err := c.failedUpstream(task); err != nil {
			c.markFailed(task, err)
			c.logger.WarnContext(ctx, "skipping task; upstream failed",
				"task_index", i+1, "error", redactError(err))
			continue
		}
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

// runAsyncWaves executes the sequential (or hierarchical-with-agents)
// process when at least one task is Async. Tasks not marked Async still run
// one at a time (they never share a wave with a sibling). Aggregation folds
// each sub-group by ascending task index after its barrier — the same
// declaration-order contract as the staged process.
//
// agents, when non-nil, is parallel to c.Tasks and supplies pre-resolved
// agents (hierarchical D-A2 path). When nil, agents are resolved via
// agentForIndex as before.
func (c *Crew) runAsyncWaves(ctx context.Context, plan *asyncPlan, agents []*Agent) (*CrewOutput, error) {
	out := &CrewOutput{}
	// Track which tasks have been executed so a mixed wave can drain
	// non-Async leftovers after the Async subset finishes.
	done := make([]bool, len(c.Tasks))

	for w, wave := range plan.waves {
		// Drain the wave: first run all ready Async tasks (chunked by
		// AsyncMaxWorkers), then each remaining non-Async task alone.
		// D-A1: non-Async never share a group with a sibling.
		remaining := make([]int, 0, len(wave))
		for _, i := range wave {
			if done[i] {
				continue
			}
			// Skip dependents of failed tasks (D-A3 FailFast=false).
			if err := c.failedUpstream(c.Tasks[i]); err != nil {
				c.markFailed(c.Tasks[i], err)
				c.logger.WarnContext(ctx, "skipping task; upstream failed",
					"task_index", i+1, "error", redactError(err))
				done[i] = true
				continue
			}
			remaining = append(remaining, i)
		}

		// Partition into Async and non-Async within the remaining set.
		var asyncIdx, syncIdx []int
		for _, i := range remaining {
			if c.Tasks[i].Async {
				asyncIdx = append(asyncIdx, i)
			} else {
				syncIdx = append(syncIdx, i)
			}
		}

		// 1) Async subset, chunked by AsyncMaxWorkers (0 = unlimited).
		for start := 0; start < len(asyncIdx); {
			end := len(asyncIdx)
			if c.AsyncMaxWorkers > 0 && start+c.AsyncMaxWorkers < end {
				end = start + c.AsyncMaxWorkers
			}
			chunk := asyncIdx[start:end]
			label := fmt.Sprintf("wave %d", w+1)
			if err := c.runWaveGroup(ctx, label, chunk, agents, out); err != nil {
				return nil, err
			}
			for _, i := range chunk {
				done[i] = true
			}
			start = end
		}

		// 2) Non-Async ready tasks, one at a time (stable by declaration index).
		for _, i := range syncIdx {
			label := fmt.Sprintf("wave %d", w+1)
			if err := c.runWaveGroup(ctx, label, []int{i}, agents, out); err != nil {
				return nil, err
			}
			done[i] = true
		}
	}
	return out, nil
}

// runWaveGroup runs one barrier-bounded group of tasks (by crew-task index),
// folds successful results into out in declaration order, and applies the
// D-M7 memory commit at the barrier. On fail-fast abort the buffer is
// discarded and the first real error is returned.
func (c *Crew) runWaveGroup(ctx context.Context, label string, idxs []int, agents []*Agent, out *CrewOutput) error {
	if len(idxs) == 0 {
		return nil
	}
	groupTasks := make([]*Task, len(idxs))
	groupAgents := make([]*Agent, len(idxs))
	for k, i := range idxs {
		groupTasks[k] = c.Tasks[i]
		if agents != nil {
			groupAgents[k] = agents[i]
		} else {
			agent := c.Tasks[i].Agent
			if agent == nil {
				agent = c.agentForIndex(i)
			}
			groupAgents[k] = agent
		}
	}

	// D-M7: arm the per-group AutoSave buffer before workers start.
	c.beginMemoryBuffer()

	results, firstErr, firstIdx := c.runTaskGroup(ctx, groupRun{
		label:    label,
		labelKey: "wave",
		tasks:    groupTasks,
		agents:   groupAgents,
		failFast: c.AsyncFailFast,
	})
	if firstErr != nil && c.AsyncFailFast {
		// Discard uncommitted buffers — failed/cancelled never commit (G2).
		c.discardMemoryBuffer()
		// Mark every task in the group failed so dependents are skipped if
		// a higher layer ever continues (defensive; FailFast aborts Kickoff).
		for _, t := range groupTasks {
			c.markFailed(t, firstErr)
		}
		return fmt.Errorf("async wave %q: task %d: %w", label, firstIdx+1, firstErr)
	}

	// Commit successful buffers in declaration order (D-M7 / G1).
	c.commitMemoryBuffer(ctx, groupTasks, c.policy)

	// Fold results into the output in declaration order (not completion order).
	for k, res := range results {
		i := idxs[k]
		if res.err != nil {
			c.markFailed(c.Tasks[i], res.err)
			if c.AsyncFailFast {
				return fmt.Errorf("async wave %q: task %d: %w", label, i+1, res.err)
			}
			c.logger.WarnContext(ctx, "async task failed; skipping dependents (FailFast=false)",
				"task_index", i+1, "error", redactError(res.err))
			continue
		}
		if res.agent == nil {
			// Defensive: should not happen (nil agent recorded as firstErr).
			continue
		}
		warnings := res.task.Warnings()
		out.TasksOutput = append(out.TasksOutput, TaskOutput{
			Task:       taskLabel(res.task, i),
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
	return nil
}

// markFailed records that task (and, transitively, its dependents via
// failedUpstream checks) should be skipped under FailFast=false.
func (c *Crew) markFailed(task *Task, err error) {
	if c.failedTasks == nil {
		c.failedTasks = make(map[*Task]error)
	}
	if task != nil {
		c.failedTasks[task] = err
	}
}

// failedUpstream returns a non-nil error when any direct Context dependency
// of task is in the failed set (D-A3: skip dependents of failed tasks only).
func (c *Crew) failedUpstream(task *Task) error {
	if task == nil || c.failedTasks == nil {
		return nil
	}
	for _, dep := range task.Context {
		if err, ok := c.failedTasks[dep]; ok {
			return fmt.Errorf("dependency %q failed: %w", taskLabel(dep, 0), err)
		}
	}
	return nil
}

// runHierarchical uses a manager to assign each task to the best agent.
//
// When any task is Async (D-A2), agents are resolved serially first
// (predictable), then the async wave scheduler executes — the same wave /
// barrier / declaration-order fold contract as the sequential async path.
func (c *Crew) runHierarchical(ctx context.Context) (*CrewOutput, error) {
	manager, err := c.resolveManager()
	if err != nil {
		return nil, err
	}
	c.logger.InfoContext(ctx, "manager resolved", "manager", manager.Role)

	hasAsync := false
	for _, t := range c.Tasks {
		if t.Async {
			hasAsync = true
			break
		}
	}
	if hasAsync {
		// Resolve every agent first (serial manager calls), then run the DAG.
		agents := make([]*Agent, len(c.Tasks))
		for i, task := range c.Tasks {
			agent := task.Agent
			if agent == nil {
				agent = c.delegate(ctx, manager, task)
			}
			if agent == nil {
				return nil, ErrNoAgent
			}
			agents[i] = agent
			c.logger.InfoContext(ctx, "task delegated", "task_index", i+1, "agent", agent.Role)
		}
		plan, err := planWaves(c.Tasks)
		if err != nil {
			return nil, err
		}
		return c.runAsyncWaves(ctx, plan, agents)
	}

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
//
// Each stage delegates its parallel work to runTaskGroup; the stage loop owns
// the per-stage barrier (optional vs. fail-fast), the D-M7 memory commit
// (G9), and the ordered fold of results into CrewOutput.
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

		// D-M7 / G9: arm the per-stage AutoSave buffer before workers start.
		c.beginMemoryBuffer()

		results, firstErr, firstIdx := c.runStage(ctx, stageName, stage.Tasks, stage.Optional)

		// A non-optional stage aborts on the first failure — discard buffer.
		if firstErr != nil && !stage.Optional {
			c.discardMemoryBuffer()
			emitProgress(ctx, Progress{
				Stage: stageName,
				Event: "stage_completed",
			})
			return nil, fmt.Errorf("stage %q: task %d: %w", stageName, firstIdx+1, firstErr)
		}

		// Commit successful buffers in declaration order (D-M7 / G1 / G9).
		c.commitMemoryBuffer(ctx, stage.Tasks, c.policy)

		emitProgress(ctx, Progress{
			Stage: stageName,
			Event: "stage_completed",
		})

		// Aggregate results in declaration order (not completion order).
		for ti, res := range results {
			if res.err != nil {
				// Only reachable for optional stages (non-optional already
				// returned above).
				c.logger.WarnContext(ctx, "task failed in optional stage",
					"stage", stageName, "task_index", ti+1, "error", redactError(res.err))
				continue
			}
			if res.agent == nil {
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

// runStage is the staged-process adapter on top of runTaskGroup: it maps
// agent resolution to stage semantics (agentForIndex by task index) and adds
// the "stage"/"task_index" labels used by logs and errors.
func (c *Crew) runStage(ctx context.Context, stageName string, tasks []*Task, optional bool) ([]groupResult, error, int) {
	agents := make([]*Agent, len(tasks))
	for i, task := range tasks {
		agent := task.Agent
		if agent == nil {
			agent = c.agentForIndex(i)
		}
		agents[i] = agent
	}
	return c.runTaskGroup(ctx, groupRun{
		label:    stageName,
		labelKey: "stage",
		tasks:    tasks,
		agents:   agents,
		failFast: !optional,
	})
}

// groupRun configures one call to runTaskGroup.
type groupRun struct {
	// label is the human-readable stage/wave name used in logs and progress.
	label string
	// labelKey is the slog key for label ("stage" for Staged, "wave" for async).
	labelKey string
	tasks    []*Task
	// agents is parallel to tasks; entries may be nil when no agent can be
	// resolved (surfaced as the first result error, matching Staged).
	agents []*Agent
	// failFast cancels siblings on the first failure (Staged non-optional /
	// AsyncFailFast true).
	failFast bool
}

// groupResult is the outcome of a single task inside a runTaskGroup barrier,
// collected by index so the fold preserves declaration order.
type groupResult struct {
	task  *Task
	agent *Agent
	out   string
	facts []Fact
	err   error
}

// stageResult is retained as an alias for groupResult for internal
// compatibility with pre-A1 staged code/tests.
type stageResult = groupResult

// runTaskGroup runs tasks concurrently (bounded by AsyncMaxWorkers when
// positive and the group is an async wave — Staged groups pass through
// uncapped, matching pre-A3 behavior), recovers panics per task, and joins
// with a barrier before returning. Results are always indexed by the task's
// position in the group (declaration order), never by completion order. When
// failFast is true the first failure cancels the sibling goroutines; the
// returned firstErr is that first failure, not a sibling's context.Canceled
// triggered by the cancellation.
//
// The barrier (join) is the only place a process may fold results or commit
// memory: workers never observe each other's results while the group is open.
// Worker concurrency is limited by a weighted semaphore when maxWorkers > 0.
func (c *Crew) runTaskGroup(ctx context.Context, g groupRun) ([]groupResult, error, int) {
	results := make([]groupResult, len(g.tasks))

	// A single derived context for the whole group: cancelling it stops every
	// sibling goroutine (on parent cancellation or a fail-fast failure).
	groupCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	// firstErr records the chronologically first failure (and its index) so a
	// fail-fast group reports the real cause, not a sibling's context.Canceled
	// that was triggered by the cancellation.
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
		// Interrupt siblings as soon as a fail-fast task fails.
		if g.failFast {
			cancel()
		}
	}

	// Worker cap: only applied when the caller is an async wave
	// (labelKey == "wave") AND AsyncMaxWorkers > 0. Staged groups stay
	// uncapped so their historical fan-out is unchanged.
	var sem chan struct{}
	if g.labelKey == "wave" && c.AsyncMaxWorkers > 0 {
		sem = make(chan struct{}, c.AsyncMaxWorkers)
	}

	var wg sync.WaitGroup
	for ti, task := range g.tasks {
		agent := g.agents[ti]
		if agent == nil {
			// No goroutine is started for this task; the missing agent is the
			// first error. Matches the previous staged behavior of aborting
			// before executing any sibling result.
			recordErr(ti, ErrNoAgent)
			continue
		}

		wg.Add(1)
		go func(ti int, task *Task, agent *Agent) {
			defer wg.Done()
			// Recover from a panic in the task goroutine so a single
			// panicking task cannot crash the whole process.
			defer func() {
				if r := recover(); r != nil {
					err := fmt.Errorf("panic: %v", r)
					results[ti] = groupResult{err: err}
					recordErr(ti, err)
				}
			}()

			// Acquire a worker slot when capped. Respect cancellation so a
			// fail-fast sibling does not leave this goroutine blocked forever.
			if sem != nil {
				select {
				case sem <- struct{}{}:
					defer func() { <-sem }()
				case <-groupCtx.Done():
					err := groupCtx.Err()
					results[ti] = groupResult{task: task, agent: agent, err: err}
					recordErr(ti, err)
					return
				}
			}

			c.logger.InfoContext(groupCtx, "task started",
				g.labelKey, g.label, "task_index", ti+1, "agent", agent.Role)

			emitProgress(groupCtx, Progress{
				Stage: g.label,
				Task:  taskLabel(task, ti),
				Agent: agent.Role,
				Event: "task_started",
			})

			result, facts, err := c.execute(groupCtx, agent, task)
			results[ti] = groupResult{
				task:  task,
				agent: agent,
				out:   result,
				facts: facts,
				err:   err,
			}
			emitProgress(groupCtx, Progress{
				Stage: g.label,
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

	return results, firstErr, firstIdx
}

// execute runs a task, assembles the context, and persists the output/memory.
// It returns the task result string, collected facts, and an error.
//
// Memory inject uses queryCommitted (committed snapshot only — D-M7) under
// MemoryPolicy. AutoSave either stashes into the per-group buffer (when a
// wave/stage barrier is open) or Puts immediately on the serial path.
//
// If a task is supplied, it is attached to ctx as a WarningSink so tools
// (including adapters from mcp/, tools/, etc.) can record non-fatal
// diagnostics via AddWarningFromCtx or by retrieving the sink from ctx
// themselves.
func (c *Crew) execute(ctx context.Context, agent *Agent, task *Task) (string, []Fact, error) {
	if agent != nil {
		ctx = ContextWithAgentRole(ctx, agent.Role)
	}

	if task != nil {
		ctx = ContextWithWarningSink(ctx, task)
	}
	contextText := task.contextText()
	// D-M3 / D-M7: inject from the committed snapshot only. Default policy
	// keeps InjectWhenEmptyContext=true (today's behavior). Never read the
	// in-wave buffer on the prompt path.
	if c.store != nil && c.policy != nil {
		shouldInject := !c.policy.InjectWhenEmptyContext || contextText == ""
		if shouldInject {
			if mem := c.queryCommitted(ctx, c.policy); mem != "" {
				if contextText == "" {
					contextText = mem
				} else {
					contextText = contextText + "\n\n" + mem
				}
			}
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

	// AutoSave (G2: success only). Buffer under an open wave/stage barrier
	// (D-M7); otherwise Put immediately so the serial path stays visible to
	// the next task in the same Kickoff.
	if c.store != nil && c.policy != nil && c.policy.AutoSave {
		entry := c.autoSaveEntry(agent, task, result, c.policy)
		c.memBufMu.Lock()
		buffering := c.buffering
		c.memBufMu.Unlock()
		if buffering {
			c.stashMemoryBuffer(task, entry)
		} else {
			// Serial path: embed (if configured) then Put immediately so the
			// next task in the same Kickoff can recall it.
			entry = c.maybeEmbedEntry(ctx, entry, task, c.policy)
			if _, putErr := c.store.Put(ctx, entry); putErr != nil {
				// G11: warn+capture, do not abort Kickoff.
				c.logger.WarnContext(ctx, "memory AutoSave failed",
					"task", taskLabel(task, 0), "error", redactError(putErr))
				task.AddWarning(fmt.Sprintf("memory AutoSave failed: %v", putErr))
			}
		}
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

// MemorySnapshot returns the accumulated short-term memory (nil if memory is
// disabled and no *Memory store was supplied). When Memory=true, this is the
// same *Memory that backs MemoryStore for the Kickoff.
func (c *Crew) MemorySnapshot() *Memory { return c.mem }

func taskLabel(t *Task, i int) string {
	if t != nil && t.Name != "" {
		return t.Name
	}
	return fmt.Sprintf("Task %d", i+1)
}

// PeerAgents implements DelegationRoster so a Crew can be passed to
// NewDelegationTool. It returns the Crew.Agents field.
func (c *Crew) PeerAgents() []*Agent {
	if c == nil {
		return nil
	}
	return c.Agents
}

// attachDelegationTools adds delegate_to_coworker to each agent that does
// not already have it. Safe to call multiple times (idempotent per agent).
func (c *Crew) attachDelegationTools() {
	if c == nil {
		return
	}
	tool := NewDelegationTool(c)
	attach := func(a *Agent) {
		if a == nil || hasDelegationTool(a.Tools) {
			return
		}
		a.Tools = append(a.Tools, tool)
	}
	for _, a := range c.Agents {
		attach(a)
	}
	if c.ManagerAgent != nil {
		attach(c.ManagerAgent)
	}
}
