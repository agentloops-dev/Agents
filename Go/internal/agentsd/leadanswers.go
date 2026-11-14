package agentsd

import (
	"encoding/json"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/agentloops-dev/agent-loops/Go/internal/agentsd/store"
	"github.com/agentloops-dev/agent-loops/Go/wire"
)

// answer_permission (#5440): the session that started a helper answers the helper's
// permission card, on the same card the person sees. It is not #46's auto-approve: no
// model or rule answers for the person; an agent the person started, and so trusts, answers
// its own helper, and the record names it. The card shows to the person at once and is
// never held for the lead (no-hold rule, 2026-11-10): whoever answers first answers it.
//
// The lead can allow only once, only what its own mode would allow it without asking
// (modeRanks), and never past the policy; it can always deny. A step that is hard to undo
// stays with the person, allow or deny (Alex, 2026-11-14).

// kindRanks is the least mode rank (modeRanks) that does a tool call of an ACP kind without
// asking: reads in the default mode, edits in acceptEdits, commands and the rest in auto.
var kindRanks = map[string]int{"read": 1, "search": 1, "think": 1, "fetch": 1, "edit": 2, "move": 2}

const otherKindRank = 3

func (d *Daemon) answerPermission(caller wire.UUID, args map[string]json.RawMessage) (string, error) {
	lead := d.Store.Agent(caller)
	if lead == nil {
		return "", &Error{Code: CodeNoSuchAgent, Message: "That conversation is not open any more, so nothing was answered."}
	}
	id := wire.UUID(strings.ToUpper(strings.TrimSpace(argString(args, "id"))))
	helper := d.Store.Agent(id)
	if helper == nil {
		return "", &Error{Code: CodeNoSuchAgent, Message: "Nothing was answered: there is no session with that id."}
	}
	if helper.ID == caller || !d.reportsTo(helper, lead) {
		return "", &Error{Code: wire.FailureNotYours, Message: "Nothing was answered: you can answer only the cards of a session you started (with start_session, or a workflow you ran). The person answers this one."}
	}
	answer := strings.ToLower(strings.TrimSpace(argString(args, "answer")))
	if answer != "allow" && answer != "deny" {
		return "", Invalid("Nothing was answered: answer is allow or deny.")
	}
	l := d.existingLive(helper.ID)
	var waiting []wire.PermissionRequest
	if l != nil {
		l.mu.Lock()
		for _, pp := range l.pending {
			if !pp.taken {
				waiting = append(waiting, pp.req)
			}
		}
		l.mu.Unlock()
	}
	sort.Slice(waiting, func(i, j int) bool { return waiting[i].AskedAt < waiting[j].AskedAt })
	name := quotedTitle(helper.Title)
	req, err := pickRequest(waiting, wire.UUID(strings.ToUpper(strings.TrimSpace(argString(args, "request")))), name)
	if err != nil {
		return "", err
	}
	if why := hardToUndo(req.ToolCall, store.FolderPath(helper.Cwd)); why != "" {
		return "", Invalid("Nothing was answered: %s, which is hard to undo, so it stays with the person on %s's card. Do not ask them for it again: they see it already.", why, name)
	}
	option := ""
	if answer == "allow" {
		if why := d.leadMayNotAllow(lead, helper, req.ToolCall); why != "" {
			return "", Invalid("Nothing was answered: %s. The person answers %s's card; you can still deny it.", why, name)
		}
		option = optionOfKind(req.Options, wire.PermissionOptionKindAllowOnce)
		if option == "" {
			return "", Invalid("Nothing was answered: the card has no Allow once, and you can allow only once. The person answers it.")
		}
	} else {
		option = optionOfKind(req.Options, wire.PermissionOptionKindRejectOnce, wire.PermissionOptionKindRejectAlways)
		if option == "" {
			return "", Invalid("Nothing was answered: the card has no Deny. The person answers it.")
		}
	}
	if !l.answerAs(req.ID, option, &permissionAnswerer{agent: lead.ID, title: plainTitle(lead.Title)}) {
		return "", Invalid("Nothing was answered: %s's card for “%s” was answered or ended first.", name, req.ToolCall.Title)
	}
	verb := "Allowed"
	if answer == "deny" {
		verb = "Denied"
	}
	return verb + " “" + req.ToolCall.Title + "” for " + name + ", once. Its card closed on every client, and its chat says you chose.", nil
}

