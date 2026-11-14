package controlplane

import (
	"context"
	"encoding/json"
	"slices"
	"strings"
	"time"

	"github.com/agentloops-dev/agent-loops/Go/internal/controlplane/records"
	"github.com/agentloops-dev/agent-loops/Go/internal/controlplane/store"
	"github.com/agentloops-dev/agent-loops/Go/internal/lineage"
	"github.com/agentloops-dev/agent-loops/Go/wire"
)

// The copy of the deploys (#5496, spec 5239-deploy-model): each machine that keeps deploys
// (lineage-deploys.json, #5241) sends all of them (deploys/sync), as deploys/delivery's figures
// need them, a few seconds after a change, and the control plane keeps them per machine under
// v1/deploys/<host>.json, in place of what the machine sent before. Only that machine writes
// its object, so nothing locks. Only refs, names, states and times (principle 7).
//
//   - The machine's store bounds what it sends (1,000 deploys), and a send is bounded again
//     here: at most mostDeploys deploys, and of each the newest mostDeployMerges merges,
//     mostDeployHealth health signals and mostDeployEnvironments environments.
//   - The hourly sweep deletes a removed machine's object a day after its last send.
//   - deploys/deliveryAll answers as the machine's deploys/delivery does
//     (lineage.DeploysDeliveryOf), over the machines in the scope (as reports/value's), each
//     deploy once.

const (
	deploysPrefix          = "v1/deploys/"
	mostDeploys            = 1_000
	mostDeployMerges       = 1_000
	mostDeployHealth       = 200
	mostDeployEnvironments = 20
)

// storedDeploys is one machine's deploys.
type storedDeploys struct {
	Deploys   []wire.DeployDeliveryFact `json:"deploys"`
	Resolving bool                      `json:"resolving,omitempty"`
	At        records.Stamp             `json:"at"`
}

func deploysKey(host string) string { return deploysPrefix + strings.ToLower(host) + ".json" }

// keepDeploysSync is deploys/sync: the deploys sent, in place of the machine's copy.
func (s *Service) keepDeploysSync(ctx context.Context, host string, sync wire.DeploysSync, now time.Time) (wire.Empty, error) {
	if s.records.Host(host) == nil {
		return wire.Empty{}, fail(noSuchHost, "No machine is called %s.", host)
	}
	if len(sync.Deploys) > mostDeploys {
		return wire.Empty{}, fail(invalidParams, "Send at most %d deploys at a time.", mostDeploys)
	}
	kept := storedDeploys{Deploys: make([]wire.DeployDeliveryFact, 0, len(sync.Deploys)), At: records.At(now)}
	for _, f := range sync.Deploys {
		r, err := wire.ParseRef(f.Ref)
		if err != nil || r.Type != "deploy" || r.Topic == "" || f.Component == "" {
			return wire.Empty{}, fail(invalidParams, "%q is not a deploy's ref.", f.Ref)
		}
		f.Merges = newest(f.Merges, mostDeployMerges)
		f.Health = newest(f.Health, mostDeployHealth)
		f.Environments = f.Environments[:min(len(f.Environments), mostDeployEnvironments)]
		kept.Deploys = append(kept.Deploys, f)
	}
	kept.Resolving = sync.Resolving != nil && *sync.Resolving
	data, err := records.Encode(kept)
	if err != nil {
		return wire.Empty{}, err
	}
	if _, err := s.cfg.Store.Put(ctx, deploysKey(host), data, store.Always); err != nil {
		return wire.Empty{}, err
	}
	return wire.Empty{}, nil
}

// newest is the last most of ds once sorted, the newest.
func newest(ds []wire.Date, most int) []wire.Date {
	if len(ds) <= most {
		return ds
	}
	ds = slices.Clone(ds)
	slices.Sort(ds)
	return ds[len(ds)-most:]
}

func (m *Methods) deploysReadMethod(ctx context.Context, params json.RawMessage, from Caller) (any, error) {
	request, err := require[wire.DeploysDeliveryRequest](params, "DeploysDeliveryRequest")
	if err != nil {
		return nil, err
	}
	user := m.s.actorFor(from.Client).User
	facts, resolving, err := m.s.scopeDeploys(ctx, request.Scope, user)
	if err != nil {
		return nil, err
	}
	out := lineage.DeploysDeliveryOf(facts, request, time.Now())
	if resolving {
		out.Resolving = &resolving
	}
	return out, nil
}

// scopeDeploys is the deploys of the machines whose people the scope takes, each once, and
// whether any of them is still working out what some bring.
func (s *Service) scopeDeploys(ctx context.Context, scope *wire.ReportScope, user string) ([]wire.DeployDeliveryFact, bool, error) {
	_, keep, err := s.reportScope(ctx, scope, user, s.isAdmin(user))
	if err != nil {
		return nil, false, err
	}
	listed, err := s.cfg.Store.List(ctx, deploysPrefix)
	if err != nil {
		return nil, false, fail(storeUnavailable, "The deploys could not be read: %v", err)
	}
	known := map[string]string{}
	for _, h := range s.records.Hosts() {
		known[strings.ToLower(h.ID)] = h.ID
	}
	var read []store.Key
	for _, k := range listed {
		host, ok := strings.CutSuffix(strings.TrimPrefix(k.Key, deploysPrefix), ".json")
		if !ok || host == "" || strings.Contains(host, "/") {
			continue
		}
		// A machine removed since: its person is not known, so only an admin sees it.
		person := ""
		if id := known[host]; id != "" {
			person = s.ownerOf(id)
		}
		if keep(person) {
			read = append(read, k)
		}
	}
	objects, err := s.storeReads.read(ctx, s.cfg.Store, deploysPrefix, listed, read)
	if err != nil {
		return nil, false, fail(storeUnavailable, "The deploys could not be read: %v", err)
	}
	var all []wire.DeployDeliveryFact
	resolving := false
	for _, data := range objects {
		var kept storedDeploys
		if data == nil || json.Unmarshal(data, &kept) != nil {
			continue
		}
		all = append(all, kept.Deploys...)
		resolving = resolving || kept.Resolving
	}
	return lineage.DeployFactsOnce(all), resolving, nil
}

// sweepDeploys deletes the objects of machines removed a day since; at most most deletions.
func sweepDeploys(ctx context.Context, st store.Store, rs *records.Records, now time.Time, most int) (int, error) {
	listed, err := st.List(ctx, deploysPrefix)
	if err != nil {
		return 0, err
	}
	known := map[string]bool{}
	for _, h := range rs.Hosts() {
		known[strings.ToLower(h.ID)] = true
	}
	n := 0
	for _, k := range listed {
		if n >= most {
			break
		}
		host, _ := strings.CutSuffix(strings.TrimPrefix(k.Key, deploysPrefix), ".json")
		if known[host] {
			continue
		}
		// A machine another copy has just added is not in these records yet: only a copy a
		// day old goes.
		object, err := st.Get(ctx, k.Key)
		if err != nil {
			return n, err
		}
		var kept storedDeploys
		if object != nil && json.Unmarshal(object.Data, &kept) == nil && kept.At.After(now.Add(-24*time.Hour)) {
			continue
		}
		if err := st.Delete(ctx, k.Key); err != nil {
			return n, err
		}
		n++
	}
	return n, nil
}
