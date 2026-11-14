package controlplane

import (
	"context"
	"encoding/json"
	"math"
	"time"

	"github.com/agentloops-dev/agent-loops/Go/internal/controlplane/records"
	"github.com/agentloops-dev/agent-loops/Go/internal/controlplane/store"
	"github.com/agentloops-dev/agent-loops/Go/wire"
)

// Report scopes (#1804): Me is the caller's own machines; the organisation is every
// machine, for an admin. There is no group scope (#5278).
//
// The policy is the organisation's budget, one object, v1/reports/policy.json, read on each call, so every copy of
// the control plane answers the same.

// scopeMethods are the scopes', their policy's (#1804) and the saved views' (#2019): each
// names no machine and checks its caller itself; reports/policy and reports/setPolicy are
// an admin's.
var scopeMethods = map[string]bool{"reports/scopes": true, "reports/policy": true, "reports/setPolicy": true,
	"reports/views": true, "reports/saveView": true, "reports/deleteView": true}

const reportPolicyKey = "v1/reports/policy.json"

// orgBudget is the organisation's key among the budgets.
const orgBudget = "org"

// mostBudgetUSD bounds a budget, so a slip of the keyboard is refused.
const mostBudgetUSD = 10_000_000

func (s *Service) reportPolicy(ctx context.Context) (wire.ReportPolicy, error) {
	policy := wire.ReportPolicy{BudgetsUSD: map[string]float64{}}
	object, err := s.cfg.Store.Get(ctx, reportPolicyKey)
	if err != nil {
		return policy, fail(storeUnavailable, "The report policy could not be read: %v", err)
	}
	if object != nil {
		json.Unmarshal(object.Data, &policy)
	}
	if policy.BudgetsUSD == nil {
		policy.BudgetsUSD = map[string]float64{}
	}
	for key := range policy.BudgetsUSD {
		if key != orgBudget {
			delete(policy.BudgetsUSD, key)
		}
	}
	return policy, nil
}

// setReportPolicy keeps an admin's policy: the organisation's budget, above nothing. A group's
// budget or lead from before #5278 is dropped.
func (s *Service) setReportPolicy(ctx context.Context, p wire.ReportPolicy) (wire.ReportPolicy, error) {
	out := wire.ReportPolicy{BudgetsUSD: map[string]float64{}}
	for key, usd := range p.BudgetsUSD {
		if key != orgBudget {
			continue
		}
		if math.IsNaN(usd) || usd < 0 || usd > mostBudgetUSD {
			return out, fail(invalidParams, "A budget is from $0 to $%d a month.", mostBudgetUSD)
		}
		if usd > 0 {
			out.BudgetsUSD[key] = usd
		}
	}
	data, err := records.Encode(out)
	if err != nil {
		return out, err
	}
	if _, err := s.cfg.Store.Put(ctx, reportPolicyKey, data, store.Always); err != nil {
		return out, fail(storeUnavailable, "The report policy could not be kept: %v", err)
	}
	return out, nil
}

// reportScopes is reports/scopes: Me, and the organisation for an admin.
func (s *Service) reportScopes(ctx context.Context, user string) (wire.ReportScopes, error) {
	admin := s.isAdmin(user)
	out := wire.ReportScopes{Admin: admin, Scopes: []wire.ReportScopeOption{{Scope: wire.ReportScope{Kind: wire.ReportScopeKindMe}, Name: "Me"}}}
	if admin {
		name := s.cfg.Name
		if settings := s.records.Settings(); settings != nil && settings.Name != "" {
			name = settings.Name
		}
		if name == "" {
			name = "Everyone"
		}
		out.Scopes = append(out.Scopes, wire.ReportScopeOption{Scope: wire.ReportScope{Kind: wire.ReportScopeKindOrg}, Name: name})
	}
	return out, nil
}

// reportScope is whose rows a scope keeps, or why the caller may not see it. With no scope,
// an admin's is the organisation and a member's their own.
func (s *Service) reportScope(ctx context.Context, scope *wire.ReportScope, user string, admin bool) (*wire.ReportScope, func(person string) bool, error) {
	if scope == nil {
		// Before scopes (#1811): an admin every person, a member their own.
		kind := wire.ReportScopeKindMe
		if admin {
			kind = wire.ReportScopeKindOrg
		}
		scope = &wire.ReportScope{Kind: kind}
	}
	switch scope.Kind {
	case wire.ReportScopeKindMe:
		return scope, func(person string) bool { return person == user }, nil
	case wire.ReportScopeKindOrg:
		if !admin {
			return nil, nil, fail(notPermitted, "Only an admin sees the whole organisation's reports.")
		}
		return scope, func(string) bool { return true }, nil
	}
	return nil, nil, fail(invalidParams, "Say a scope: me or org.")
}

// scopedReport is reports/usage for a scope: the rows the caller may see, and the budget of
// the organisation for an admin.
func (s *Service) scopedReport(ctx context.Context, r wire.ReportRange, scope *wire.ReportScope, user string) (wire.UsageReport, error) {
	admin := s.isAdmin(user)
	scope, keep, err := s.reportScope(ctx, scope, user, admin)
	if err != nil {
		return wire.UsageReport{}, err
	}
	policy, err := s.reportPolicy(ctx)
	if err != nil {
		return wire.UsageReport{}, err
	}
	rows, err := s.report(ctx, r, keep, time.Now())
	if err != nil {
		return wire.UsageReport{}, err
	}
	out := wire.UsageReport{Range: r, Rows: rows, Admin: admin, Scope: scope}
	if scope.Kind == wire.ReportScopeKindOrg {
		if usd, ok := policy.BudgetsUSD[orgBudget]; ok {
			out.BudgetUSD = &usd
			if spent, err := s.monthSpentUSD(ctx, keep, time.Now()); err == nil {
				out.MonthSpentUSD = &spent
			}
		}
	}
	return out, nil
}
