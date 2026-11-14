package agentsd

import (
	"crypto/ecdsa"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/agentloops-dev/agent-loops/Go/internal/machinelock"
	"github.com/agentloops-dev/agent-loops/Go/wire"
)

func peerApproval(t *testing.T, state *machinelock.State, peer *ecdsa.PrivateKey, host, kind, digest string) wire.PersonApproval {
	t.Helper()
	a := wire.PersonApproval{Kind: kind, Digest: digest, Host: host, At: time.Now().Unix(), Key: machinelock.PublicKey(peer), HostName: new("Studio")}
	sig, err := machinelock.SignData(peer, personApprovalSigned(state.Person, a))
	if err != nil {
		t.Fatal(err)
	}
	a.Sig = sig
	return a
}

// A machine takes an approval only when a machine of its own lock signed it.
func TestAMachineTakesOnlyApprovalsASignerOfItsLockMade(t *testing.T) {
	d, phone, peer, state := lockedMachine(t)
	defer d.Shutdown()
	digest := "abcd"
	good := peerApproval(t, state, peer, "h-2", approvalKindWorkflow, digest)
	if !d.takePersonApproval(state, good) {
		t.Fatal("a peer's approval was not taken")
	}
	if a, ok := d.sharedApproval(approvalKindWorkflow, digest); !ok || approvedByWords(a) != "Approved by you on Studio, "+time.Now().UTC().Format("2006-01-02") {
		t.Fatalf("%+v", a)
	}
	other := newLockKey(t)
	for name, a := range map[string]wire.PersonApproval{
		"a stranger's key":   peerApproval(t, state, other, "h-2", approvalKindWorkflow, "x1"),
		"a client's key":     peerApproval(t, state, phone, "h-2", approvalKindWorkflow, "x2"),
		"another machine id": peerApproval(t, state, peer, "h-1", approvalKindWorkflow, "x3"),
		"an unknown kind":    peerApproval(t, state, peer, "h-2", "plugin", "x4"),
	} {
		if d.takePersonApproval(state, a) {
			t.Fatalf("%s was taken", name)
		}
	}
	changed := peerApproval(t, state, peer, "h-2", approvalKindWorkflow, "x5")
	changed.Digest = "x6"
	if d.takePersonApproval(state, changed) {
		t.Fatal("an approval with a changed digest was taken")
	}
}

// takeForTest keeps an approval as a machine of the person's lock signed (the signature
// check has its own test).
func takeForTest(d *Daemon, digest string) {
	d.personApprovals.mu.Lock()
	defer d.personApprovals.mu.Unlock()
	if d.personApprovals.taken == nil {
		d.personApprovals.taken = map[string]wire.PersonApproval{}
	}
	d.personApprovals.taken[approvalTakenKey(approvalKindWorkflow, digest)] = wire.PersonApproval{Kind: approvalKindWorkflow,
		Digest: digest, Host: "h-2", HostName: new("Studio"), At: time.Now().Unix()}
}

// A new machine runs an approved, unchanged workflow with no prompt; a changed file waits,
// and so does a file the person denied here.
func TestAWorkflowApprovedElsewhereIsApprovedHereOnlyWhileItIsTheSameFile(t *testing.T) {
	d, c := start(t, t.TempDir())
	defer d.Shutdown()
	dir := folder(t, c, 1)
	d.workflowMu.Lock()
	d.scanLocked()
	d.workflowMu.Unlock()
	// Approval has begun, so a file added now waits.
	os.MkdirAll(filepath.Join(dir, ".agents", "workflows"), 0o755)
	file := filepath.Join(dir, ".agents", "workflows", "triage.md")
	os.WriteFile(file, []byte("---\non: manual\n---\nTriage the new way.\n"), 0o644)
	d.rescanWorkflows(dir)
	s, err := d.oneWorkflow(dir, "triage")
	if err != nil || s.AwaitingApproval == nil {
		t.Fatalf("%v %+v", err, s.AwaitingApproval)
	}
	digest := s.AwaitingApproval.Digest

	takeForTest(d, digest)
	d.rescanWorkflows(dir)
	s, _ = d.oneWorkflow(dir, "triage")
	if s.AwaitingApproval != nil || s.ApprovedFrom == nil || *s.ApprovedFrom == "" {
		t.Fatalf("not approved from the person's other machine: %+v %v", s.AwaitingApproval, s.ApprovedFrom)
	}

	// A change to the file has another digest: it waits again.
	os.WriteFile(file, []byte("---\non: manual\n---\nTriage and push.\n"), 0o644)
	d.rescanWorkflows(dir)
	if s, _ = d.oneWorkflow(dir, "triage"); s.AwaitingApproval == nil || s.ApprovedFrom != nil {
		t.Fatalf("a changed file did not wait: %+v", s)
	}

	// A file denied here stays denied, though the person approved it elsewhere.
	d.workflowMu.Lock()
	denied := d.fileLocked(dir, "triage").digest
	d.stateLocked(dir, "triage", true).DeniedDigest = &denied
	d.workflowMu.Unlock()
	takeForTest(d, denied)
	d.rescanWorkflows(dir)
	if s, _ = d.oneWorkflow(dir, "triage"); s.DeniedHere == nil {
		t.Fatalf("a denied file was approved: %+v", s)
	}
}

// A connector entry approved elsewhere is approved here, and one that changed is not.
func TestAConnectorApprovedElsewhereIsApprovedHereOnlyWhileItIsTheSameEntry(t *testing.T) {
	d, c := start(t, t.TempDir())
	defer d.Shutdown()
	dir := folder(t, c, 1)
	path := filepath.Join(dir, ".agents", "mcp.json")
	os.MkdirAll(filepath.Dir(path), 0o755)
	write := func(arg string) {
		os.WriteFile(path, []byte(`{"mcpServers":{"docs":{"command":"docs-server","args":["`+arg+`"]}}}`), 0o644)
	}
	write("one")
	d.mcpMu.Lock()
	d.mcpApprovalsLocked() // approval begins; this entry is approved as it stands
	d.mcpMu.Unlock()
	write("two")
	entries, _ := readMCPFile(path)
	digest := entryDigest(entries[0].raw)
	dest := wire.SkillDestination{Kind: wire.SkillDestinationKindFolder, Folder: &dir}
	list := func() wire.FolderMCPServer {
		t.Helper()
		a, err := d.mcpList(dest)
		if err != nil || len(a.Servers) != 1 {
			t.Fatalf("%v %+v", err, a)
		}
		return a.Servers[0]
	}
	if list().Approval.Waiting == nil {
		t.Fatal("a new entry did not wait")
	}
	d.personApprovals.mu.Lock()
	d.personApprovals.taken = map[string]wire.PersonApproval{approvalTakenKey(approvalKindConnector, digest): {Kind: approvalKindConnector,
		Digest: digest, Host: "h-2", HostName: new("Studio"), At: time.Now().Unix()}}
	d.personApprovals.mu.Unlock()
	if s := list(); s.Approval.Waiting != nil || s.ApprovedFrom == nil {
		t.Fatalf("not approved from the person's other machine: %+v", s)
	}
	write("three")
	if s := list(); s.Approval.Waiting == nil || s.ApprovedFrom != nil {
		t.Fatalf("a changed entry did not wait: %+v", s)
	}
}
