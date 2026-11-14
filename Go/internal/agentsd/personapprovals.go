package agentsd

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/agentloops-dev/agent-loops/Go/internal/agentsd/store"
	"github.com/agentloops-dev/agent-loops/Go/internal/machinelock"
	"github.com/agentloops-dev/agent-loops/Go/internal/safe"
	"github.com/agentloops-dev/agent-loops/Go/wire"
)

// A person's approvals (#5462, epic #5281). An approval of a workflow file or a connector's
// entry goes with the person and the file's content hash, not with one machine: a machine
// that approves signs a record with its lock key and sends it to the control plane
// (approvals/put); another machine of the same person takes it, once its own pinned lock
// names the signer, for a file with the same digest. A changed file has another digest, so a
// push to the repo still cannot change what runs without the person's OK, and a file that
// no one approved is never taken. Deny on This Host stays local. A machine with no control
// plane, or no lock, asks as before; so does a bot's machine for a workflow it only hears.

const (
	approvalKindWorkflow  = "workflow"
	approvalKindConnector = "connector"
	approvalsPollEvery    = 5 * time.Minute
	approvalDomain        = "agents-approval/1"
)

type personApprovals struct {
	mu sync.Mutex
	// taken is each verified approval, by kind and digest.
	taken map[string]wire.PersonApproval
}

func approvalTakenKey(kind, digest string) string { return kind + "|" + digest }

// personApprovalSigned is what a machine signs.
func personApprovalSigned(person string, a wire.PersonApproval) []byte {
	return fmt.Appendf(nil, "%s\n%s\n%s\n%s\n%s\n%d", approvalDomain, person, a.Kind, a.Digest, a.Host, a.At)
}

// signPersonApproval is this machine's approval of a file, or false with no lock, key or id.
func (d *Daemon) signPersonApproval(kind, digest, name string) (wire.PersonApproval, bool) {
	state, pinned := d.heldLock()
	me := d.ownHostID()
	if !pinned || state == nil || me == "" {
		return wire.PersonApproval{}, false
	}
	signer, err := d.machineSigner()
	if err != nil {
		return wire.PersonApproval{}, false
	}
	a := wire.PersonApproval{Kind: kind, Digest: digest, Host: me, At: nowFunc().Unix(), Key: machinelock.PublicKey(signer)}
	if name != "" {
		a.Name = &name
	}
	for _, m := range state.Members(wire.LockEntryKindAddMachine) {
		if m.ID == me && m.Name != "" {
			n := m.Name
			a.HostName = &n
		}
	}
	a.Sig, err = machinelock.SignData(signer, personApprovalSigned(state.Person, a))
	return a, err == nil
}

// takePersonApproval is an approval from the control plane, kept only when a machine of
// this person's lock signed it: the control plane can store any bytes.
func (d *Daemon) takePersonApproval(state *machinelock.State, a wire.PersonApproval) bool {
	if a.Kind != approvalKindWorkflow && a.Kind != approvalKindConnector || a.Digest == "" {
		return false
	}
	m, ok := state.Member(a.Key)
	if !ok || m.Kind != wire.LockEntryKindAddMachine || m.ID != a.Host {
		return false
	}
	if !machinelock.VerifyData(a.Key, personApprovalSigned(state.Person, a), a.Sig) {
		return false
	}
	if a.HostName == nil && m.Name != "" {
		n := m.Name
		a.HostName = &n
	}
	d.personApprovals.mu.Lock()
	defer d.personApprovals.mu.Unlock()
	if d.personApprovals.taken == nil {
		d.personApprovals.taken = map[string]wire.PersonApproval{}
	}
	k := approvalTakenKey(a.Kind, a.Digest)
	if old, had := d.personApprovals.taken[k]; had && old.At <= a.At {
		return true
	}
	d.personApprovals.taken[k] = a
	return true
}

// sharedApproval is the person's approval of a file with this digest, if a machine signed one.
func (d *Daemon) sharedApproval(kind, digest string) (wire.PersonApproval, bool) {
	d.personApprovals.mu.Lock()
	defer d.personApprovals.mu.Unlock()
	a, ok := d.personApprovals.taken[approvalTakenKey(kind, digest)]
	return a, ok
}

func (d *Daemon) anySharedApprovals() bool {
	d.personApprovals.mu.Lock()
	defer d.personApprovals.mu.Unlock()
	return len(d.personApprovals.taken) > 0
}

// approvedByWords is "Approved by you on Studio, 2026-11-14".
func approvedByWords(a wire.PersonApproval) string {
	where := a.Host
	if a.HostName != nil && *a.HostName != "" {
		where = *a.HostName
	}
	return "Approved by you on " + where + ", " + time.Unix(a.At, 0).UTC().Format("2006-01-02")
}

// sharePersonApproval tells the person's other machines of an approval made here.
func (d *Daemon) sharePersonApproval(kind, digest, name string) {
	if d.askControl.Load() == nil {
		return
	}
	safe.Go("share an approval", func() {
		if !d.controlHas(wire.CapApprovals1) {
			return
		}
		a, ok := d.signPersonApproval(kind, digest, name)
		if !ok {
			return
		}
		d.askApprovals(wire.CallApprovalsPut.Name, wire.PersonApprovalsPut{Approvals: []wire.PersonApproval{a}})
	})
}

// refreshPersonApprovals reads the person's approvals, takes the signed ones, and approves
// here each waiting file that one covers.
func (d *Daemon) refreshPersonApprovals() {
	if d.askControl.Load() == nil || !d.controlHas(wire.CapApprovals1) {
		return
	}
	d.askApprovals(wire.CallApprovalsList.Name, wire.Empty{})
}

