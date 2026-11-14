package agentsd

import (
	"context"
	"encoding/json"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/agentloops-dev/agent-loops/Go/internal/conflictrisk"
	"github.com/agentloops-dev/agent-loops/Go/wire"
)

// conflict_risk_analysis (#5473, specs/5389-conflict-risk-analysis): the app's tool over
// internal/conflictrisk. An agent, in any project, says before its first push and before
// auto-merge which open pull requests its diff would conflict with, and gets one advice: go,
// wait for #N or stack on #N. The open requests come from the folder's forge as forge/pulls
// reads them (GitHub, GitLab, Forgejo, Bitbucket, Azure Repos), never a hard-coded one; the
// conflicts from git merge-tree, which writes objects and no files. It only reads, and the
// advice is never a gate.

// conflictRiskTimeout bounds one call: the forge's list, the fetch of the heads, the merges.
const conflictRiskTimeout = 2 * time.Minute

// conflictRiskMost is how many open requests one call checks.
const conflictRiskMost = 100

var requestNumber = regexp.MustCompile(`[#!](\d+)$`)

// conflictRiskTool is conflict_risk_analysis.
func (d *Daemon) conflictRiskTool(caller wire.UUID, args map[string]json.RawMessage) (string, error) {
	a := d.Store.Agent(caller)
	if a == nil {
		return "", &Error{Code: CodeNoSuchAgent, Message: "That conversation is not open any more, so nothing was analysed."}
	}
	o := conflictrisk.Options{Repo: string(a.Cwd), Head: strings.TrimSpace(argString(args, "diff")), Ignore: d.folderKeysRaw(folderPath(a), "regeneratedFiles")}
	if o.Head == "working-tree" || o.Head == "working tree" {
		o.Head = ""
	}
	for _, g := range strings.Split(argString(args, "ignore"), ",") {
		if g = strings.TrimSpace(g); g != "" {
			o.Ignore = append(o.Ignore, g)
		}
	}
	for _, s := range strings.FieldsFunc(argString(args, "depends_on"), func(r rune) bool { return r == ',' || r == ' ' || r == '#' }) {
		if n, err := strconv.Atoi(s); err == nil {
			o.DependsOn = append(o.DependsOn, n)
		}
	}
	ctx, cancel := context.WithTimeout(d.ctx, conflictRiskTimeout)
	defer cancel()

	repo := d.folderRepo(folderPath(a))
	if repo.forge == "" {
		return "", Invalid("Nothing analysed: this folder's repo is not on a forge this host reads (GitHub, GitLab, Forgejo, Bitbucket or Azure DevOps), so there is no list of open pull requests.")
	}
	list, err := d.forgeList(ctx, true, wire.ForgeListRequest{Folder: new(folderPath(a)), State: new("open"), Limit: new(int64(conflictRiskMost))})
	if err != nil {
		return "", toolWords(err, "forge/pulls: ")
	}
	branch := ""
	if o.Head == "" {
		branch, _ = git(string(a.Cwd), "rev-parse", "--abbrev-ref", "HEAD")
	} else {
		branch = strings.TrimPrefix(o.Head, "origin/")
	}
	for _, it := range list.Items {
		if p, ok := conflictRiskPR(repo.forge, it); ok {
			if it.HeadRef != nil && *it.HeadRef == branch && branch != "" && branch != "HEAD" {
				o.Self = p.Number
			}
			o.PRs = append(o.PRs, p)
		}
	}
	rep, err := conflictrisk.Analyse(ctx, o)
	if err != nil {
		return "", Invalid("%v", err)
	}
	words := rep.Words()
	b, _ := json.Marshal(rep)
	return words + "\n\n" + string(b), nil
}

// conflictRiskPR is an open request as the core reads it, with what the forge fetches its
// head by: GitHub and Forgejo's refs/pull/N/head, GitLab's refs/merge-requests/N/head, and
// the branch for the rest.
func conflictRiskPR(forge string, it wire.ForgeItem) (conflictrisk.PR, bool) {
	m := requestNumber.FindStringSubmatch(it.Node.Ref)
	if m == nil {
		return conflictrisk.PR{}, false
	}
	n, _ := strconv.Atoi(m[1])
	p := conflictrisk.PR{Number: n, Base: deref(it.BaseRef)}
	switch forge {
	case "github", "forgejo":
		p.FetchRef = "refs/pull/" + m[1] + "/head"
	case "gitlab":
		p.FetchRef = "refs/merge-requests/" + m[1] + "/head"
	default:
		if it.HeadRef == nil || *it.HeadRef == "" {
			return p, false
		}
		p.FetchRef = "refs/heads/" + *it.HeadRef
	}
	approved := deref(it.Review) == "approved"
	for _, l := range it.Labels {
		approved = approved || l.Name == "approved"
	}
	p.LandsSoon = approved || deref(it.Checks) == "passing" && it.AutoMerge != nil && *it.AutoMerge
	return p, true
}

// folderKeysRaw is the strings a folder's approved .agents/folder.json lists under name, such
// as "regeneratedFiles": ["Web/src/protocol/generated.ts"], the files a command remakes.
func (d *Daemon) folderKeysRaw(folder, name string) []string {
	m, err := d.approvedConfig(folder)
	if err != nil {
		return nil
	}
	list, _ := m[name].([]any)
	var out []string
	for _, v := range list {
		if t, ok := v.(string); ok && strings.TrimSpace(t) != "" {
			out = append(out, strings.TrimSpace(t))
		}
	}
	return out
}
