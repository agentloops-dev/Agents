package controlplane

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/agentloops-dev/agent-loops/Go/internal/controllink"
	"github.com/agentloops-dev/agent-loops/Go/internal/controlplane/store"
	"github.com/agentloops-dev/agent-loops/Go/wire"
)

// Report scopes (#1804): Me and the organisation. The organisation and the policy are an
// admin's; a member sees only Me. There is no group scope (#5278).
func TestReportScopes(t *testing.T) {
	t.Parallel()
	p := startPlane(t, store.NewMemory(), controllink.NewPrivateKey())
	p.host("OWNERS-MAC")
	_, admin := p.client("Owner's window")
	bea := p.invite(admin, "Bea", wire.UserRoleMember)
	cy := p.invite(admin, "Cy", wire.UserRoleMember)
	beas, cys := p.machine(bea, "BEAS-MAC"), p.machine(cy, "CYS-MAC")

	day := time.Now().UTC().Format(usageDayLayout)
	for _, h := range []*fakeHost{beas, cys} {
		r := wire.UsageRollup{Days: []wire.UsageRollupPeriod{{At: day, Rows: []wire.UsageRollupRow{{Folder: "shop", Runtime: "claude",
			Cost: map[string]float64{"USD": 2}, Turns: 1, WorkingSeconds: 60}}}}}
		params, _ := json.Marshal(r)
		if _, err := p.s.methods.HostSaid(h.member.Host, "usage/rollup", params); err != nil {
			t.Fatal(err)
		}
	}
	org := &wire.ReportScope{Kind: wire.ReportScopeKindOrg}
	week := func(s *controllink.Session, scope *wire.ReportScope) wire.UsageReport {
		t.Helper()
		var r wire.UsageReport
		json.Unmarshal(call(t, s, "", "reports/usage", wire.ReportQuery{Range: wire.ReportRangeWeek, Scope: scope}), &r)
		return r
	}

	// Bea sees Me alone, and is refused the organisation, a group and the policy.
	var scopes wire.ReportScopes
	json.Unmarshal(call(t, bea.window, "", "reports/scopes", map[string]any{}), &scopes)
	if len(scopes.Scopes) != 1 || scopes.Scopes[0].Scope.Kind != wire.ReportScopeKindMe || scopes.Admin {
		t.Fatalf("%+v", scopes)
	}
	if e := callError(t, bea.window, "", "reports/usage", wire.ReportQuery{Range: wire.ReportRangeWeek, Scope: org}); !strings.Contains(e, "Only an admin") {
		t.Fatal(e)
	}
	if e := callError(t, admin, "", "reports/usage", wire.ReportQuery{Range: wire.ReportRangeWeek, Scope: &wire.ReportScope{Kind: "group"}}); !strings.Contains(e, "me, org or project") {
		t.Fatal(e)
	}
	if e := callError(t, bea.window, "", "reports/setPolicy", wire.ReportPolicy{}); !strings.Contains(e, "Only an admin") {
		t.Fatal(e)
	}
	if r := week(bea.window, nil); len(r.Rows) != 1 || r.Rows[0].PersonName != "Bea" || r.BudgetUSD != nil {
		t.Fatalf("%+v", r)
	}

	// The admin sets the organisation's budget; a budget of any other key is left out.
	var policy wire.ReportPolicy
	json.Unmarshal(call(t, admin, "", "reports/setPolicy", wire.ReportPolicy{BudgetsUSD: map[string]float64{"org": 400, "old-group": 5}}), &policy)
	if policy.BudgetsUSD["org"] != 400 || len(policy.BudgetsUSD) != 1 {
		t.Fatalf("%+v", policy)
	}

	// The organisation: everyone, with the budget and the month's spend beside it (#2057).
	r := week(admin, org)
	if len(r.Rows) != 2 || r.Scope.Kind != wire.ReportScopeKindOrg || r.BudgetUSD == nil || *r.BudgetUSD != 400 ||
		r.MonthSpentUSD == nil || *r.MonthSpentUSD != 4 {
		t.Fatalf("%+v", r)
	}
	var csv wire.ReportExport
	json.Unmarshal(call(t, admin, "", "reports/export", wire.ReportQuery{Range: wire.ReportRangeWeek, Scope: org}), &csv)
	if strings.Contains(csv.Text, "groups") || csv.Count != 2 {
		t.Fatal(csv.Text)
	}
}

