package agentsd

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	sdk "github.com/coder/acp-go-sdk"

	"github.com/agentloops-dev/agent-loops/Go/wire"
)

// leadAndHelper is a lead in mode and a helper it started, with the helper's live state
// registered, as start_session leaves them.
func leadAndHelper(t *testing.T, d *Daemon, mode string) (wire.Agent, wire.Agent, *live) {
	t.Helper()
	folder := t.TempDir()
	cat := "mode"
	lead := seeded(t, d, folder, func(a *wire.Agent) {
		a.AdvertisedOptions = []wire.ConfigOption{{ID: "mode", Name: "Mode", Category: &cat, Type: "select", CurrentValue: wire.JSONValue(`"` + mode + `"`)}}
		title := "Lead"
		a.Title = &title
	})
	helper := seeded(t, d, folder, func(a *wire.Agent) {
		a.StartedByAgent = &lead.ID
		title := "Helper"
		a.Title = &title
	})
	return lead, helper, d.live(helper.ID)
}

// askCard opens a card on the helper in the background, and gives back the option chosen.
func askCard(t *testing.T, l *live, kind sdk.ToolKind, title string, raw any, paths ...string) (wire.UUID, chan string) {
	t.Helper()
	var locs []sdk.ToolCallLocation
	for _, p := range paths {
		locs = append(locs, sdk.ToolCallLocation{Path: p})
	}
	got := make(chan string, 1)
	go func() {
		got <- l.Permission(context.Background(), sdk.RequestPermissionRequest{
			ToolCall: sdk.ToolCallUpdate{Title: &title, Kind: &kind, RawInput: raw, Locations: locs},
			Options: []sdk.PermissionOption{
				{Kind: sdk.PermissionOptionKindAllowAlways, Name: "Always", OptionId: "always"},
				{Kind: sdk.PermissionOptionKindAllowOnce, Name: "Yes", OptionId: "yes"},
				{Kind: sdk.PermissionOptionKindRejectOnce, Name: "No", OptionId: "no"},
			}})
	}()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		l.mu.Lock()
		for id := range l.pending {
			l.mu.Unlock()
			return id, got
		}
		l.mu.Unlock()
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("no card opened")
	return "", nil
}

func answerArgs(id wire.UUID, answer string) map[string]json.RawMessage {
	return map[string]json.RawMessage{"id": json.RawMessage(`"` + string(id) + `"`), "answer": json.RawMessage(`"` + answer + `"`)}
}

// #5440: a lead allows its helper's edit once, the card closes, and the chat and the
// events say the lead chose.
func TestALeadAllowsItsHelpersEditOnce(t *testing.T) {
	d, _ := start(t, t.TempDir())
	defer d.Shutdown()
	lead, helper, l := leadAndHelper(t, d, "acceptEdits")
	_, got := askCard(t, l, sdk.ToolKindEdit, "Edit main.go", map[string]any{"file_path": "main.go"})
	said, err := d.answerPermission(lead.ID, answerArgs(helper.ID, "allow"))
	if err != nil {
		t.Fatal(err)
	}
	if option := <-got; option != "yes" {
		t.Fatalf("allow once, never always: %q", option)
	}
	if !strings.Contains(said, "Allowed “Edit main.go” for “Helper”") {
		t.Fatal(said)
	}
	entries, _, _ := d.Store.Transcript(helper.ID, 0)
	var answered *wire.TranscriptEntryKindPermissionAnswered
	for _, e := range entries {
		if e.Kind.PermissionAnswered != nil {
			answered = e.Kind.PermissionAnswered
		}
	}
	if answered == nil || answered.AnsweredBy == nil || *answered.AnsweredBy != lead.ID || deref(answered.AnsweredByTitle) != "Lead" {
		t.Fatalf("%+v", answered)
	}
	if len(d.pendingPermissions()) != 0 {
		t.Fatal("the card is still open")
	}
	found := false
	d.eventMu.Lock()
	for _, e := range d.eventList {
		if e.Name == "agent.permission_answered" && e.Details["by"] == string(lead.ID) && e.Details["answer"] == "allow" {
			found = true
		}
	}
	d.eventMu.Unlock()
	if !found {
		t.Fatal("no agent.permission_answered")
	}
}

