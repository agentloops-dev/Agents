package agentsd

import (
	"encoding/json"
	"slices"
	"strconv"
	"testing"
	"time"

	"github.com/agentloops-dev/agents/Go/internal/githubmcp"
	"github.com/agentloops-dev/agents/Go/wire"
)

// #5458: the order is AGENTS.md's: priority:high children first, then UI children (stories),
// then the earliest milestone due date (none last), then the epic nearest to done.
func TestRankSuggestionsOrder(t *testing.T) {
	row := func(n string, high, ui bool, due string, done, total int64) suggestable {
		return suggestable{item: wire.ForgeItem{Node: wire.LineageNode{Kind: "issue", Ref: "github:issue:a/b#" + n}}, high: high, ui: ui, due: due, done: done, tot: total}
	}
	rows := []suggestable{
		row("1", false, false, "", 9, 10),
		row("2", false, true, "2026-12-06", 0, 8),
		row("3", false, true, "2026-12-04", 0, 8),
		row("4", true, false, "", 0, 3),
		row("5", false, false, "2026-12-01", 1, 10),
		row("6", false, false, "", 11, 12),
		row("7", false, true, "", 0, 0),
	}
	rankSuggestions(rows)
	var got []string
	for _, r := range rows {
		got = append(got, r.item.Node.Ref[len("github:issue:a/b#"):])
	}
	if want := []string{"4", "3", "2", "7", "5", "6", "1"}; !slices.Equal(got, want) {
		t.Fatalf("order %v, want %v", got, want)
	}
}

// #5458: only open children count, and a story is a UI child.
func TestSuggestableOfReadsOpenChildren(t *testing.T) {
	epic := wire.ForgeItem{Node: wire.LineageNode{Kind: "issue", Ref: "github:issue:a/b#1"}, Progress: &wire.ForgeProgress{Done: 2, Total: 5}}
	child := func(state string, labels ...string) wire.ForgePlanChild {
		var ls []wire.ForgeLabel
		for _, l := range labels {
			ls = append(ls, wire.ForgeLabel{Name: l})
		}
		return wire.ForgePlanChild{Item: wire.ForgeItem{State: state, Labels: ls}}
	}
	s := suggestableOf(epic, wire.ForgePlan{Children: []wire.ForgePlanChild{child("closed", "priority:high", "story"), child("open", "task")}})
	if s.high || s.ui || s.done != 2 || s.tot != 5 {
		t.Fatalf("closed children counted: %+v", s)
	}
	s = suggestableOf(epic, wire.ForgePlan{Children: []wire.ForgePlanChild{child("open", "priority:high"), child("open", "story")}})
	if !s.high || !s.ui {
		t.Fatalf("open children not counted: %+v", s)
	}
}

// #5458: an epic with a lead (a session not archived labelled #<epic> and lead) is left out; the
// others come at most 3, with the rest counted; Start lead's form is filled; a dismissed epic
// stays out until it changes; and an epic with a lead has no dismissal.
func TestWorkSuggestLeads(t *testing.T) {
	d, c, gh, _ := forgeDaemon(t)
	epic := func(n int, title string) githubmcp.Item {
		i := planIssue(n, "open", "epic")
		i.Title, i.URL = title, "https://github.com/alexec/agents/issues/"+strconv.Itoa(n)
		return i
	}
	gh.epics = []githubmcp.Item{epic(1, "One"), epic(2, "Two"), epic(3, "Three"), epic(4, "Four"), epic(5, "Five")}
	gh.plan = githubmcp.Plan{Epic: epic(1, "One"), Children: []githubmcp.PlanChild{}}
	var folder string
	for _, f := range d.folderPaths() {
		if d.folderRepo(f) == (forgeRepo{"github", "alexec/agents"}) {
			folder = f
		}
	}
	lead := func(ref string, state wire.AgentState) {
		a := seeded(t, d, folder, func(a *wire.Agent) {
			a.State = state
			if state == wire.AgentStateArchived {
				r := wire.AgentArchivedReasonByUser
				a.ArchivedReason = &r
			}
			a.Labels = []wire.SessionLabel{{Value: ref, Owner: wire.SessionLabelOwnerAgent}, {Value: "lead", Owner: wire.SessionLabelOwnerAgent}}
		})
		d.noteLineage(nil, a)
		d.lineageSettle(5 * time.Second)
	}
	lead("#1", wire.AgentStateRunning)
	lead("#2", wire.AgentStateArchived)

	var out wire.WorkSuggestLeads
	json.Unmarshal(c.call(t, 1, "work/suggest-leads", wire.WorkSuggestLeadsRequest{}), &out)
	var got []string
	for _, s := range out.Suggestions {
		got = append(got, s.Node.Ref)
	}
	if want := []string{"github:issue:alexec/agents#2", "github:issue:alexec/agents#3", "github:issue:alexec/agents#4"}; !slices.Equal(got, want) || out.More != 1 {
		t.Fatalf("suggestions %v more %d", got, out.More)
	}
	s := out.Suggestions[0]
	if s.Title != "Two" || !slices.Equal(s.Start.Labels, []string{"#2", "lead"}) || s.Start.Worktree != nil ||
		s.Start.Prompt != "#2: Two\n\nLead this epic with the `lead` skill (`.agents/skills/lead/SKILL.md`): https://github.com/alexec/agents/issues/2" {
		t.Fatalf("row: %+v", s)
	}

	c.call(t, 2, "work/dismiss-lead", wire.WorkDismissLeadRequest{Node: out.Suggestions[0].Node})
	out = wire.WorkSuggestLeads{}
	json.Unmarshal(c.call(t, 3, "work/suggest-leads", wire.WorkSuggestLeadsRequest{}), &out)
	if len(out.Suggestions) != 3 || out.Suggestions[0].Node.Ref != "github:issue:alexec/agents#3" || out.More != 0 {
		t.Fatalf("after dismiss: %+v", out)
	}

	// The epic changes: it is back.
	gh.epics[1].UpdatedAt = "2026-11-05T10:00:00Z"
	d.forgeMu.Lock()
	d.forgeCache = nil
	d.forgeMu.Unlock()
	out = wire.WorkSuggestLeads{}
	json.Unmarshal(c.call(t, 4, "work/suggest-leads", wire.WorkSuggestLeadsRequest{}), &out)
	if len(out.Suggestions) != 3 || out.Suggestions[0].Node.Ref != "github:issue:alexec/agents#2" || out.More != 1 {
		t.Fatalf("after a change: %+v", out)
	}
}
