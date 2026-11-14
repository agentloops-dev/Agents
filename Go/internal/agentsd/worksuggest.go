package agentsd

import (
	"cmp"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"

	"github.com/agentloops-dev/agent-loops/Go/internal/agentsd/store"
	"github.com/agentloops-dev/agent-loops/Go/internal/githubmcp"
	"github.com/agentloops-dev/agent-loops/Go/wire"
)

// New Session's Suggested section (#5457, #5458): the open epics that have no lead, ranked, so a
// person who opens New Session to start work can lead the epic that needs a lead. One list from
// the daemon, so the Mac, the Remote and the web page show the same rows. It reads the forge's
// open epics, each epic's plan (the forge cache forge/plan uses) and this machine's sessions.

// suggestLeadsMost is how many suggestions one read gives; the rest are only counted.
const suggestLeadsMost = 3

// suggestLeadsPlans is how many plans are read at once, as workFeaturesPlans.
const suggestLeadsPlans = 4

// dismissedLeadsFile is the person's list of dismissed epics, under their home's .agents.
const dismissedLeadsFile = "dismissed-leads.json"

// dismissedLead is what an epic was when it was dismissed: it comes back when that changes.
type dismissedLead struct {
	UpdatedAt wire.Date `json:"updatedAt"`
	Done      int64     `json:"done"`
	Total     int64     `json:"total"`
}

func (d *Daemon) registerWorkSuggestLeads() {
	On(d, wire.CallWorkSuggestLeads, func(ctx context.Context, _ *Client, r wire.WorkSuggestLeadsRequest) (wire.WorkSuggestLeads, error) {
		return d.workSuggestLeads(ctx, r)
	})
	On(d, wire.CallWorkDismissLead, func(ctx context.Context, _ *Client, r wire.WorkDismissLeadRequest) (wire.ForgeDone, error) {
		return d.workDismissLead(ctx, r)
	})
}

// suggestedLeadMu keeps the dismissed list's read and write whole.
var suggestedLeadMu sync.Mutex

func (d *Daemon) dismissedLeadsPath() string {
	return filepath.Join(d.personalHome(), ".agents", dismissedLeadsFile)
}

// readDismissedLeads is the dismissed list; a file that is missing or unreadable is an empty one.
func (d *Daemon) readDismissedLeads() map[string]dismissedLead {
	out := map[string]dismissedLead{}
	if b, err := os.ReadFile(d.dismissedLeadsPath()); err == nil {
		if json.Unmarshal(b, &out) != nil || out == nil {
			out = map[string]dismissedLead{}
		}
	}
	return out
}

func (d *Daemon) writeDismissedLeads(m map[string]dismissedLead) error {
	b, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	return store.WriteAtomically(d.dismissedLeadsPath(), append(b, '\n'), 0o600)
}

// workDismissLead hides an epic until it changes (its update time or its progress) or a lead of
// it has ended.
func (d *Daemon) workDismissLead(ctx context.Context, r wire.WorkDismissLeadRequest) (wire.ForgeDone, error) {
	const method = "work/dismiss-lead"
	epics, _, err := d.suggestEpics(ctx, nil)
	if err != nil {
		return wire.ForgeDone{}, err
	}
	i := slices.IndexFunc(epics, func(e wire.ForgeItem) bool { return e.Node == r.Node })
	if i < 0 {
		return wire.ForgeDone{}, Invalid("%s: %s is not an open epic", method, r.Node.Ref)
	}
	epic := epics[i]
	p := progressOf(epic)
	suggestedLeadMu.Lock()
	defer suggestedLeadMu.Unlock()
	m := d.readDismissedLeads()
	m[r.Node.Ref] = dismissedLead{UpdatedAt: epic.UpdatedAt, Done: p.Done, Total: p.Total}
	if err := d.writeDismissedLeads(m); err != nil {
		return wire.ForgeDone{}, Invalid("%s: %v", method, err)
	}
	return wire.ForgeDone{}, nil
}

