package controlplane

import (
	"testing"
	"time"

	"github.com/agentloops-dev/agent-loops/Go/wire"
)

func rep(step wire.MachineSetupStepID, state wire.MachineSetupState) wire.MachineSetupReport {
	return wire.MachineSetupReport{Step: step, State: state}
}

// #5355: the eight steps run in order, 5 and 6 together; a failure stops the run, keeps what
// is done, and Try Again runs the failed step and the ones after it.
func TestMachineSetupStepsRunInOrderAndRetry(t *testing.T) {
	t.Parallel()
	now := time.Now()
	st := newStoredSetup(now)
	if len(st.Steps) != 8 {
		t.Fatalf("eight steps: %d", len(st.Steps))
	}
	if _, _, err := st.report(rep(wire.MachineSetupStepIDJoin, wire.MachineSetupStateRunning), now); err == nil {
		t.Fatal("step 2 cannot begin before step 1 is done")
	}
	for i, id := range setupSteps[:4] {
		at := now.Add(time.Duration(i) * time.Second)
		if _, _, err := st.report(rep(id, wire.MachineSetupStateRunning), at); err != nil {
			t.Fatal(err)
		}
		took, finished, err := st.report(rep(id, wire.MachineSetupStateDone), at.Add(time.Second))
		if err != nil || !finished || took != time.Second {
			t.Fatalf("%s: %v %v %v", id, took, finished, err)
		}
	}
	// Runtimes and projects together; signing in waits for both.
	for _, id := range []wire.MachineSetupStepID{wire.MachineSetupStepIDRuntimes, wire.MachineSetupStepIDProjects} {
		if _, _, err := st.report(rep(id, wire.MachineSetupStateRunning), now); err != nil {
			t.Fatalf("%s together: %v", id, err)
		}
	}
	if _, _, err := st.report(rep(wire.MachineSetupStepIDRuntimes, wire.MachineSetupStateDone), now); err != nil {
		t.Fatal(err)
	}
	if _, _, err := st.report(rep(wire.MachineSetupStepIDSignIn, wire.MachineSetupStateRunning), now); err == nil {
		t.Fatal("signing in waits for projects")
	}
	msg := "script exited 1"
	if _, _, err := st.report(wire.MachineSetupReport{Step: wire.MachineSetupStepIDProjects, State: wire.MachineSetupStateFailed, Error: &msg}, now); err != nil {
		t.Fatal(err)
	}
	if _, _, err := st.report(rep(wire.MachineSetupStepIDSignIn, wire.MachineSetupStateRunning), now); err == nil {
		t.Fatal("a failed run stops")
	}
	if !st.retry() {
		t.Fatal("a failed step is retried")
	}
	if st.Steps[4].State != wire.MachineSetupStateDone || st.Steps[5].State != wire.MachineSetupStateWaiting || st.Steps[5].Error != "" {
		t.Fatalf("steps before the failed one stay done; it and the rest wait: %+v", st.Steps)
	}
	if st.retry() {
		t.Fatal("nothing failed")
	}
	for _, id := range setupSteps[5:] {
		for _, state := range []wire.MachineSetupState{wire.MachineSetupStateRunning, wire.MachineSetupStateDone} {
			if _, _, err := st.report(rep(id, state), now.Add(time.Minute)); err != nil {
				t.Fatalf("%s %s: %v", id, state, err)
			}
		}
	}
	if st.Ready == nil {
		t.Fatal("ready when step 8 is done")
	}
}

func TestMachineSetupSaysSlowerThanUsual(t *testing.T) {
	t.Parallel()
	now := time.Now()
	st := newStoredSetup(now)
	if _, _, err := st.report(rep(wire.MachineSetupStepIDCreate, wire.MachineSetupStateRunning), now); err != nil {
		t.Fatal(err)
	}
	times := map[wire.MachineSetupStepID][]float64{wire.MachineSetupStepIDCreate: {10, 20, 30}}
	if s := st.wire(now.Add(90*time.Second), times).Steps[0].Slower; s != nil {
		t.Fatal("90 s is within the 2 minute floor")
	}
	if s := st.wire(now.Add(3*time.Minute), times).Steps[0].Slower; s == nil || !*s {
		t.Fatal("3 minutes is past 3 times a 20 s median and the floor")
	}
	if s := st.wire(now.Add(time.Hour), nil).Steps[0].Slower; s != nil {
		t.Fatal("no past runs, no median")
	}
}
