package lineage

import (
	"testing"
	"time"

	"github.com/agentloops-dev/agent-loops/Go/wire"
)

// #5245: a target's four DORA figures from its deploys.
func TestDeployFiguresOf(t *testing.T) {
	now := time.Date(2026, 11, 14, 12, 0, 0, 0, time.UTC)
	at := wire.DateOf(now)
	since := wire.DateOf(now.Add(-28 * 24 * time.Hour))
	ago := func(hours float64) wire.Date { return at - wire.Date(hours*3600) }
	deploys := []DeployFact{
		// Too old: counted nowhere, but its merge is not counted either.
		{State: wire.DeployStateSucceeded, At: ago(40 * 24), Merges: []wire.Date{ago(41 * 24)}},
		{State: wire.DeployStateSucceeded, At: ago(100), Merges: []wire.Date{ago(102), ago(110)}},
		{State: wire.DeployStateFailed, At: ago(80)},
		{State: wire.DeployStateCancelled, At: ago(79)},
		// Recovers the failure 4 hours after it.
		{State: wire.DeployStateSucceeded, At: ago(76), Merges: []wire.Date{ago(77)}, Health: []wire.Date{ago(75)}},
		// A rollback, and the recovery of the health signal 5 hours before it.
		{State: wire.DeployStateSucceeded, At: ago(70), Rollback: true},
		{State: wire.DeployStateWaiting, At: ago(1)},
	}
	f := DeployFiguresOf(deploys, since, at)
	if f.Ended != 4 || f.Failed != 1 || f.Rollbacks != 1 {
		t.Fatalf("ended %d failed %d rollbacks %d, want 4 1 1", f.Ended, f.Failed, f.Rollbacks)
	}
	if f.DeploymentFrequency.Count != 3 || len(f.DeploymentFrequency.Weeks) != 4 || *f.DeploymentFrequency.Value != 0.75 {
		t.Errorf("frequency %+v, want 3 in 4 weeks, 0.75 a week", f.DeploymentFrequency)
	}
	if f.LeadTime.Count != 3 || *f.LeadTime.Value != 2*3600 {
		t.Errorf("lead time %+v, want the median of 1h, 2h and 10h", f.LeadTime)
	}
	if f.ChangeFailureRate.Count != 4 || *f.ChangeFailureRate.Value != 0.5 {
		t.Errorf("change failure rate %+v, want 2 of 4", f.ChangeFailureRate)
	}
	if f.RecoveryTime.Count != 2 || *f.RecoveryTime.Value != 4.5*3600 {
		t.Errorf("recovery %+v, want the median of 4h and 5h", f.RecoveryTime)
	}
	if f.RecoveryTime.Band == nil || *f.RecoveryTime.Band == "" {
		t.Error("recovery has no band")
	}

	none := DeployFiguresOf(nil, since, at)
	if none.Ended != 0 || none.LeadTime.Value != nil || none.ChangeFailureRate.Value != nil || *none.DeploymentFrequency.Value != 0 {
		t.Errorf("no deploys: %+v", none)
	}
}

// #5496: two machines' copies of one deploy count once, and the one that says more wins.
func TestDeployFactsOnce(t *testing.T) {
	ref := "github:deploy:alexec/agents#1"
	running := wire.DeployDeliveryFact{Ref: ref, State: wire.DeployStateRunning, At: 10}
	ended := wire.DeployDeliveryFact{Ref: ref, State: wire.DeployStateSucceeded, At: 20}
	brings := ended
	brings.Merges = []wire.Date{5}
	other := wire.DeployDeliveryFact{Ref: "github:deploy:alexec/agents#2", State: wire.DeployStateFailed, At: 30}
	got := DeployFactsOnce([]wire.DeployDeliveryFact{running, other, ended, brings, ended})
	if len(got) != 2 || got[0].State != wire.DeployStateSucceeded || len(got[0].Merges) != 1 || got[1].Ref != other.Ref {
		t.Fatalf("once: %+v", got)
	}
}