// The scopes' methods are their own list, naming no machine (as TestEveryClientMethodChecksTheOwner
// has it for the others).
func TestReportScopeMethodsNameNoMachine(t *testing.T) {
	t.Parallel()
	p := startPlane(t, store.NewMemory(), controllink.NewPrivateKey())
	for method := range scopeMethods {
		if own[method] || workspaceMethods[method] || hostNamingMethods[method] || consoleMethods[method] || !p.s.methods.Handles(method) {
			t.Errorf("%s: a scope method is the scopes' alone", method)
		}
	}
}

// A project lens (#5277): every person sees it, and it adds up the rows whose folder is in
// the project, on every machine.
func TestReportProjectLens(t *testing.T) {
	t.Parallel()
	p := startPlane(t, store.NewMemory(), controllink.NewPrivateKey())
	p.host("OWNERS-MAC")
	_, admin := p.client("Owner's window")
	bea := p.invite(admin, "Bea", wire.UserRoleMember)
	cy := p.invite(admin, "Cy", wire.UserRoleMember)
	beas, cys := p.machine(bea, "BEAS-MAC"), p.machine(cy, "CYS-MAC")
	var made wire.Project
	json.Unmarshal(call(t, admin, "", "projects/set", wire.ProjectSet{Name: "Shop"}), &made)
	day := time.Now().UTC().Format(usageDayLayout)
	for _, h := range []*fakeHost{beas, cys} {
		joined, _ := json.Marshal(wire.ProjectsJoined{Folders: []wire.JoinedFolder{{Folder: "file:///shop", Name: "shop", Project: made.ID}}})
		if _, err := p.s.methods.HostSaid(h.member.Host, "projects/joined", joined); err != nil {
			t.Fatal(err)
		}
		r := wire.UsageRollup{Days: []wire.UsageRollupPeriod{{At: day, Rows: []wire.UsageRollupRow{
			{Folder: "shop", Runtime: "claude", Cost: map[string]float64{"USD": 2}, Turns: 1},
			{Folder: "other", Runtime: "claude", Cost: map[string]float64{"USD": 5}, Turns: 1}}}}}
		params, _ := json.Marshal(r)
		if _, err := p.s.methods.HostSaid(h.member.Host, "usage/rollup", params); err != nil {
			t.Fatal(err)
		}
	}
	lens := &wire.ReportScope{Kind: wire.ReportScopeKindProject, Project: &made.ID}
	var scopes wire.ReportScopes
	json.Unmarshal(call(t, bea.window, "", "reports/scopes", map[string]any{}), &scopes)
	if len(scopes.Scopes) != 2 || scopes.Scopes[1].Name != "Shop" || scopes.Scopes[1].Scope.Kind != wire.ReportScopeKindProject {
		t.Fatalf("%+v", scopes)
	}
	// A member sees the project's rows on every machine, not the other folder's.
	var r wire.UsageReport
	json.Unmarshal(call(t, bea.window, "", "reports/usage", wire.ReportQuery{Range: wire.ReportRangeWeek, Scope: lens}), &r)
	if len(r.Rows) != 2 || r.Rows[0].Folder != "shop" || r.Rows[1].Folder != "shop" || r.Scope.Kind != wire.ReportScopeKindProject {
		t.Fatalf("%+v", r)
	}
	none := "nowhere"
	if e := callError(t, bea.window, "", "reports/usage", wire.ReportQuery{Range: wire.ReportRangeWeek,
		Scope: &wire.ReportScope{Kind: wire.ReportScopeKindProject, Project: &none}}); !strings.Contains(e, "project") {
		t.Fatal(e)
	}
}
