// Package conflictrisk is conflictRiskAnalysis (#5473, spec #5389): before an agent pushes or
// lets a change land, it says which open pull requests that change would conflict with, and
// one advice: go, wait for #N, or stack on #N. Advisory, never a gate.
//
// It is repo-agnostic and forge-neutral: it knows git (git merge-tree --write-tree, which
// writes objects and never touches a worktree) and a list of open requests that the caller
// read from its forge (GitHub, GitLab, Forgejo, Bitbucket, Azure Repos). It calls no forge.
package conflictrisk

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// Version is the report's version.
const Version = 1

// The advice, and a request's state.
const (
	Go    = "go"
	Wait  = "wait"
	Stack = "stack"

	Clean    = "clean"
	Conflict = "conflict"
	Errored  = "error"
)

// smallFiles and smallLines are the overlap that never says wait (spec rule 4).
const (
	smallFiles = 3
	smallLines = 40
)

// Regenerating are the files a command remakes, so a conflict in one is not counted (spec).
// A project names more in Options.Ignore.
var Regenerating = []string{"*_gen.go", "generated.ts", "tokens.css", "Package.resolved", "go.sum"}

// PR is one open pull or merge request, as the caller's forge says it.
type PR struct {
	Number int `json:"number"`
	// FetchRef is what to fetch from the remote to get its head: GitHub's refs/pull/N/head,
	// GitLab's refs/merge-requests/N/head, or its branch (refs/heads/x). Empty: Head is
	// already in the repository.
	FetchRef string `json:"fetchRef,omitempty"`
	// Head is its head commit, when the caller knows it and has it.
	Head string `json:"head,omitempty"`
	// Base is the branch it targets; "" or the default branch for main.
	Base string `json:"base,omitempty"`
	// LandsSoon: checks green and auto-merge on, or approved.
	LandsSoon bool `json:"landsSoon,omitempty"`
}

// Options is one analysis.
type Options struct {
	// Repo is a clone of the project: any folder in it.
	Repo string
	// Remote is where FetchRef is fetched from; "origin" when empty.
	Remote string
	// Main is the default branch's ref (origin/main); found when empty.
	Main string
	// Head is the diff: a branch, tag or commit. Empty: the working tree, with its
	// uncommitted and new files, made into a temporary commit that nothing refers to.
	Head string
	// Self is the number of the lane's own request, left out of the list.
	Self int
	// DependsOn are the requests the lane's change builds on (the lead says so): stack, not wait.
	DependsOn []int
	// Ignore are more files that regenerate (path globs, or a name), beside Regenerating.
	Ignore []string
	PRs    []PR
}

// Entry is the report's line for one request.
type Entry struct {
	Number    int      `json:"number"`
	Head      string   `json:"head"`
	Base      string   `json:"base"`
	State     string   `json:"state"`
	Files     []string `json:"files"`
	Ignored   []string `json:"ignored"`
	Lines     int      `json:"lines"`
	LandsSoon bool     `json:"lands_soon"`
	Advice    string   `json:"advice"`
	Note      string   `json:"note,omitempty"`
	// MainConflicts are the request's own conflicts with the default branch: not counted.
	MainConflicts []string `json:"main_conflicts,omitempty"`
}

// Report is what the tool answers.
type Report struct {
	Version       int      `json:"version"`
	Head          string   `json:"head"`
	Main          string   `json:"main"`
	MainConflicts []string `json:"main_conflicts"`
	// RebaseNow: the lane conflicts with the default branch itself, whatever the others do.
	RebaseNow bool    `json:"rebase_now"`
	Advice    string  `json:"advice"`
	AdviceFor int     `json:"advice_for,omitempty"`
	PRs       []Entry `json:"prs"`
}

type runner struct {
	dir string
	env []string
}

func (r runner) git(ctx context.Context, args ...string) ([]byte, int, error) {
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = r.dir
	cmd.Env = append(append(os.Environ(), "GIT_TERMINAL_PROMPT=0", "LC_ALL=C"), r.env...)
	var out, errb bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errb
	err := cmd.Run()
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		return out.Bytes(), ee.ExitCode(), fmt.Errorf("git %s: %s", args[0], strings.TrimSpace(errb.String()))
	}
	if err != nil {
		return nil, -1, err
	}
	return out.Bytes(), 0, nil
}

