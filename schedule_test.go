package crewai

import (
	"errors"
	"testing"
)

// build makes a fresh Task with no dependencies.
func build() *Task { return &Task{} }

func TestPlanWavesEmpty(t *testing.T) {
	plan, err := planWaves(nil)
	if err != nil {
		t.Fatalf("planWaves(nil): %v", err)
	}
	if len(plan.waves) != 0 {
		t.Errorf("empty plan has waves: %v", plan.waves)
	}
}

func TestPlanWavesSerialChain(t *testing.T) {
	a, b, c := build(), build(), build()
	// b depends on a, c depends on b: three waves of one task each.
	b.WithContext(a)
	c.WithContext(b)

	plan, err := planWaves([]*Task{a, b, c})
	if err != nil {
		t.Fatalf("planWaves: %v", err)
	}
	want := [][]int{{0}, {1}, {2}}
	if len(plan.waves) != len(want) {
		t.Fatalf("waves = %v, want %v", plan.waves, want)
	}
	for w := range want {
		if len(plan.waves[w]) != 1 || plan.waves[w][0] != want[w][0] {
			t.Errorf("wave %d = %v, want [%d]", w, plan.waves[w], want[w][0])
		}
	}
}

func TestPlanWavesIndependentShareWave(t *testing.T) {
	a, b, c := build(), build(), build()
	a.Async = true
	b.Async = true
	// A and B independent at wave 0; C depends on both, wave 1.
	c.WithContext(a, b)

	plan, err := planWaves([]*Task{a, b, c})
	if err != nil {
		t.Fatalf("planWaves: %v", err)
	}
	if len(plan.waves) != 2 {
		t.Fatalf("waves = %v, want 2 waves", plan.waves)
	}
	if !(plan.waves[0][0] == 0 && plan.waves[0][1] == 1 && plan.waves[1][0] == 2) {
		t.Errorf("unexpected wave assignment: %v", plan.waves)
	}
	aw := plan.asyncWaves([]*Task{a, b, c})
	if len(aw[0]) != 2 {
		t.Errorf("async wave 0 = %v, want 2 Async tasks", aw[0])
	}
	if len(aw[1]) != 0 {
		t.Errorf("async wave 1 = %v, want none (c is not Async)", aw[1])
	}
}

func TestPlanWavesDiamond(t *testing.T) {
	//   a
	//  / \
	// b   c   (b,c depend on a; d depends on b,c)
	//  \ /
	//   d
	a, b, c, d := build(), build(), build(), build()
	b.WithContext(a)
	c.WithContext(a)
	d.WithContext(b, c)
	for _, tsk := range []*Task{a, b, c, d} {
		tsk.Async = true
	}

	plan, err := planWaves([]*Task{a, b, c, d})
	if err != nil {
		t.Fatalf("planWaves: %v", err)
	}
	want := [][]int{{0}, {1, 2}, {3}}
	if len(plan.waves) != len(want) {
		t.Fatalf("waves = %v, want %v", plan.waves, want)
	}
	for w := range want {
		if len(plan.waves[w]) != len(want[w]) {
			t.Fatalf("wave %d = %v, want %v", w, plan.waves[w], want[w])
		}
		for k := range want[w] {
			if plan.waves[w][k] != want[w][k] {
				t.Errorf("wave %d[%d] = %d, want %d", w, k, plan.waves[w][k], want[w][k])
			}
		}
	}
}

func TestPlanWavesCycleRejected(t *testing.T) {
	a, b := build(), build()
	a.WithContext(b)
	b.WithContext(a)
	a.Async = true
	b.Async = true

	_, err := planWaves([]*Task{a, b})
	if !errors.Is(err, ErrTaskDependencyCycle) {
		t.Fatalf("cycle error = %v, want %v", err, ErrTaskDependencyCycle)
	}
}

func TestPlanWavesSelfDependencyRejected(t *testing.T) {
	a := build()
	a.WithContext(a)
	a.Async = true

	_, err := planWaves([]*Task{a})
	if !errors.Is(err, ErrTaskDependencyCycle) {
		t.Fatalf("self-dependency error = %v, want %v", err, ErrTaskDependencyCycle)
	}
}

func TestPlanWavesDuplicateTaskPointerRejected(t *testing.T) {
	a := build()
	_, err := planWaves([]*Task{a, a})
	if !errors.Is(err, ErrTaskDependencyCycle) {
		t.Fatalf("duplicate task error = %v, want %v", err, ErrTaskDependencyCycle)
	}
}

func TestPlanWavesUnknownContextPointerIgnored(t *testing.T) {
	// A Context entry pointing at a task that is not in the crew task list is
	// ignored by the planner (same behavior as the serial executor, which also
	// ignores dependencies whose Output is empty).
	inTask := build()
	ext := build() // never assigned to the crew list
	inTask.WithContext(ext)
	inTask.Async = true

	plan, err := planWaves([]*Task{inTask})
	if err != nil {
		t.Fatalf("unknown external dependency should be ignored, got %v", err)
	}
	if len(plan.waves) != 1 || plan.waves[0][0] != 0 {
		t.Errorf("waves = %v, want [[0]]", plan.waves)
	}
}

func TestPlanWavesWaveOrderIsDeclarationOrder(t *testing.T) {
	// Ready tasks inside a wave are ascending by declaration index even when
	// the declaration lists dependents first.
	a, b, c := build(), build(), build()
	a.Async, b.Async, c.Async = true, true, true
	c.WithContext(a, b) // declared last; waves must be [{0,1},{2}]

	plan, err := planWaves([]*Task{a, b, c})
	if err != nil {
		t.Fatalf("planWaves: %v", err)
	}
	if !(plan.waves[0][0] == 0 && plan.waves[0][1] == 1) {
		t.Errorf("wave 0 = %v, want [0 1]", plan.waves[0])
	}
}