func (d *Daemon) askApprovals(method string, params any) {
	ask := d.askControl.Load()
	if ask == nil {
		return
	}
	ctx, cancel := context.WithTimeout(d.ctx, reportPatience)
	defer cancel()
	out, err := (*ask)(ctx, method, params)
	if err != nil {
		log.Printf("%s: %v", method, err)
		return
	}
	var list wire.PersonApprovals
	if json.Unmarshal(bytes.TrimSpace(out), &list) != nil {
		return
	}
	state, pinned := d.heldLock()
	if !pinned || state == nil {
		return
	}
	for _, a := range list.Approvals {
		d.takePersonApproval(state, a)
	}
	d.adoptPersonApprovals()
}

// adoptPersonApprovals approves here every waiting workflow and connector that a taken
// approval covers.
func (d *Daemon) adoptPersonApprovals() {
	if !d.anySharedApprovals() {
		return
	}
	d.workflowMu.Lock()
	d.scanLocked() // adopts the workflows (adoptSharedWorkflowsLocked)
	d.workflowMu.Unlock()
	d.adoptSharedMCPAll()
	d.tellEveryWorkflow()
}

// watchPersonApprovals reads the person's approvals now and every few minutes.
func (d *Daemon) watchPersonApprovals() {
	safe.Go("person approvals", func() {
		t := time.NewTicker(approvalsPollEvery)
		defer t.Stop()
		for {
			select {
			case <-d.ctx.Done():
				return
			case <-t.C:
				d.refreshPersonApprovals()
			}
		}
	})
}

// adoptSharedWorkflowsLocked approves each workflow file that waits here and that the person
// approved on another machine, by the digest of the file with its skills. A file the person
// denied here, one never approved by anyone, one that changed, and one heard for a bot
// (#2027) are left to wait. With workflowMu held.
func (d *Daemon) adoptSharedWorkflowsLocked(files map[string]map[string]*workflowFile) {
	if d.workflowRecsBad || !d.anySharedApprovals() {
		return
	}
	changed := false
	for folder, ws := range files {
		for id, f := range ws {
			if f.digest == "" || !d.listedHere(f.wf) || d.hearsFor(f.wf) != "" {
				continue
			}
			a, ok := d.sharedApproval(approvalKindWorkflow, f.digest)
			if !ok {
				continue
			}
			home := approvalFolderOf(folder, f)
			if st := d.stateLocked(home, id, false); st != nil {
				if st.ApprovedDigest != nil && *st.ApprovedDigest == f.digest || st.DeniedDigest != nil && *st.DeniedDigest == f.digest {
					continue
				}
			}
			st := d.stateLocked(home, id, true)
			d.keepCopyLocked(st, f)
			from := approvedByWords(a)
			st.ApprovedFrom = &from
			if here := d.stateLocked(folder, id, false); here != nil {
				if o := here.LastOutcome; o != nil && o.Refused != nil && o.Refused.Value.AwaitingApproval != nil {
					here.LastOutcome = nil
				}
			}
			changed = true
			log.Printf("workflows: %s approved here, as the person approved it on %s", id, a.Host)
		}
	}
	if changed {
		d.saveRecordsLocked(true)
	}
}

// adoptSharedMCPAll is adoptSharedMCPFolder for every folder here.
func (d *Daemon) adoptSharedMCPAll() {
	if !d.anySharedApprovals() {
		return
	}
	changed := false
	for _, p := range d.folders(false) {
		if d.adoptSharedMCPFolder(store.FolderPath(p.Folder.Folder)) {
			changed = true
		}
	}
	if changed {
		d.mcpChanged()
	}
}

// adoptSharedMCPFolder approves each connector entry of the folder's .agents/mcp.json that
// waits here and that the person approved on another machine: the same entry digest. An
// entry the person never approved, or that changed, keeps waiting. It says whether it
// approved one.
func (d *Daemon) adoptSharedMCPFolder(folder string) bool {
	if !d.anySharedApprovals() {
		return false
	}
	entries, problem := readMCPFile(filepath.Join(folder, ".agents", "mcp.json"))
	if problem != "" {
		return false
	}
	d.mcpMu.Lock()
	defer d.mcpMu.Unlock()
	approved, _, unreadable := d.mcpApprovalsLocked()
	if unreadable || approved == nil {
		return false
	}
	from := map[string]string{}
	json.Unmarshal(d.mcpApprovals["from"], &from)
	changed := false
	for _, e := range entries {
		digest, key := entryDigest(e.raw), mcpApprovalKey(folder, e.name)
		if approved[key] == digest {
			continue
		}
		a, ok := d.sharedApproval(approvalKindConnector, digest)
		if !ok {
			continue
		}
		approved[key] = digest
		from[key] = digest + "|" + approvedByWords(a)
		changed = true
		log.Printf("mcp: %s in %s approved here, as the person approved it on %s", e.name, folder, a.Host)
	}
	if !changed {
		return false
	}
	d.mcpApprovals["approved"], _ = json.Marshal(approved)
	d.mcpApprovals["from"], _ = json.Marshal(from)
	if err := d.saveMCPApprovalsLocked(false); err != nil {
		log.Printf("mcp: could not record the shared approvals: %v", err)
	}
	return true
}

// mcpApprovedFrom is where an approved entry's approval came from, when it is another
// machine's of the same person and still of this digest. With mcpMu held.
func (d *Daemon) mcpApprovedFromLocked(folder, name, digest string) *string {
	if d.mcpApprovals == nil {
		return nil
	}
	from := map[string]string{}
	json.Unmarshal(d.mcpApprovals["from"], &from)
	if v, ok := from[mcpApprovalKey(folder, name)]; ok {
		if d, words, found := strings.Cut(v, "|"); found && d == digest {
			return &words
		}
	}
	return nil
}
