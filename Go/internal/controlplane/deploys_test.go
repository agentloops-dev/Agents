package controlplane

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/agentloops-dev/agent-loops/Go/internal/controllink"
	"github.com/agentloops-dev/agent-loops/Go/internal/controlplane/records"
	"github.com/agentloops-dev/agent-loops/Go/internal/controlplane/store"
	"github.com/agentloops-dev/agent-loops/Go/wire"
)

// deployFact is a deploy of alexec/agents to production that ended at, with merges.
func deployFact(id string, state wire.DeployState, at time.Time, merges ...time.Time) wire.DeployDeliveryFact {
	f := wire.DeployDeliveryFact{Ref: "github:deploy:alexec/agents#" + id, Component: "alexec/agents", State: state, At: wire.DateOf(at),
		Environments: []wire.DeployDeliveryEnvironment{{Name: "production", Ref: "github:environment:alexec/agents#production",
			Tier: wire.EnvironmentTierProduction}}}
	for _, m := range merges {
		f.Merges = append(f.Merges, wire.DateOf(m))
	}
	return f
}

// #5496: the control plane keeps each machine's deploys and answers deploys/deliveryAll from
// them, as a machine's deploys/delivery does: two machines' copies of one deploy count once
// (the one that says more wins); a member sees only their own machines; a send replaces the
// machine's copy; a bad ref and too many deploys are refused.
func TestDeploysCopyAnswersOverEveryMachine(t *testing.T) {
	t.Parallel()
	mem := store.NewMemory()
	p := startPlane(t, mem, controllink.NewPrivateKey())
	owner := p.host("OWNERS-MAC")
	_, admin := p.client("Owner's window")
	bea := p.invite(admin, "Bea", wire.UserRoleMember)
	beas := p.machine(bea, "BEAS-MAC")
	send := func(host string, sync wire.DeploysSync) error {
		t.Helper()
		params, _ := json.Marshal(sync)
		_, err := p.s.methods.HostSaid(host, "deploys/sync", params)
		return err
	}
	now := time.Now().UTC().Truncate(time.Second)
	day := 24 * time.Hour
	one := deployFact("1", wire.DeployStateSucceeded, now.Add(-3*day), now.Add(-4*day))
	two := deployFact("2", wire.DeployStateFailed, now.Add(-2*day))
	three := deployFact("3", wire.DeployStateSucceeded, now.Add(-day), now.Add(-day-time.Hour))
	if err := send(owner.member.Host, wire.DeploysSync{Deploys: []wire.DeployDeliveryFact{one, two, three}}); err != nil {
		t.Fatal(err)
	}
	// Bea's machine watches the same repository: deploy 3 again, without what it brings yet.
	bare := three
	bare.Merges = nil
	if err := send(beas.member.Host, wire.DeploysSync{Deploys: []wire.DeployDeliveryFact{bare}, Resolving: new(true)}); err != nil {
		t.Fatal(err)
	}

	read := func(s *controllink.Session, scope *wire.ReportScope) wire.DeploysDelivery {
		t.Helper()
		var d wire.DeploysDelivery
		if err := json.Unmarshal(call(t, s, "", "deploys/deliveryAll", wire.DeploysDeliveryRequest{Range: wire.ReportRangeWeek, Scope: scope}), &d); err != nil {
			t.Fatal(err)
		}
		return d
	}
	org := &wire.ReportScope{Kind: wire.ReportScopeKindOrg}
	d := read(admin, org)
	if d.Deploys != 3 || len(d.Targets) != 1 || d.Resolving == nil || !*d.Resolving {
		t.Fatalf("org %+v (deploy 3 is on both machines: once)", d)
	}
	tg := d.Targets[0]
	if tg.Component != "alexec/agents" || tg.Environment != "production" || tg.Ended != 3 || tg.Failed != 1 ||
		tg.LeadTime.Count != 2 || tg.RecoveryTime.Count != 1 {
		t.Fatalf("target %+v", tg)
	}
	if mine := read(bea.window, nil); mine.Deploys != 1 || len(mine.Targets) != 1 || mine.Targets[0].LeadTime.Count != 0 {
		t.Fatalf("Bea's own %+v", mine)
	}
	if e := callError(t, bea.window, "", "deploys/deliveryAll", wire.DeploysDeliveryRequest{Range: wire.ReportRangeWeek, Scope: org}); e == "" {
		t.Fatal("a member read the organisation")
	}

	// A send replaces the machine's copy.
	if err := send(owner.member.Host, wire.DeploysSync{Deploys: []wire.DeployDeliveryFact{two}}); err != nil {
		t.Fatal(err)
	}
	if d := read(admin, &wire.ReportScope{Kind: wire.ReportScopeKindMe}); d.Deploys != 1 || d.Resolving != nil {
		t.Fatalf("after the second send: %+v", d)
	}

	// What is refused: a ref that is not a deploy's, and more deploys than a machine keeps.
	bad := one
	bad.Ref = "github:run:alexec/agents#1"
	many := make([]wire.DeployDeliveryFact, mostDeploys+1)
	for i := range many {
		many[i] = one
	}
	for _, s := range []wire.DeploysSync{{Deploys: []wire.DeployDeliveryFact{bad}}, {Deploys: many}} {
		if err := send(owner.member.Host, s); err == nil {
			t.Fatalf("kept %d deploys", len(s.Deploys))
		}
	}
}

// #5496: the hourly sweep deletes a removed machine's deploys a day after its last send, and
// leaves a known machine's.
func TestDeploysSweepDropsARemovedMachine(t *testing.T) {
	t.Parallel()
	mem := store.NewMemory()
	p := startPlane(t, mem, controllink.NewPrivateKey())
	host := p.host("OWNERS-MAC").member.Host
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)
	put := func(key string, at time.Time) {
		t.Helper()
		data, _ := json.Marshal(storedDeploys{Deploys: []wire.DeployDeliveryFact{}, At: records.At(at)})
		if _, err := mem.Put(ctx, key, data, store.Always); err != nil {
			t.Fatal(err)
		}
	}
	put(deploysKey(host), now.Add(-72*time.Hour))
	put(deploysKey("GONE-MAC"), now.Add(-48*time.Hour))
	put(deploysKey("NEW-MAC"), now.Add(-time.Hour))
	if n, err := sweepDeploys(ctx, mem, p.s.records, now, mostPerSweep); err != nil || n != 1 {
		t.Fatalf("swept %d, %v", n, err)
	}
	for key, kept := range map[string]bool{deploysKey(host): true, deploysKey("GONE-MAC"): false, deploysKey("NEW-MAC"): true} {
		if o, _ := mem.Get(ctx, key); (o != nil) != kept {
			t.Fatalf("%s kept %v, want %v", key, o != nil, kept)
		}
	}
}
