package crewai

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"time"
)

// Flow is an event-driven workflow over user-defined state S (D-F1, D-F3).
// Steps are registered with Start / Listen / Router (D-F2). The library
// never locks S — callers own synchronization of shared state (D-F5).
type Flow[S any] struct {
	nodes           []flowNode[S]
	continueOnError bool
	events          EventFunc
	runMu           sync.Mutex
}

// FlowStep mutates state and returns an error to abort (or record, when
// continue-on-error is set).
type FlowStep[S any] func(ctx context.Context, state *S) error

// FlowRouter mutates state and returns the exact names of next steps
// (D-F6). Unknown names fail the run.
type FlowRouter[S any] func(ctx context.Context, state *S) ([]string, error)

// FlowStepTrace is a metadata-only record of one step (D-C4, D-F12).
type FlowStepTrace struct {
	Name       string `json:"name"`
	DurationMs int64  `json:"duration_ms,omitempty"`
	Err        string `json:"err,omitempty"`
	Skipped    bool   `json:"skipped,omitempty"`
}

// FlowResult is the outcome of Flow.Run.
type FlowResult[S any] struct {
	State    S
	Steps    []FlowStepTrace
	Errors   map[string]error
	Duration time.Duration
}

type flowKind uint8

const (
	kindListen flowKind = iota
	kindRouter
)

type flowNode[S any] struct {
	name   string
	deps   []string
	start  bool
	kind   flowKind
	listen FlowStep[S]
	router FlowRouter[S]
}

type flowStatus uint8

const (
	stPending flowStatus = iota
	stDone
	stFailed
	stSkipped
)

// NewFlow builds an empty Flow. Register steps before Run.
func NewFlow[S any]() *Flow[S] { return &Flow[S]{} }

// Start registers a zero-dep listen step and marks it as a start (D-F4).
func (f *Flow[S]) Start(name string, fn FlowStep[S]) *Flow[S] {
	f.nodes = append(f.nodes, flowNode[S]{name: name, start: true, kind: kindListen, listen: fn})
	return f
}

// Listen registers a step that runs after all deps complete (D-F10).
// A Listen with no deps is also a start (D-F4).
func (f *Flow[S]) Listen(name string, fn FlowStep[S], deps ...string) *Flow[S] {
	f.nodes = append(f.nodes, flowNode[S]{name: name, deps: append([]string(nil), deps...), kind: kindListen, listen: fn})
	return f
}

// Router registers a routing step. After it succeeds, only dependents
// named in the returned slice are enabled (D-F6).
func (f *Flow[S]) Router(name string, fn FlowRouter[S], deps ...string) *Flow[S] {
	f.nodes = append(f.nodes, flowNode[S]{name: name, deps: append([]string(nil), deps...), kind: kindRouter, router: fn})
	return f
}

// WithFlowContinueOnError records per-step errors in FlowResult.Errors
// instead of aborting at the next barrier (D-F8, D-F11). Dependents of a
// failed step are skipped.
func (f *Flow[S]) WithFlowContinueOnError() *Flow[S] {
	f.continueOnError = true
	return f
}

// WithEvents registers a lifecycle sink (flow_started / flow_step_* /
// flow_completed). Same contract as Crew.WithEvents (D-F7, D-C4).
func (f *Flow[S]) WithEvents(fn EventFunc) *Flow[S] {
	f.events = fn
	return f
}