// plainTitle is a session's title with no quotes, for "<title> chose …".
func plainTitle(t *string) string {
	if t == nil || strings.TrimSpace(*t) == "" {
		return "Untitled"
	}
	return strings.TrimSpace(*t)
}

// existingLive is the helper's live state, or nil when it has none (no card can wait).
func (d *Daemon) existingLive(id wire.UUID) *live {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.lives[id]
}

func pickRequest(waiting []wire.PermissionRequest, want wire.UUID, name string) (wire.PermissionRequest, error) {
	if len(waiting) == 0 {
		return wire.PermissionRequest{}, Invalid("Nothing was answered: %s is not waiting on a permission card.", name)
	}
	if want == "" {
		if len(waiting) == 1 {
			return waiting[0], nil
		}
		var lines []string
		for _, r := range waiting {
			lines = append(lines, string(r.ID)+": “"+r.ToolCall.Title+"”")
		}
		return wire.PermissionRequest{}, Invalid("Nothing was answered: %s waits on %d cards; name one in request: %s.", name, len(waiting), strings.Join(lines, "; "))
	}
	for _, r := range waiting {
		if r.ID == want {
			return r, nil
		}
	}
	return wire.PermissionRequest{}, Invalid("Nothing was answered: %s has no card %s waiting; it was answered or ended.", name, want)
}

func optionOfKind(options []wire.PermissionOption, kinds ...wire.PermissionOptionKind) string {
	for _, k := range kinds {
		for _, o := range options {
			if o.Kind == k {
				return o.OptionID
			}
		}
	}
	return ""
}

// leadMayNotAllow is why the lead may not allow this tool call, or "": the policy asks the
// person for each request, the lead's mode is not known, or its mode would ask for it too.
func (d *Daemon) leadMayNotAllow(lead, helper *wire.Agent, call wire.ToolCall) string {
	if yr := d.yourPolicyRules().PermissionRequests; yr != nil && *yr == wire.ClientPermissionModeDefault {
		return "your policy says Ask me for every permission request (Policy ▸ Permission requests)"
	}
	if _, _, capped := d.alwaysApproveCeiling(); capped {
		return "the organisation's policy caps the permission mode, so each request goes to the person"
	}
	mode := inheritedMode(lead, lead.RuntimeID)
	if mode == nil {
		return "your runtime does not say its permission mode, so the app cannot tell whether it allows this"
	}
	rank, ok := modeRank(*mode)
	if !ok {
		return "your permission mode (" + *mode + ") is not one the app ranks"
	}
	kind := ""
	if call.Kind != nil {
		kind = *call.Kind
	}
	need, ok := kindRanks[kind]
	if !ok {
		need = otherKindRank
	}
	if rank < need {
		return "your mode (" + *mode + ") would ask the person for this too"
	}
	on := func(a *wire.Agent) bool {
		return a.EffectiveSandbox != nil && a.EffectiveSandbox.State == wire.SandboxStateOn
	}
	if need >= otherKindRank && on(lead) && !on(helper) {
		return "your commands run in a sandbox and its do not"
	}
	return ""
}

// hardToUndo is why a tool call is a step only the person answers, or "" (#5440): a delete,
// a file outside the helper's folder, a force push or a deleted branch or tag, a hard reset
// or clean, an rm, a publish, a write to an outside service, or a payment.
func hardToUndo(call wire.ToolCall, folder string) string {
	if call.Kind != nil && *call.Kind == "delete" {
		return "it deletes"
	}
	for _, loc := range call.Locations {
		if outside(loc.Path, folder) {
			return "it touches " + loc.Path + ", outside the session's folder"
		}
	}
	text := call.Title + "\n" + commandOf(call.RawInput)
	for _, r := range hardCommands {
		if r.re.MatchString(text) {
			return "it " + r.why
		}
	}
	return ""
}