// suggestEpics reads the open epics, a few pages of them, with the forges that could not be read.
func (d *Daemon) suggestEpics(ctx context.Context, folder *string) ([]wire.ForgeItem, []wire.ForgeListError, error) {
	var epics []wire.ForgeItem
	var errs []wire.ForgeListError
	q := wire.ForgeListRequest{Folder: folder, State: new("open"), Label: new("epic"), Limit: new(int64(forgeListMost))}
	for range featureEpicPages {
		page, err := d.forgeList(ctx, false, q)
		if err != nil {
			return nil, nil, Invalid("work/suggest-leads: %v", strings.TrimPrefix(err.Error(), "forge/issues: "))
		}
		for _, e := range page.Errors {
			if !slices.Contains(errs, e) {
				errs = append(errs, e)
			}
		}
		epics = append(epics, page.Items...)
		if page.Cursor == nil {
			break
		}
		q.Cursor = page.Cursor
	}
	return epics, errs, nil
}

// suggestable is an epic with no lead, with what its rank is made of.
type suggestable struct {
	item      wire.ForgeItem
	done, tot int64
	high, ui  bool
	due       string
}

// workSuggestLeads lists the open epics with no lead, ranked.
func (d *Daemon) workSuggestLeads(ctx context.Context, r wire.WorkSuggestLeadsRequest) (wire.WorkSuggestLeads, error) {
	out := wire.WorkSuggestLeads{Suggestions: []wire.WorkLeadSuggestion{}}
	epics, listErrs, err := d.suggestEpics(ctx, r.Folder)
	if err != nil {
		return wire.WorkSuggestLeads{}, err
	}
	out.Errors = listErrs

	// No lead: the epics whose lead is none, read from the sessions each time.
	var open []wire.ForgeItem
	led := map[string]bool{}
	for _, e := range epics {
		if e.State != "open" || deref(e.Level) != "epic" {
			continue
		}
		if d.leadOf(e.Node) != nil {
			led[e.Node.Ref] = true
			continue
		}
		open = append(open, e)
	}
	// An epic that has a lead again is no longer dismissed: when that lead ends, it is back.
	suggestedLeadMu.Lock()
	dismissed := d.readDismissedLeads()
	changed := false
	for ref := range dismissed {
		if led[ref] {
			delete(dismissed, ref)
			changed = true
		}
	}
	if changed {
		_ = d.writeDismissedLeads(dismissed)
	}
	suggestedLeadMu.Unlock()
	open = slices.DeleteFunc(open, func(e wire.ForgeItem) bool {
		x, ok := dismissed[e.Node.Ref]
		return ok && x == dismissedLead{UpdatedAt: e.UpdatedAt, Done: progressOf(e).Done, Total: progressOf(e).Total}
	})

	// Each one's plan, a few at once, for its children's labels.
	rows := make([]suggestable, len(open))
	errs := make([]error, len(open))
	var wg sync.WaitGroup
	next := make(chan int)
	for range min(suggestLeadsPlans, len(open)) {
		wg.Go(func() {
			for n := range next {
				plan, err := d.forgePlan(ctx, wire.ForgePlanRequest{Node: open[n].Node})
				errs[n] = err
				rows[n] = suggestableOf(open[n], plan)
			}
		})
	}
	for n := range open {
		next <- n
	}
	close(next)
	wg.Wait()
	var ranked []suggestable
	for n, row := range rows {
		if errs[n] != nil {
			f, _ := d.forgeOwningName(open[n].Node)
			out.Errors = append(out.Errors, wire.ForgeListError{Forge: f, Message: open[n].Node.Ref + ": " + errs[n].Error()})
			row = suggestableOf(open[n], wire.ForgePlan{})
		}
		ranked = append(ranked, row)
	}

	// Due dates, by the milestone's name, from each repo's open milestones.
	d.suggestDueDates(ctx, ranked)
	rankSuggestions(ranked)
	for n, s := range ranked {
		if n >= suggestLeadsMost {
			out.More++
			continue
		}
		out.Suggestions = append(out.Suggestions, suggestionOf(s))
	}
	return out, nil
}

func progressOf(i wire.ForgeItem) wire.ForgeProgress {
	if i.Progress == nil {
		return wire.ForgeProgress{}
	}
	return *i.Progress
}

// forgeOwningName is the forge the node is on, for an error's label.
func (d *Daemon) forgeOwningName(n wire.LineageNode) (string, bool) {
	if f, _, ok := d.forgeOwning(n); ok {
		return f.Name(), true
	}
	return "", false
}