// Run executes the flow. Only one Run may be in flight per *Flow (D-F9).
// Parallel ready steps share *S (user-owned sync, D-F5). Traces fold by
// registration order at each barrier (D-F12). ctx is checked at the next
// barrier — in-flight steps are not hard-cancelled (D-F11).
func (f *Flow[S]) Run(ctx context.Context, initial S) (*FlowResult[S], error) {
	if !f.runMu.TryLock() {
		return nil, ErrFlowRunning
	}
	defer f.runMu.Unlock()

	if ctx == nil {
		ctx = context.Background()
	}
	index, err := f.validate()
	if err != nil {
		return nil, err
	}

	id := newKickoffID()
	ctx = ContextWithKickoffID(ctx, id)
	if f.events != nil {
		ctx = ContextWithEvents(ctx, f.events)
	}

	start := time.Now()
	state := initial
	emitEvent(ctx, CrewEvent{Type: EventFlowStarted, KickoffID: id})

	n := len(f.nodes)
	status := make([]flowStatus, n)
	routerSel := make([]map[string]bool, n)
	traces := make([]FlowStepTrace, 0, n)
	errs := map[string]error{}

	var runErr error
	for {
		if err := ctx.Err(); err != nil {
			runErr = err
			break
		}
		ready, skip := f.collectReady(index, status, routerSel)
		if len(ready) == 0 && len(skip) == 0 {
			break
		}
		skipSet := map[int]bool{}
		for _, i := range skip {
			status[i] = stSkipped
			skipSet[i] = true
		}
		var out []flowStepOut
		if len(ready) > 0 {
			out = f.runWave(ctx, &state, ready)
		}
		waveErr := f.foldWave(ctx, ready, skip, skipSet, out, status, routerSel, index, &traces, errs)
		if waveErr != nil && !f.continueOnError {
			runErr = waveErr
			break
		}
		if waveErr != nil && runErr == nil {
			runErr = waveErr
		}
	}

	res := &FlowResult[S]{
		State:    state,
		Steps:    traces,
		Errors:   errs,
		Duration: time.Since(start),
	}
	ev := CrewEvent{Type: EventFlowCompleted, KickoffID: id, DurationMs: res.Duration.Milliseconds()}
	if runErr != nil && !f.continueOnError {
		ev.Err = redactString(runErr.Error())
	}
	emitEvent(ctx, ev)
	if f.continueOnError {
		return res, nil
	}
	return res, runErr
}

func (f *Flow[S]) validate() (map[string]int, error) {
	index := make(map[string]int, len(f.nodes))
	hasStart := false
	for i, n := range f.nodes {
		if n.name == "" {
			return nil, fmt.Errorf("%w: empty name", ErrFlowUnknownStep)
		}
		if _, dup := index[n.name]; dup {
			return nil, fmt.Errorf("%w: %s", ErrFlowDuplicateStep, n.name)
		}
		index[n.name] = i
		if n.start && len(n.deps) > 0 {
			return nil, fmt.Errorf("%w: %s", ErrFlowStartDeps, n.name)
		}
		if n.kind == kindListen && n.listen == nil {
			return nil, fmt.Errorf("crewai: flow step %q: nil function", n.name)
		}
		if n.kind == kindRouter && n.router == nil {
			return nil, fmt.Errorf("crewai: flow router %q: nil function", n.name)
		}
		if len(n.deps) == 0 {
			hasStart = true
		}
	}
	if !hasStart {
		return nil, ErrFlowNoStart
	}
	for _, n := range f.nodes {
		seen := map[string]bool{}
		for _, d := range n.deps {
			if d == n.name {
				return nil, fmt.Errorf("%w: %s depends on itself", ErrFlowCycle, n.name)
			}
			if _, ok := index[d]; !ok {
				return nil, fmt.Errorf("%w: %s depends on %s", ErrFlowUnknownStep, n.name, d)
			}
			if seen[d] {
				continue
			}
			seen[d] = true
		}
	}
	if err := f.detectCycle(index); err != nil {
		return nil, err
	}
	return index, nil
}

func (f *Flow[S]) detectCycle(index map[string]int) error {
	n := len(f.nodes)
	indeg := make([]int, n)
	adj := make([][]int, n)
	for i, node := range f.nodes {
		for _, d := range node.deps {
			j := index[d]
			adj[j] = append(adj[j], i)
			indeg[i]++
		}
	}
	var ready []int
	for i := 0; i < n; i++ {
		if indeg[i] == 0 {
			ready = append(ready, i)
		}
	}
	seen := 0
	for len(ready) > 0 {
		i := ready[0]
		ready = ready[1:]
		seen++
		for _, j := range adj[i] {
			indeg[j]--
			if indeg[j] == 0 {
				ready = append(ready, j)
			}
		}
	}
	if seen != n {
		return ErrFlowCycle
	}
	return nil
}