func (r runner) text(ctx context.Context, args ...string) (string, error) {
	out, _, err := r.git(ctx, args...)
	return strings.TrimSpace(string(out)), err
}

// Analyse reads the repository and the open requests and says what each one means for the lane.
func Analyse(ctx context.Context, o Options) (Report, error) {
	r := runner{dir: o.Repo}
	var rep Report
	rep.Version = Version
	rep.PRs = []Entry{}
	rep.MainConflicts = []string{}
	remote := o.Remote
	if remote == "" {
		remote = "origin"
	}
	main, err := mainOf(ctx, r, o.Main, remote)
	if err != nil {
		return rep, err
	}
	rep.Main = main
	head, err := headOf(ctx, r, o.Head)
	if err != nil {
		return rep, err
	}
	rep.Head = head

	ignore := append(append([]string{}, Regenerating...), o.Ignore...)
	mc, _, err := mergeTree(ctx, r, main, head, ignore)
	if err != nil {
		return rep, err
	}
	rep.MainConflicts = mc
	rep.RebaseNow = len(mc) > 0
	mainLane := set(mc)
	laneBase, _ := r.text(ctx, "merge-base", main, head)

	prs := make([]PR, 0, len(o.PRs))
	for _, p := range o.PRs {
		if p.Number != o.Self || o.Self == 0 {
			prs = append(prs, p)
		}
	}
	sort.Slice(prs, func(i, j int) bool { return prs[i].Number < prs[j].Number })
	fetchAll(ctx, r, remote, prs)

	for _, p := range prs {
		e := entry(ctx, r, p, main, head, laneBase, mainLane, ignore, o)
		if e.Head == head {
			continue // the lane's own request
		}
		rep.PRs = append(rep.PRs, e)
	}
	rep.Advice, rep.AdviceFor = overall(rep.PRs)
	return rep, nil
}

func ref(n int) string { return "refs/conflict-risk/" + strconv.Itoa(n) }

// fetchAll gets every head in one fetch; when that fails (one is gone), one by one.
func fetchAll(ctx context.Context, r runner, remote string, prs []PR) {
	var specs []string
	for _, p := range prs {
		if p.FetchRef != "" {
			specs = append(specs, "+"+p.FetchRef+":"+ref(p.Number))
		}
	}
	if len(specs) == 0 {
		return
	}
	args := append([]string{"fetch", "--no-tags", "--quiet", "--no-write-fetch-head", remote}, specs...)
	if _, _, err := r.git(ctx, args...); err == nil {
		return
	}
	for _, s := range specs {
		r.git(ctx, "fetch", "--no-tags", "--quiet", "--no-write-fetch-head", remote, s)
	}
}

func entry(ctx context.Context, r runner, p PR, main, head, laneBase string, mainLane map[string]bool, ignore []string, o Options) Entry {
	e := Entry{Number: p.Number, Base: p.Base, State: Errored, Files: []string{}, Ignored: []string{}, LandsSoon: p.LandsSoon, Advice: Go}
	if e.Base == "" {
		e.Base = "main"
	}
	rev := p.Head
	if p.FetchRef != "" {
		rev = ref(p.Number)
	}
	if rev == "" {
		e.Note = "its head is not known"
		return e
	}
	sha, err := r.text(ctx, "rev-parse", "--verify", rev+"^{commit}")
	if err != nil {
		e.Note = "its head is not in this repository: " + err.Error()
		return e
	}
	e.Head = sha
	if sha == head {
		return e
	}
	e.Lines = lines(ctx, r, main, sha, nil)

	pc, _, err := mergeTree(ctx, r, main, sha, ignore)
	if err != nil {
		e.Note = err.Error()
		return e
	}
	e.MainConflicts = pc
	all, _, err := mergeTree(ctx, r, head, sha, nil)
	if err != nil {
		e.Note = err.Error()
		return e
	}
	mainPR := set(pc)
	for _, f := range all {
		switch {
		case ignored(f, ignore):
			e.Ignored = append(e.Ignored, f)
		case mainLane[f] || mainPR[f]:
			// Already in conflict with the default branch: not a pair's.
		default:
			e.Files = append(e.Files, f)
		}
	}
	e.State = Clean
	if len(e.Files) > 0 {
		e.State = Conflict
	}
	e.Advice, e.Note = advise(ctx, r, e, p, main, head, laneBase, sha, o)
	return e
}

