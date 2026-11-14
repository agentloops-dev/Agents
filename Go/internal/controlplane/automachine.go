package controlplane

import (
	"cmp"
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/agentloops-dev/agent-loops/Go/wire"
)

// Auto picks a machine for a new session (#5374, epic #5372, specs/5373-auto-machine). The
// control plane holds each machine's capacity (pushed in machine/capacity), its folders
// (projects/joined) and the person's last start, so it decides; a client draws the answer.

// capacityTrusted is how old a pushed figure may be before Auto stops trusting it: the
// machine then counts as ok, last among the ok ones.
const capacityTrusted = 2 * time.Minute

// runtimePatience is how long Auto waits for a machine's runtimes/list.
const runtimePatience = 3 * time.Second

type capacityNote struct {
	level wire.CapacityLevel
	load5 *float64
	at    time.Time
}

// autoBook is what Auto keeps, in memory: each machine's last capacity push, and the
// machine each person last started each project on.
type autoBook struct {
	autoMu   sync.Mutex
	capacity map[string]capacityNote
	lastUsed map[string]string
	// runtimeReady is set by tests; nil asks the machine.
	runtimeReady func(host, runtime string) bool
}

func lastUsedKey(person, project string) string { return person + "\x00" + project }

func (b *autoBook) noteCapacity(host string, r wire.CapacityReport, now time.Time) {
	b.autoMu.Lock()
	defer b.autoMu.Unlock()
	if b.capacity == nil {
		b.capacity = map[string]capacityNote{}
	}
	b.capacity[host] = capacityNote{level: r.Level, load5: r.LoadPerCore5, at: now}
}

func (b *autoBook) capacityOf(host string) (capacityNote, bool) {
	b.autoMu.Lock()
	defer b.autoMu.Unlock()
	n, ok := b.capacity[host]
	return n, ok
}

func (b *autoBook) forgetCapacity(host string) {
	b.autoMu.Lock()
	delete(b.capacity, host)
	b.autoMu.Unlock()
}

func (b *autoBook) noteStart(person, project, host string) {
	b.autoMu.Lock()
	defer b.autoMu.Unlock()
	if b.lastUsed == nil {
		b.lastUsed = map[string]string{}
	}
	b.lastUsed[lastUsedKey(person, project)] = host
}

func (b *autoBook) lastStart(person, project string) string {
	b.autoMu.Lock()
	defer b.autoMu.Unlock()
	return b.lastUsed[lastUsedKey(person, project)]
}

// autoMachine is one of the person's machines as Auto sees it.
type autoMachine struct {
	ID, Name     string
	HasFolder    bool
	Online       bool
	RuntimeReady bool
	// Level and Load5 are the last push; Heard is when, zero for none.
	Level wire.CapacityLevel
	Load5 *float64
	Heard time.Time
}

// known is whether the machine has a figure Auto trusts.
func (m autoMachine) known(now time.Time) bool {
	return m.Level != "" && !m.Heard.IsZero() && now.Sub(m.Heard) <= capacityTrusted
}

func (m autoMachine) tier(now time.Time) wire.CapacityLevel {
	if !m.known(now) {
		return wire.CapacityLevelOk
	}
	return m.Level
}

var tierOrder = map[wire.CapacityLevel]int{wire.CapacityLevelOk: 0, wire.CapacityLevelNear: 1, wire.CapacityLevelFull: 2}

