package crewai

import "fmt"

// asyncPlan is the wave schedule over a task list for the async
// (Async-enabled) sequential / hierarchical processes. Each wave is a set of
// task indexes that may run together because all their dependencies finished
// in strictly earlier waves; waves run in order and aggregation folds by
// index inside each run after a barrier.
type asyncPlan struct {
	// waves lists, in execution order, the indexes of Crew.Tasks that may
	// start together. Indexes within a wave are ascending (declaration order).
	waves [][]int
}

// planWaves computes the DAG schedule implied by Task.Context. Each task is
// assigned to the earliest wave that is strictly after all of its Context
// dependencies, so a task is never scheduled in the same wave as a
// dependency — that is structural, not just detected.
//
// Rejected at plan time (G12, fail fast): duplicate task pointers, a Context
// edge pointing at the task itself, and any cycle. Context entries pointing
// at tasks outside the crew's task list are ignored (same behavior as the
// serial executor). Within each wave, indexes are ascending (declaration
// order), so the fold after the barrier is deterministic.
//
// The wave assignment is the single source of truth used by Kickoff: the
// async scheduler runs wave 0, then wave 1, ... in order, so every Context
// dependency is guaranteed to be in an earlier wave than its dependents.
func planWaves(tasks []*Task) (*asyncPlan, error) {
	n := len(tasks)
	if n == 0 {
		return &asyncPlan{waves: nil}, nil
	}
	index := make(map[*Task]int, n)
	for i, t := range tasks {
		if _, dup := index[t]; dup {
			return nil, fmt.Errorf("%w: task %d appears twice", ErrTaskDependencyCycle, i+1)
		}
		index[t] = i
	}

	// indegree/adjacent encode edges dep -> dependent. Unknown pointers in
	// Context are ignored (matches serial behavior).
	indegree := make([]int, n)
	adjacent := make([][]int, n) // dep -> dependents
	for i, t := range tasks {
		for _, d := range t.Context {
			j, ok := index[d]
			if !ok {
				continue // dep outside the crew list: ignored, like serial
			}
			if j == i {
				// Self-dependency is a wave-length-1 cycle.
				return nil, fmt.Errorf("%w: task %d depends on itself", ErrTaskDependencyCycle, i+1)
			}
			indegree[i]++
			adjacent[j] = append(adjacent[j], i)
		}
	}

	// Kahn's algorithm: every node is placed in the first wave after all of
	// its dependencies. This also detects cycles as leftover nodes.
	waveOf := make([]int, n)
	for i := range waveOf {
		waveOf[i] = -1
	}
	plan := &asyncPlan{}

	// ready are the tasks that may run now (indegree 0), processed in
	// ascending index order so wave membership is deterministic.
	var ready []int
	for i := 0; i < n; i++ {
		if indegree[i] == 0 {
			ready = append(ready, i)
		}
	}
	for w := 0; len(ready) > 0; w++ {
		wave := make([]int, len(ready))
		copy(wave, ready)
		for _, i := range wave {
			waveOf[i] = w
		}

		var next []int
		for _, i := range wave {
			for _, j := range adjacent[i] {
				indegree[j]--
				if indegree[j] == 0 {
					// j becomes ready in the wave after i. Because each
					// dependency is in an earlier wave than its dependents, a
					// single pass is enough to assign it.
					next = append(next, j)
				}
			}
		}
		// next is built by iterating wave in ascending order; dependents share
		// the same new wave regardless of which dep unlocked them first.
		plan.waves = append(plan.waves, wave)
		ready = next
	}

	// Any unscheduled node is part of a cycle.
	for i := 0; i < n; i++ {
		if waveOf[i] == -1 {
			return nil, fmt.Errorf("%w: task %d is part of a dependency cycle", ErrTaskDependencyCycle, i+1)
		}
	}

	// Wave assignment guarantees every dependency is in a strictly earlier
	// wave than its dependents, so same-wave Context edges are excluded by
	// construction (they can only arise from a cycle, already rejected).
	return plan, nil
}

// asyncWaves returns only the Async tasks, wave by wave. Non-Async tasks are
// left to the serial path of their process.
func (p *asyncPlan) asyncWaves(tasks []*Task) [][]int {
	out := make([][]int, len(p.waves))
	for w, wave := range p.waves {
		for _, i := range wave {
			if tasks[i].Async {
				out[w] = append(out[w], i)
			}
		}
	}
	return out
}