// suggestableOf is an epic with its progress and what its open children say: priority:high, and
// a story (a person can see or use it, so a client changes) is a UI child.
func suggestableOf(epic wire.ForgeItem, plan wire.ForgePlan) suggestable {
	p := progressOf(epic)
	s := suggestable{item: epic, done: p.Done, tot: p.Total}
	for _, c := range plan.Children {
		if c.Item.State != "open" {
			continue
		}
		for _, l := range c.Item.Labels {
			switch strings.ToLower(strings.TrimSpace(l.Name)) {
			case "priority:high":
				s.high = true
			case "story":
				s.ui = true
			}
		}
	}
	return s
}

// suggestDueDates fills each epic's milestone due date from its repo's open milestones (GitHub's).
func (d *Daemon) suggestDueDates(ctx context.Context, rows []suggestable) {
	byRepo := map[string][]githubmcp.Milestone{}
	read := map[string]bool{}
	gh, err := d.forgeGitHub()
	if err != nil {
		return
	}
	for n := range rows {
		m := deref(rows[n].item.Milestone)
		repo, _, ok := nodeOfRepo(rows[n].item.Node)
		if m == "" || !ok {
			continue
		}
		if !read[repo.path] {
			read[repo.path] = true
			byRepo[repo.path], _ = gh.Milestones(ctx, repo.path)
		}
		for _, l := range byRepo[repo.path] {
			if strings.EqualFold(l.Title, m) {
				rows[n].due = l.Due
			}
		}
	}
}

// rankSuggestions puts epics in AGENTS.md's order (work by epic): priority:high children first,
// then UI children, then the earliest milestone due date (an epic with none after those with
// one), then the epic nearest to done (children closed of all). The rest is by number, so the
// order is the same each read.
func rankSuggestions(rows []suggestable) {
	slices.SortStableFunc(rows, func(a, b suggestable) int {
		return cmp.Or(
			boolFirst(a.high, b.high),
			boolFirst(a.ui, b.ui),
			dueFirst(a.due, b.due),
			-cmp.Compare(doneShare(a), doneShare(b)),
			cmp.Compare(a.item.Node.Ref, b.item.Node.Ref),
		)
	})
}

func boolFirst(a, b bool) int {
	switch {
	case a == b:
		return 0
	case a:
		return -1
	}
	return 1
}

// dueFirst orders YYYY-MM-DD dates, the earliest first, and none last.
func dueFirst(a, b string) int {
	switch {
	case a == b:
		return 0
	case a == "":
		return 1
	case b == "":
		return -1
	}
	return cmp.Compare(a, b)
}

func doneShare(s suggestable) float64 {
	if s.tot == 0 {
		return 0
	}
	return float64(s.done) / float64(s.tot)
}

// suggestionOf is one row, with Start lead's form (Web/src/model/epicPlan.ts, leadStart): the
// epic's first line, the lead skill and the epic's address, the labels #<n> and lead, no worktree.
func suggestionOf(s suggestable) wire.WorkLeadSuggestion {
	i := s.item
	row := wire.WorkLeadSuggestion{Node: i.Node, Title: i.Title, URL: i.URL, Folder: i.Folder, Milestone: i.Milestone,
		Done: s.done, Total: s.tot, High: optionalTrue(s.high), Ui: optionalTrue(s.ui), Standing: i.Standing, Due: optional(s.due)}
	row.Start = leadStartOf(i)
	return row
}

func leadStartOf(i wire.ForgeItem) wire.ForgeStart {
	labels := []string{"lead"}
	first := i.Title
	if _, n, ok := nodeOfRepo(i.Node); ok {
		labels = []string{"#" + strconv.Itoa(n), "lead"}
		first = "#" + strconv.Itoa(n) + ": " + i.Title
	}
	skill := "Lead this epic with the `lead` skill (`.agents/skills/lead/SKILL.md`)"
	if url := deref(i.URL); url != "" {
		skill += ": " + url
	} else {
		skill += "."
	}
	return wire.ForgeStart{Prompt: first + "\n\n" + skill, Labels: labels}
}