func (f *Flow[S]) collectReady(index map[string]int, status []flowStatus, routerSel []map[string]bool) (ready, skip []int) {
	for i, node := range f.nodes {
		if status[i] != stPending {
			continue
		}
		depsFinished, depsOK, gated := f.depsState(node, index, status, routerSel)
		if !depsFinished {
			continue
		}
		if !depsOK || !gated {
			skip = append(skip, i)
			continue
		}
		ready = append(ready, i)
	}
	return ready, skip
}

func (f *Flow[S]) depsState(node flowNode[S], index map[string]int, status []flowStatus, routerSel []map[string]bool) (finished, ok, gated bool) {
	ok = true
	gated = true
	for _, d := range node.deps {
		j := index[d]
		switch status[j] {
		case stPending:
			return false, false, false
		case stFailed, stSkipped:
			ok = false
		case stDone:
			if f.nodes[j].kind == kindRouter {
				if routerSel[j] == nil || !routerSel[j][node.name] {
					gated = false
				}
			}
		}
	}
	return true, ok, gated
}

type flowStepOut struct {
	dur   time.Duration
	err   error
	route []string
}

func (f *Flow[S]) runWave(ctx context.Context, state *S, ready []int) []flowStepOut {
	out := make([]flowStepOut, len(ready))
	var wg sync.WaitGroup
	for k, i := range ready {
		wg.Add(1)
		go func(k, i int) {
			defer wg.Done()
			defer func() {
				if r := recover(); r != nil {
					out[k].err = fmt.Errorf("panic: %v", r)
				}
			}()
			node := f.nodes[i]
			emitEvent(ctx, CrewEvent{Type: EventFlowStepStarted, Task: node.name})
			t0 := time.Now()
			var err error
			var route []string
			if node.kind == kindRouter {
				route, err = node.router(ctx, state)
			} else {
				err = node.listen(ctx, state)
			}
			out[k] = flowStepOut{dur: time.Since(t0), err: err, route: route}
		}(k, i)
	}
	wg.Wait()
	return out
}

func (f *Flow[S]) foldWave(
	ctx context.Context,
	ready, skip []int,
	skipSet map[int]bool,
	out []flowStepOut,
	status []flowStatus,
	routerSel []map[string]bool,
	index map[string]int,
	traces *[]FlowStepTrace,
	errs map[string]error,
) error {
	readyPos := make(map[int]int, len(ready))
	for k, i := range ready {
		readyPos[i] = k
	}
	combined := append(append([]int{}, skip...), ready...)
	sort.Ints(combined)

	var first error
	for _, i := range combined {
		node := f.nodes[i]
		if skipSet[i] {
			tr := FlowStepTrace{Name: node.name, Skipped: true}
			*traces = append(*traces, tr)
			continue
		}
		k := readyPos[i]
		tr := FlowStepTrace{Name: node.name, DurationMs: out[k].dur.Milliseconds()}
		if out[k].err != nil {
			status[i] = stFailed
			errs[node.name] = out[k].err
			tr.Err = redactString(out[k].err.Error())
			if first == nil {
				first = fmt.Errorf("crewai: flow step %q: %w", node.name, out[k].err)
			}
		} else {
			status[i] = stDone
			if node.kind == kindRouter {
				sel := map[string]bool{}
				for _, name := range out[k].route {
					j, ok := index[name]
					if !ok {
						err := fmt.Errorf("%w: %s", ErrFlowUnknownStep, name)
						status[i] = stFailed
						errs[node.name] = err
						tr.Err = redactString(err.Error())
						if first == nil {
							first = err
						}
						sel = nil
						break
					}
					depends := false
					for _, d := range f.nodes[j].deps {
						if d == node.name {
							depends = true
							break
						}
					}
					if !depends {
						err := fmt.Errorf("%w: %s is not a listener of %s", ErrFlowUnknownStep, name, node.name)
						status[i] = stFailed
						errs[node.name] = err
						tr.Err = redactString(err.Error())
						if first == nil {
							first = err
						}
						sel = nil
						break
					}
					sel[name] = true
				}
				routerSel[i] = sel
			}
		}
		*traces = append(*traces, tr)
		ev := CrewEvent{
			Type:       EventFlowStepCompleted,
			Task:       node.name,
			DurationMs: tr.DurationMs,
			Err:        tr.Err,
		}
		emitEvent(ctx, ev)
	}
	return first
}
