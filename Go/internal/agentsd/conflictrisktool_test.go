package agentsd

import (
	"strings"
	"testing"

	"github.com/agentloops-dev/agent-loops/Go/wire"
)

func TestConflictRiskRequestsAreReadWhateverTheForge(t *testing.T) {
	soon := true
	head, base := "feat", "main"
	checks := "passing"
	gh := wire.ForgeItem{Node: wire.LineageNode{Kind: "pr", Ref: "github:acme/app#12"}, Checks: &checks, AutoMerge: &soon, BaseRef: &base, HeadRef: &head}
	if p, ok := conflictRiskPR("github", gh); !ok || p.Number != 12 || p.FetchRef != "refs/pull/12/head" || !p.LandsSoon {
		t.Errorf("github: %+v", p)
	}
	gl := wire.ForgeItem{Node: wire.LineageNode{Kind: "pr", Ref: "gitlab:g/app!7"}, HeadRef: &head, Labels: []wire.ForgeLabel{{Name: "approved"}}}
	if p, ok := conflictRiskPR("gitlab", gl); !ok || p.Number != 7 || p.FetchRef != "refs/merge-requests/7/head" || !p.LandsSoon {
		t.Errorf("gitlab: %+v", p)
	}
	bb := wire.ForgeItem{Node: wire.LineageNode{Kind: "pr", Ref: "bitbucket:ws/app#3"}, HeadRef: &head}
	if p, ok := conflictRiskPR("bitbucket", bb); !ok || p.FetchRef != "refs/heads/feat" || p.LandsSoon {
		t.Errorf("bitbucket: %+v", p)
	}
	if _, ok := conflictRiskPR("azuredevops", wire.ForgeItem{Node: wire.LineageNode{Ref: "azuredevops:o/p/r#4"}}); ok {
		t.Error("a request with no branch to fetch was taken")
	}
}

func TestConflictRiskAnalysisIsListedAndAnswersWhereThereIsNoForge(t *testing.T) {
	var listed bool
	for _, raw := range offeredTools(false, false, false, false) {
		listed = listed || strings.Contains(string(raw), `"conflict_risk_analysis"`)
	}
	if !listed {
		t.Fatal("conflict_risk_analysis is not offered")
	}
	d, _ := start(t, t.TempDir())
	defer d.Shutdown()
	dir := t.TempDir()
	a := seeded(t, d, dir, nil)
	m := appClient(t, d, a.ID)
	m.post("initialize", map[string]any{}, 1)
	text, isErr := m.tool(2, "conflict_risk_analysis", map[string]any{})
	if !isErr || !strings.Contains(text, "not on a forge") {
		t.Errorf("%v %q", isErr, text)
	}
}