func outside(path, folder string) bool {
	if path == "" || folder == "" || !filepath.IsAbs(path) {
		return false
	}
	rel, err := filepath.Rel(filepath.Clean(folder), filepath.Clean(path))
	return err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// commandOf is the command a tool call runs, from its raw input, as the runtimes send it.
func commandOf(raw wire.JSONValue) string {
	if len(raw) == 0 {
		return ""
	}
	var in map[string]any
	if json.Unmarshal(raw, &in) != nil {
		return ""
	}
	var parts []string
	for _, k := range []string{"command", "cmd", "script", "file_path", "path", "url"} {
		switch v := in[k].(type) {
		case string:
			parts = append(parts, v)
		case []any:
			for _, x := range v {
				if s, ok := x.(string); ok {
					parts = append(parts, s)
				}
			}
		}
	}
	return strings.Join(parts, " ")
}

type hardCommand struct {
	re  *regexp.Regexp
	why string
}

// hardCommands are the commands that stay with the person, by what they do. They are
// matched anywhere in the command, so a chain (a && b) or a sub-shell is caught too.
var hardCommands = []hardCommand{
	{regexp.MustCompile(`\bgit\b[^\n|;&]*\bpush\b[^\n|;&]*(\s(-f|--force|--force-with-lease|--force-if-includes|-d|--delete|--mirror|--prune)\b|\s\+\S|\s:\S)`), "force pushes, or deletes a remote branch or tag"},
	{regexp.MustCompile(`\bgit\b[^\n|;&]*\b(branch\s[^\n|;&]*(-D|-d|--delete)|tag\s[^\n|;&]*(-d|--delete))\b`), "deletes a branch or a tag"},
	{regexp.MustCompile(`\bgit\b[^\n|;&]*\b(reset\s[^\n|;&]*--hard|clean\s[^\n|;&]*-[a-zA-Z]*f|checkout\s+(--\s+)?\.(\s|$)|restore\s|stash\s+(drop|clear)|filter-branch|filter-repo|update-ref\s+-d|reflog\s+expire|gc\s[^\n|;&]*--prune)`), "throws work away (a hard reset, a clean, a stash dropped, history rewritten)"},
	{regexp.MustCompile(`(^|[\s;&|(` + "`" + `])(sudo\s+)?(rm|rmdir|unlink|shred|srm|trash)\s`), "deletes files"},
	{regexp.MustCompile(`\bfind\b[^\n|;&]*\s-delete\b`), "deletes files"},
	{regexp.MustCompile(`(^|[\s;&|(])sudo\s`), "runs as root"},
	{regexp.MustCompile(`\b(npm|pnpm|yarn|bun)\s+publish\b|\bcargo\s+publish\b|\btwine\s+upload\b|\bgem\s+push\b|\bpod\s+trunk\s+push\b|\b(docker|podman)\s+(image\s+)?push\b|\bpoetry\s+publish\b|\bgoreleaser\b|\bxcrun\s+altool\b|\bfastlane\b`), "publishes outside the repository"},
	{regexp.MustCompile(`\bgh\s+(repo\s+(create|delete|edit|rename|archive|fork)|release\s+(create|delete|edit|upload)|gist\s+create|secret\s|variable\s|api\s[^\n|;&]*(-X|--method)\s*(POST|PUT|PATCH|DELETE))\b`), "publishes or changes something outside the repository"},
	{regexp.MustCompile(`\b(curl|wget|http|xh)\b[^\n|;&]*(\s(-X|--request)\s*(POST|PUT|PATCH|DELETE)|\s(-d|--data\S*|-F|--form|-T|--upload-file|--post-data|--post-file)[\s=])`), "writes to an outside service"},
	{regexp.MustCompile(`(?i)\b(stripe|paypal|payments?|purchase|invoice)\b`), "may spend money"},
	{regexp.MustCompile(`\b(kubectl|helm|terraform|tofu|pulumi|gcloud|aws|az|doctl|flyctl|vercel|netlify|heroku)\b`), "changes outside infrastructure"},
}

// noteLeadAnswered is the record of a lead's answer (#5440): agent.permission_answered, by
// the lead's name, beside the tool call in the app-tool log.
func (d *Daemon) noteLeadAnswered(helper wire.UUID, req wire.PermissionRequest, option string, by permissionAnswerer) {
	answer := "allow"
	for _, o := range req.Options {
		if o.OptionID == option && (o.Kind == wire.PermissionOptionKindRejectOnce || o.Kind == wire.PermissionOptionKindRejectAlways) {
			answer = "deny"
		}
	}
	d.raiseAgentEvent("agent.permission_answered", helper, "had its permission request answered by “"+by.title+"”.",
		map[string]string{"by": string(by.agent), "by_title": by.title, "answer": answer, "request": string(req.ID), "tool": req.ToolCall.Title})
}
