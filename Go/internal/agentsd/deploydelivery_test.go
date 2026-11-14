package agentsd

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"

	"github.com/agentloops-dev/agent-loops/Go/wire"
)

// #5245: deploys/delivery's figures for each target, from the deploys kept and what each brings.
func TestDeploysDelivery(t *testing.T) {
	d, c := start(t, t.TempDir())
	defer d.Shutdown()
	c.call(t, 1, "folders/add", map[string]any{"folder": "file://" + gitFolder(t, "git@github.com:alexec/agents.git")})
	t0 := time.Date(2026, 11, 13, 10, 0, 0, 0, time.UTC)
	forge := &fakeReleases{
		compares: map[string][]compareCommit{
			"aaa..bbb": {{SHA: "s1", Message: "Deploys: lineage nodes (#5241)", At: t0.Add(-2 * time.Hour)}},
		},
		asked: map[string]int{},
	}
	d.releaseCompare, d.releaseCommitPR = forge.compare, forge.commitPR
	d.lineageEvent(deployEvent(t, "deploy.succeeded", 1, "production", "aaa", "v0.18.0", 10, wire.DeployStateSucceeded, t0.Add(-3*time.Hour), t0.Add(-3*time.Hour)))
	d.lineageEvent(deployEvent(t, "deploy.failed", 2, "production", "bbb", "v0.19.0", 11, wire.DeployStateFailed, t0.Add(-time.Hour), t0.Add(-time.Hour)))
	d.lineageEvent(deployEvent(t, "deploy.succeeded", 3, "production", "bbb", "v0.19.0", 12, wire.DeployStateSucceeded, t0, t0))
	d.lineageEvent(deployEvent(t, "deploy.succeeded", 4, "staging", "aaa", "v0.18.0", 13, wire.DeployStateSucceeded, t0, t0))
	d.lineageSettle(5 * time.Second)
	now := t0.Add(time.Hour)

	all := d.deploysDelivery(wire.DeploysDeliveryRequest{Range: wire.ReportRangeWeek}, now)
	if all.Deploys != 4 || len(all.Targets) != 2 || all.WindowDays != 7 {
		t.Fatalf("every tier: %+v, want 4 deploys in 2 targets over 7 days", all)
	}
	prod := all.Targets[0]
	if prod.Environment != "production" || prod.Tier != wire.EnvironmentTierProduction || prod.Component != "alexec/agents" ||
		prod.EnvironmentRef != "github:environment:alexec/agents#production" {
		t.Fatalf("first target: %+v, want production first", prod)
	}
	if prod.Ended != 3 || prod.Failed != 1 || prod.DeploymentFrequency.Count != 2 || *prod.ChangeFailureRate.Value != 1.0/3 {
		t.Errorf("production: %+v", prod)
	}
	if prod.RecoveryTime.Count != 1 || *prod.RecoveryTime.Value != 3600 {
		t.Errorf("recovery %+v, want 1h", prod.RecoveryTime)
	}
	if all.Resolving == nil || !*all.Resolving || prod.LeadTime.Count != 0 {
		t.Fatalf("lead time before the compare: resolving %v, %+v", all.Resolving, prod.LeadTime)
	}
	d.deploySettle(5 * time.Second)

	tier := wire.EnvironmentTierProduction
	got := d.deploysDelivery(wire.DeploysDeliveryRequest{Range: wire.ReportRangeMonth, Tier: &tier}, now)
	if len(got.Targets) != 1 || got.WindowDays != 30 || got.Resolving != nil {
		t.Fatalf("production only: %+v", got)
	}
	if l := got.Targets[0].LeadTime; l.Count != 1 || *l.Value != 2*3600 {
		t.Errorf("lead time %+v, want #5241's merge to the deploy, 2h", l)
	}

	// Out of the range: no target.
	if late := d.deploysDelivery(wire.DeploysDeliveryRequest{Range: wire.ReportRangeWeek}, now.Add(30*24*time.Hour)); len(late.Targets) != 0 {
		t.Errorf("a month later: %+v, want no target", late.Targets)
	}
}

// #5496: the machine sends every deploy it keeps to the control plane's copy (deploys/sync),
// working out what each brings on its way, and sends again only when they changed, or on a
// new link.
func TestDeploysSyncSendsEveryDeploy(t *testing.T) {
	d, c := start(t, t.TempDir())
	defer d.Shutdown()
	c.call(t, 1, "folders/add", map[string]any{"folder": "file://" + gitFolder(t, "git@github.com:alexec/agents.git")})
	t0 := time.Now().UTC().Add(-2 * time.Hour).Truncate(time.Second)
	forge := &fakeReleases{
		compares: map[string][]compareCommit{
			"aaa..bbb": {{SHA: "s1", Message: "Deploys: lineage nodes (#5241)", At: t0.Add(-2 * time.Hour)}},
		},
		asked: map[string]int{},
	}
	d.releaseCompare, d.releaseCommitPR = forge.compare, forge.commitPR
	d.lineageEvent(deployEvent(t, "deploy.succeeded", 1, "production", "aaa", "v0.18.0", 10, wire.DeployStateSucceeded, t0.Add(-time.Hour), t0.Add(-time.Hour)))
	d.lineageEvent(deployEvent(t, "deploy.succeeded", 2, "production", "bbb", "v0.19.0", 11, wire.DeployStateSucceeded, t0, t0))
	d.lineageSettle(5 * time.Second)

	var mu sync.Mutex
	var sent []wire.DeploysSync
	d.SetControlCapabilities(func() []string { return []string{wire.CapDeploysCopy1} })
	d.SetControlAsk(askControl(func(_ context.Context, method string, params any) (json.RawMessage, error) {
		if method == wire.CallDeploysSync.Name {
			mu.Lock()
			sent = append(sent, params.(wire.DeploysSync))
			mu.Unlock()
		}
		return json.RawMessage(`{}`), nil
	}))
	count := func() int { mu.Lock(); defer mu.Unlock(); return len(sent) }
	last := func() wire.DeploysSync { mu.Lock(); defer mu.Unlock(); return sent[len(sent)-1] }

	d.sendDeploys(false)
	first := last()
	if len(first.Deploys) != 2 || first.Resolving == nil || !*first.Resolving {
		t.Fatalf("first send: %+v, want both deploys, resolving", first)
	}
	f := first.Deploys[1]
	if f.Ref != "github:deploy:alexec/agents#2" || f.Component != "alexec/agents" || f.State != wire.DeployStateSucceeded ||
		len(f.Environments) != 1 || f.Environments[0].Tier != wire.EnvironmentTierProduction {
		t.Fatalf("deploy 2: %+v", f)
	}
	d.deploySettle(5 * time.Second)
	d.sendDeploys(false)
	if got := last(); got.Resolving != nil || len(got.Deploys[1].Merges) != 1 {
		t.Fatalf("after the compare: %+v, want #5241's merge", got)
	}
	n := count()
	d.sendDeploys(false)
	if count() != n {
		t.Fatal("sent again with nothing changed")
	}
	d.sendDeploys(true)
	if count() != n+1 {
		t.Fatal("a new link did not send again")
	}
}