// advise is the spec's rules 1 to 4 for one request.
func advise(ctx context.Context, r runner, e Entry, p PR, main, head, laneBase, sha string, o Options) (string, string) {
	if len(e.Files) == 0 {
		return Go, ""
	}
	if p.LandsSoon {
		return Wait, "it lands soon: rebase once on a clean base"
	}
	builds := contains(o.DependsOn, p.Number)
	if !builds {
		// The lane shares history with it that the default branch does not have.
		if mb, err := r.text(ctx, "merge-base", head, sha); err == nil {
			if _, code, _ := r.git(ctx, "merge-base", "--is-ancestor", mb, main); code == 1 {
				builds = true
			}
		}
	}
	if builds {
		return Stack, "the change builds on it: base the branch on its branch, no auto-merge until it lands"
	}
	note := "same files as #" + strconv.Itoa(e.Number) + ": " + strings.Join(e.Files, ", ") + "; its order is not known"
	if len(e.Files) <= smallFiles && lines(ctx, r, main, sha, e.Files) <= smallLines && lines(ctx, r, main, head, e.Files) <= smallLines {
		note = "a small overlap: " + strings.Join(e.Files, ", ")
	}
	return Go, note
}

// overall is the strongest advice: stack over wait over go. Of several waits, the one that
// lands last: the largest change. A request that waits for the lane (its base is the lane)
// was already left as go.
func overall(prs []Entry) (string, int) {
	best, num, lines := Go, 0, -1
	rank := map[string]int{Go: 0, Wait: 1, Stack: 2}
	for _, e := range prs {
		switch {
		case rank[e.Advice] > rank[best]:
			best, num, lines = e.Advice, e.Number, e.Lines
		case e.Advice == best && best != Go && e.Lines > lines:
			num, lines = e.Number, e.Lines
		}
	}
	return best, num
}

// mainOf is the default branch's commit.
func mainOf(ctx context.Context, r runner, given, remote string) (string, error) {
	cands := []string{given}
	if given == "" {
		if sym, err := r.text(ctx, "symbolic-ref", "--short", "refs/remotes/"+remote+"/HEAD"); err == nil {
			cands = append(cands, sym)
		}
		cands = append(cands, remote+"/main", remote+"/master", "main", "master")
	}
	for _, c := range cands {
		if c == "" {
			continue
		}
		if sha, err := r.text(ctx, "rev-parse", "--verify", c+"^{commit}"); err == nil {
			return sha, nil
		}
	}
	return "", errors.New("conflictRiskAnalysis: the default branch is not in this repository; name it in main")
}

// headOf is the diff's head commit; the working tree is committed to an object nothing
// refers to, with a temporary index, so nothing in the folder changes.
func headOf(ctx context.Context, r runner, given string) (string, error) {
	if given != "" {
		sha, err := r.text(ctx, "rev-parse", "--verify", given+"^{commit}")
		if err != nil {
			return "", fmt.Errorf("conflictRiskAnalysis: %q is not a commit here: %w", given, err)
		}
		return sha, nil
	}
	headSHA, err := r.text(ctx, "rev-parse", "--verify", "HEAD^{commit}")
	if err != nil {
		return "", err
	}
	gitDir, err := r.text(ctx, "rev-parse", "--absolute-git-dir")
	if err != nil {
		return "", err
	}
	idx, err := os.CreateTemp("", "conflict-risk-index-")
	if err != nil {
		return "", err
	}
	idx.Close()
	defer os.Remove(idx.Name())
	if src := filepath.Join(gitDir, "index"); fileExists(src) {
		b, _ := os.ReadFile(src)
		os.WriteFile(idx.Name(), b, 0o600)
	}
	w := runner{dir: r.dir, env: []string{"GIT_INDEX_FILE=" + idx.Name()}}
	if _, _, err := w.git(ctx, "add", "-A", "--", "."); err != nil {
		return "", err
	}
	tree, err := w.text(ctx, "write-tree")
	if err != nil {
		return "", err
	}
	c := w
	c.env = append(c.env, "GIT_AUTHOR_NAME=conflict-risk", "GIT_AUTHOR_EMAIL=conflict-risk@localhost",
		"GIT_COMMITTER_NAME=conflict-risk", "GIT_COMMITTER_EMAIL=conflict-risk@localhost")
	return c.text(ctx, "commit-tree", tree, "-p", headSHA, "-m", "conflictRiskAnalysis: working tree")
}

