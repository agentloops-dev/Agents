package agentsd

import (
	"cmp"
	"context"
	"maps"
	"slices"
	"strings"
	"time"

	"github.com/agentloops-dev/agent-loops/Go/internal/lineage"
	"github.com/agentloops-dev/agent-loops/Go/internal/safe"
	"github.com/agentloops-dev/agent-loops/Go/wire"
)

// deploys/delivery (#5245, spec 5239-deploy-model, board 5): the four DORA figures of each
// target this machine keeps deploys of (lineage-deploys.json, #5241), for Reports ▸ Planning ▸
// Delivery on the web page. A target is the deploy's component (its repo) in each environment
// it is part-of. Lead time needs what each deploy brings, from the forge's compare: the brings
// cache has it, and the deploys it lacks are worked out in the background, at most
// deliveryAsksAtOnce a read, so the answer says resolving and the next read has more.
//
// The control plane keeps a copy of the same facts (deploysync.go, #5496) and answers
// deploys/deliveryAll with the same code (lineage.DeploysDeliveryOf), for a group or org scope
// and while this machine sleeps.

// deliveryAsksAtOnce bounds the compares one read starts.
const deliveryAsksAtOnce = 20

func (d *Daemon) registerDeploysDelivery() {
	On(d, wire.CallDeploysDelivery, func(_ context.Context, _ *Client, r wire.DeploysDeliveryRequest) (wire.DeploysDelivery, error) {
		return d.deploysDelivery(r, nowFunc()), nil
	})
}

// environmentTier is the tier of a deploy's environment: the forge's, when the environment is
// the forge's and the forge says one; else by its name, as the github server's tierOf; other
// when nothing says.
func environmentTier(rec deployRecord, name string) wire.EnvironmentTier {
	if env := rec.Deploy.Target.Environment; env.Name == name && env.Tier != "" && env.Tier != wire.EnvironmentTierOther {
		return env.Tier
	}
	switch strings.ToLower(name) {
	case "production", "prod":
		return wire.EnvironmentTierProduction
	case "staging", "stage":
		return wire.EnvironmentTierStaging
	case "testing", "test", "qa":
		return wire.EnvironmentTierTesting
	case "development", "dev":
		return wire.EnvironmentTierDevelopment
	}
	return wire.EnvironmentTierOther
}

func (d *Daemon) deploysDelivery(r wire.DeploysDeliveryRequest, now time.Time) wire.DeploysDelivery {
	since := wire.DateOf(now.Add(-time.Duration(lineage.DeliveryDays(r.Range)) * 24 * time.Hour))
	facts, pending := d.deliveryFacts(since)
	out := lineage.DeploysDeliveryOf(facts, r, now)
	if pending {
		out.Resolving = &pending
	}
	return out
}

// deliveryFacts is every deploy this machine keeps, as the figures need it (lineage.DeployFact,
// with its component and environments), for deploys/delivery and the control plane's copy
// (deploys/sync, #5496). What a succeeded deploy that ended since since brings comes from the
// brings cache; those it lacks are worked out in the background, at most deliveryAsksAtOnce
// a call, and pending says some are not known yet.
func (d *Daemon) deliveryFacts(since wire.Date) ([]wire.DeployDeliveryFact, bool) {
	// The health signals seen in each deploy, read before the deploys' lock.
	health := map[string][]wire.Date{}
	links, _ := d.lineage().All()
	for _, l := range links {
		if l.Rel == lineage.RelSeenIn && l.To.Kind == lineage.KindDeploy {
			health[l.To.Ref] = append(health[l.To.Ref], l.At)
		}
	}

	type ask struct {
		ref, prev, base, head string
		repo                  forgeRepo
		at                    wire.Date
	}
	var asks []ask
	pending := false
	facts := []wire.DeployDeliveryFact{}

	d.deployBookMu.Lock()
	f := d.deploysFileLocked()
	refs := slices.Sorted(maps.Keys(f.Deploys))
	for _, ref := range refs {
		rec := f.Deploys[ref]
		r, err := wire.ParseRef(ref)
		if err != nil || r.Topic == "" {
			continue
		}
		fact := wire.DeployDeliveryFact{Ref: ref, Component: r.Topic, State: rec.Deploy.State, At: wire.DateOf(deployTime(rec)),
			Health: health[ref], Environments: []wire.DeployDeliveryEnvironment{}}
		if rec.Deploy.State == wire.DeployStateSucceeded {
			if rollbackLocked(f, ref, rec) {
				fact.Rollback = new(true)
			}
			if fact.At >= since {
				prev, before, ok := deployBeforeLocked(f, ref, rec)
				base, head := deploySHA(before.Deploy), deploySHA(rec.Deploy)
				e, cached := d.brings[ref]
				switch {
				case cached && e.prev == prev && e.ok:
					for _, n := range e.prs {
						if m, dated := e.merged[n]; dated {
							fact.Merges = append(fact.Merges, wire.DateOf(m))
						}
					}
				case ok && base != "" && head != "" && base != head && !(cached && e.prev == prev):
					pending = true
					if !d.bringsAsking[ref] {
						asks = append(asks, ask{ref, prev, base, head, forgeRepo{r.Provider, r.Topic}, fact.At})
					}
				}
			}
		}
		for _, env := range rec.Environments {
			e, err := wire.ParseRef(env)
			if err != nil || e.ID == "" {
				continue
			}
			fact.Environments = append(fact.Environments, wire.DeployDeliveryEnvironment{Name: e.ID, Ref: env, Tier: environmentTier(rec, e.ID)})
		}
		facts = append(facts, fact)
	}
	// The newest deploys first, so a page's lead time fills from now back.
	slices.SortFunc(asks, func(a, b ask) int { return cmp.Compare(b.at, a.at) })
	asks = asks[:min(len(asks), deliveryAsksAtOnce)]
	if len(asks) > 0 {
		if d.bringsAsking == nil {
			d.bringsAsking = map[string]bool{}
			d.bringsSlots = make(chan struct{}, bringsWorkers)
		}
		if d.bringsIdle == nil {
			d.bringsIdle = make(chan struct{})
		}
		for _, a := range asks {
			d.bringsAsking[a.ref] = true
			safe.Go("deploy brings", func() { d.workOutBrings(a.ref, a.prev, a.repo, a.base, a.head) })
		}
	}
	d.deployBookMu.Unlock()
	return facts, pending
}