// A lead's mode bounds what it allows; it can still deny. A step that is hard to undo
// stays with the person, allow or deny.
func TestALeadAllowsOnlyWhatItsModeDoesAndNeverAHardStep(t *testing.T) {
	d, _ := start(t, t.TempDir())
	defer d.Shutdown()
	lead, helper, l := leadAndHelper(t, d, "acceptEdits")
	id, got := askCard(t, l, sdk.ToolKindExecute, "go test ./...", map[string]any{"command": "go test ./..."})
	if _, err := d.answerPermission(lead.ID, answerArgs(helper.ID, "allow")); err == nil || !strings.Contains(err.Error(), "would ask the person for this too") {
		t.Fatalf("acceptEdits does not run commands: %v", err)
	}
	if _, err := d.answerPermission(lead.ID, answerArgs(helper.ID, "deny")); err != nil {
		t.Fatal(err)
	}
	if option := <-got; option != "no" {
		t.Fatalf("%q", option)
	}
	_ = id

	for _, c := range []struct {
		kind  sdk.ToolKind
		title string
		raw   map[string]any
		path  string
	}{
		{sdk.ToolKindExecute, "git push --force origin main", map[string]any{"command": "git push --force origin main"}, ""},
		{sdk.ToolKindExecute, "Run", map[string]any{"command": "cd x && rm -rf build"}, ""},
		{sdk.ToolKindExecute, "npm publish", map[string]any{"command": "npm publish"}, ""},
		{sdk.ToolKindExecute, "Run", map[string]any{"command": "git push origin :old-branch"}, ""},
		{sdk.ToolKindDelete, "Delete notes.md", nil, ""},
		{sdk.ToolKindEdit, "Edit zshrc", nil, "/etc/zshrc"},
	} {
		lead, helper, l := leadAndHelper(t, d, "bypassPermissions")
		var paths []string
		if c.path != "" {
			paths = []string{c.path}
		}
		_, got := askCard(t, l, c.kind, c.title, c.raw, paths...)
		for _, answer := range []string{"allow", "deny"} {
			if _, err := d.answerPermission(lead.ID, answerArgs(helper.ID, answer)); err == nil || !strings.Contains(err.Error(), "hard to undo") {
				t.Fatalf("%s %s: %v", c.title, answer, err)
			}
		}
		if !l.answer(firstPending(l), "no") {
			t.Fatal("the person can still answer it")
		}
		<-got
	}

	// In bypassPermissions, a routine command is the lead's to allow.
	lead2, helper2, l2 := leadAndHelper(t, d, "bypassPermissions")
	_, got2 := askCard(t, l2, sdk.ToolKindExecute, "go test ./...", map[string]any{"command": "go test ./..."})
	if _, err := d.answerPermission(lead2.ID, answerArgs(helper2.ID, "allow")); err != nil {
		t.Fatal(err)
	}
	if option := <-got2; option != "yes" {
		t.Fatalf("%q", option)
	}
}

// Only the session that started it answers a helper's card; the person's own answer still
// wins when it comes first.
func TestOnlyTheStarterAnswersAndThePersonCanBeFirst(t *testing.T) {
	d, _ := start(t, t.TempDir())
	defer d.Shutdown()
	lead, helper, l := leadAndHelper(t, d, "bypassPermissions")
	other := seeded(t, d, t.TempDir(), nil)
	id, got := askCard(t, l, sdk.ToolKindRead, "Read a.go", nil)
	if _, err := d.answerPermission(other.ID, answerArgs(helper.ID, "allow")); err == nil || !strings.Contains(err.Error(), "a session you started") {
		t.Fatalf("%v", err)
	}
	if !l.answer(id, "no") {
		t.Fatal("the person's answer")
	}
	if option := <-got; option != "no" {
		t.Fatalf("%q", option)
	}
	if _, err := d.answerPermission(lead.ID, answerArgs(helper.ID, "allow")); err == nil || !strings.Contains(err.Error(), "not waiting") {
		t.Fatalf("%v", err)
	}
}

func firstPending(l *live) wire.UUID {
	l.mu.Lock()
	defer l.mu.Unlock()
	for id := range l.pending {
		return id
	}
	return ""
}

func TestHardToUndoKeepsRoutineCommands(t *testing.T) {
	for _, cmd := range []string{"go test ./...", "git push origin agents/5440", "git commit -m 'x'", "git checkout -b x", "npm run build", "gh pr create --fill"} {
		raw, _ := json.Marshal(map[string]string{"command": cmd})
		kind := "execute"
		if why := hardToUndo(wire.ToolCall{Title: cmd, Kind: &kind, RawInput: raw}, "/tmp/f"); why != "" {
			t.Errorf("%s: %s", cmd, why)
		}
	}
}