func fileExists(p string) bool { _, err := os.Stat(p); return err == nil }

// mergeTree is the files git cannot merge for a and b. Exit 1 is a conflict, not a failure.
// A file that regenerates (ignore, when given) is left out.
func mergeTree(ctx context.Context, r runner, a, b string, ignore []string) ([]string, bool, error) {
	out, code, err := r.git(ctx, "merge-tree", "--write-tree", "--name-only", "--no-messages", a, b)
	if code != 0 && code != 1 {
		return nil, false, fmt.Errorf("merge-tree %s %s: %w", short(a), short(b), err)
	}
	files := []string{}
	for i, l := range strings.Split(strings.TrimRight(string(out), "\n"), "\n") {
		if i == 0 || l == "" {
			continue // the tree's id
		}
		if ignore != nil && ignored(l, ignore) {
			continue
		}
		files = append(files, l)
	}
	sort.Strings(files)
	return slicesCompact(files), code == 1, nil
}

func short(s string) string {
	if len(s) > 8 {
		return s[:8]
	}
	return s
}

// lines is additions plus deletions of sha since it left main, of only files when named.
func lines(ctx context.Context, r runner, main, sha string, files []string) int {
	base, err := r.text(ctx, "merge-base", main, sha)
	if err != nil {
		return 0
	}
	args := []string{"diff", "--numstat", base, sha}
	if len(files) > 0 {
		args = append(args, "--")
		args = append(args, files...)
	}
	out, err := r.text(ctx, args...)
	if err != nil {
		return 0
	}
	n := 0
	for _, l := range strings.Split(out, "\n") {
		f := strings.Fields(l)
		if len(f) < 2 {
			continue
		}
		a, _ := strconv.Atoi(f[0])
		d, _ := strconv.Atoi(f[1])
		n += a + d
	}
	return n
}

func ignored(file string, patterns []string) bool {
	for _, p := range patterns {
		if ok, _ := path.Match(p, file); ok {
			return true
		}
		if ok, _ := path.Match(p, path.Base(file)); ok {
			return true
		}
	}
	return false
}

func set(s []string) map[string]bool {
	m := make(map[string]bool, len(s))
	for _, x := range s {
		m[x] = true
	}
	return m
}

func contains(s []int, n int) bool {
	for _, x := range s {
		if x == n {
			return true
		}
	}
	return false
}

func slicesCompact(s []string) []string {
	out := s[:0]
	for i, x := range s {
		if i == 0 || x != s[i-1] {
			out = append(out, x)
		}
	}
	return out
}

// Words is the report as an agent or a person reads it: one line per request, then the advice.
func (r Report) Words() string {
	var b strings.Builder
	if r.RebaseNow {
		fmt.Fprintf(&b, "Rebase now: this change conflicts with the default branch in %s.\n", strings.Join(r.MainConflicts, ", "))
	}
	for _, e := range r.PRs {
		switch e.State {
		case Clean:
			fmt.Fprintf(&b, "#%d: no conflict.\n", e.Number)
		case Conflict:
			fmt.Fprintf(&b, "#%d: conflict in %s. Advice: %s", e.Number, strings.Join(e.Files, ", "), e.Advice)
			if e.Note != "" {
				fmt.Fprintf(&b, " (%s)", e.Note)
			}
			b.WriteString(".\n")
		default:
			fmt.Fprintf(&b, "#%d: not checked (%s).\n", e.Number, e.Note)
		}
	}
	switch r.Advice {
	case Wait:
		fmt.Fprintf(&b, "Advice: wait for #%d, then rebase.", r.AdviceFor)
	case Stack:
		fmt.Fprintf(&b, "Advice: stack on #%d.", r.AdviceFor)
	default:
		b.WriteString("Advice: go.")
	}
	b.WriteString(" Advisory, not a gate.")
	return b.String()
}