// autoPick is the rule of specs/5373-auto-machine as a pure function: from the person's
// machines, the one the person last started this project on ("" for none) and the runtime
// asked for, the pick or why none fits.
func autoPick(machines []autoMachine, lastUsed, runtime string, now time.Time) wire.AutoMachinePick {
	var withFolder []autoMachine
	for _, m := range machines {
		if m.HasFolder {
			withFolder = append(withFolder, m)
		}
	}
	single := len(withFolder) == 1
	none := func(why wire.AutoMachineNone, text string, offline []string) wire.AutoMachinePick {
		out := wire.AutoMachinePick{None: &why, Text: &text, Single: &single}
		if offline != nil {
			out.Offline = offline
		}
		return out
	}
	if len(withFolder) == 0 {
		return none(wire.AutoMachineNoneNoFolder, "No machine has this project. Add a folder on one.", nil)
	}
	var online []autoMachine
	var offline []string
	for _, m := range withFolder {
		if m.Online {
			online = append(online, m)
		} else {
			offline = append(offline, m.Name)
		}
	}
	if len(online) == 0 {
		slices.Sort(offline)
		return none(wire.AutoMachineNoneOffline, "Every machine with this project is offline: "+strings.Join(offline, ", ")+".", offline)
	}
	var cands []autoMachine
	for _, m := range online {
		if m.RuntimeReady {
			cands = append(cands, m)
		}
	}
	if len(cands) == 0 {
		return none(wire.AutoMachineNoneNoRuntime, "No machine with this project has "+runtime+" installed and signed in.", nil)
	}
	best := slices.MinFunc(cands, func(a, b autoMachine) int {
		if c := cmp.Compare(tierOrder[a.tier(now)], tierOrder[b.tier(now)]); c != 0 {
			return c
		}
		if c := cmp.Compare(b2i(a.ID != lastUsed), b2i(b.ID != lastUsed)); c != 0 {
			return c
		}
		// A machine with no trusted figure is last among those in its tier.
		if c := cmp.Compare(b2i(!a.known(now) || a.Load5 == nil), b2i(!b.known(now) || b.Load5 == nil)); c != 0 {
			return c
		}
		if a.known(now) && b.known(now) && a.Load5 != nil && b.Load5 != nil {
			if c := cmp.Compare(*a.Load5, *b.Load5); c != 0 {
				return c
			}
		}
		return cmp.Compare(a.Name, b.Name)
	})
	tier := best.tier(now)
	out := wire.AutoMachinePick{Machine: new(wire.MachineRef), MachineName: &best.Name, Tier: &tier, Single: &single}
	*out.Machine = wire.MachineRef(best.ID)
	var load string
	if best.known(now) && best.Load5 != nil {
		out.LoadPerCore = best.Load5
		load = fmt.Sprintf("load %.1f per core", *best.Load5)
	}
	reason := wire.AutoMachineReasonLeastLoaded
	if best.ID == lastUsed {
		reason = wire.AutoMachineReasonLastUsed
	}
	out.Reason = &reason
	var text string
	switch {
	case tier == wire.CapacityLevelOk && reason == wire.AutoMachineReasonLastUsed:
		text = "you last started here."
	case tier == wire.CapacityLevelOk && load != "":
		text = "least busy, " + load + "."
	case tier == wire.CapacityLevelOk:
		text = "least busy."
	default:
		how := "near capacity"
		if tier == wire.CapacityLevelFull {
			how = "at capacity"
		}
		lead := "the least busy"
		if reason == wire.AutoMachineReasonLastUsed {
			lead = "you last started here"
		}
		text = lead + ", though " + how
		if load != "" {
			text += " (" + load + ")"
		}
		text += "."
	}
	text = "Auto chose " + best.Name + ": " + text
	out.Text = &text
	return out
}

func b2i(b bool) int {
	if b {
		return 1
	}
	return 0
}

// previewStart is sessions/previewStart: what Auto would pick for the caller now.
func (m *Methods) previewStart(ctx context.Context, request wire.AutoMachineRequest, from Caller) (wire.AutoMachinePick, error) {
	person := m.callerPerson(from)
	if !wire.ValidProjectID(request.Project) {
		return wire.AutoMachinePick{}, fail(invalidParams, "%q is not a project id.", request.Project)
	}
	joined, err := m.s.joinedFolders(ctx)
	if err != nil {
		return wire.AutoMachinePick{}, err
	}
	now := time.Now()
	var machines []autoMachine
	for _, h := range m.records.Hosts() {
		if !m.owns(from, h.ID) {
			continue
		}
		am := autoMachine{ID: h.ID, Name: h.Name}
		for _, f := range joined[h.ID] {
			if f.Project == request.Project {
				am.HasFolder = true
				break
			}
		}
		if am.HasFolder {
			state, _ := m.router.State(h.ID)
			am.Online = state.online()
			if n, ok := m.s.capacityOf(h.ID); ok {
				am.Level, am.Load5, am.Heard = n.level, n.load5, n.at
			}
		}
		machines = append(machines, am)
	}
	m.s.fillRuntimes(machines, request.RuntimeID)
	return autoPick(machines, m.s.lastStart(person, request.Project), request.RuntimeID, now), nil
}

// fillRuntimes asks each online machine with the folder whether the runtime is installed
// and signed in there, all at once.
func (s *Service) fillRuntimes(machines []autoMachine, runtime string) {
	var wg sync.WaitGroup
	for i := range machines {
		if !machines[i].HasFolder || !machines[i].Online {
			continue
		}
		wg.Add(1)
		go func(m *autoMachine) {
			defer wg.Done()
			m.RuntimeReady = s.runtimeReadyOn(m.ID, runtime)
		}(&machines[i])
	}
	wg.Wait()
}

func (s *Service) runtimeReadyOn(host, runtime string) bool {
	if s.autoBook.runtimeReady != nil {
		return s.autoBook.runtimeReady(host, runtime)
	}
	reply := s.router.Ask(host, "runtimes/list", map[string]any{}, runtimePatience)
	if reply == nil {
		return false
	}
	var list []wire.RuntimeStatus
	if json.Unmarshal(reply.Result, &list) != nil {
		return false
	}
	for _, r := range list {
		if r.Runtime.ID == runtime && r.Availability.Available != nil {
			return true
		}
	}
	return false
}

// noteStarted records, for Auto's rank 1, the machine a person started a session on: an
// agents/start on a host whose cwd is in one of its folders of a project.
func (s *Service) noteStarted(person, host string, params json.RawMessage) {
	var start struct {
		Cwd string `json:"cwd"`
	}
	if person == "" || json.Unmarshal(params, &start) != nil || start.Cwd == "" {
		return
	}
	joined, err := s.joinedFolders(context.Background())
	if err != nil {
		return
	}
	for _, f := range joined[host] {
		folder := strings.TrimSuffix(string(f.Folder), "/")
		if start.Cwd == string(f.Folder) || start.Cwd == folder || strings.HasPrefix(start.Cwd, folder+"/") {
			s.noteStart(person, f.Project, host)
			return
		}
	}
}
