package controlplane

import (
	"encoding/base64"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/agentloops-dev/agent-loops/Go/internal/controlplane/providers"
	"github.com/agentloops-dev/agent-loops/Go/internal/controlplane/records"
	"github.com/agentloops-dev/agent-loops/Go/wire"
)

// machineListCap is the most machines or kept requests one answer holds (#5283).
const machineListCap = 200

// machineFacts is what machines/list filters and sorts a machine by, read without building
// its whole row; the row is built only for the machines on the page.
type machineFacts struct {
	record   records.MachineRecord
	state    string
	os       string
	provider string
	labels   []string
	version  string
	lastSeen time.Time
}

func (m *Methods) machineFactsOf(h records.MachineRecord) machineFacts {
	state, _ := m.router.Described(h.ID)["state"].(string)
	f := machineFacts{record: h, state: state, version: h.Version, lastSeen: m.router.LastSeen(h.ID)}
	if fields := strings.Fields(h.Platform); len(fields) > 0 {
		f.os = fields[0]
	}
	if link := providers.LinkOf(&h); link != nil && link.Provider != "" {
		f.provider = link.Provider
		f.labels = []string{link.Provider}
	}
	return f
}

// machineFilter is the filters of a machines/list query, ready to match.
type machineFilter struct {
	query, state, provider, os, version string
	labels                              []string
}

func newMachineFilter(q wire.MachineListQuery) machineFilter {
	f := machineFilter{query: strings.ToLower(strings.TrimSpace(deref(q.Query)))}
	f.state, f.provider, f.os, f.version = deref(q.State), deref(q.Provider), deref(q.Os), deref(q.Version)
	f.labels = q.Labels
	return f
}

func (f machineFilter) matches(m machineFacts, except string) bool {
	if except != "state" && f.state != "" && m.state != f.state {
		return false
	}
	if except != "provider" && f.provider != "" && !strings.EqualFold(m.provider, f.provider) {
		return false
	}
	if except != "os" && f.os != "" && !strings.EqualFold(m.os, f.os) {
		return false
	}
	if except != "version" && f.version != "" && m.version != f.version {
		return false
	}
	if except != "labels" {
		for _, want := range f.labels {
			if !hasLabel(m.labels, want) {
				return false
			}
		}
	}
	if f.query != "" && !m.matchesQuery(f.query) {
		return false
	}
	return true
}

func hasLabel(labels []string, want string) bool {
	for _, l := range labels {
		if strings.EqualFold(l, want) {
			return true
		}
	}
	return false
}

func (m machineFacts) matchesQuery(q string) bool {
	if strings.Contains(strings.ToLower(m.record.Name), q) || strings.Contains(strings.ToLower(m.record.ID), q) {
		return true
	}
	for _, l := range m.labels {
		if strings.Contains(strings.ToLower(l), q) {
			return true
		}
	}
	return false
}

// listMachines answers machines/list (#5283). With no filter, sort, limit or cursor it is the
// plain array of up to machineListCap machines by name, as it was; otherwise a MachineList.
func (m *Methods) listMachines(facts []machineFacts, q wire.MachineListQuery) (any, error) {
	plain := q.Query == nil && q.State == nil && len(q.Labels) == 0 && q.Provider == nil && q.Os == nil &&
		q.Version == nil && q.Sort == nil && q.Limit == nil && q.Cursor == nil
	sortKey := deref(q.Sort)
	if sortKey == "" {
		sortKey = "name"
	}
	if !slicesContains([]string{"name", "state", "lastSeen", "version"}, sortKey) {
		return nil, fail(invalidParams, "machines/list: sort is name, state, lastSeen or version.")
	}
	limit := machineListCap
	if q.Limit != nil && *q.Limit >= 1 && *q.Limit < machineListCap {
		limit = int(*q.Limit)
	}
	offset, err := pageOffset(q.Cursor)
	if err != nil {
		return nil, err
	}
	filter := newMachineFilter(q)
	var shown []machineFacts
	for _, f := range facts {
		if filter.matches(f, "") {
			shown = append(shown, f)
		}
	}
	sortMachines(shown, sortKey)
	page := shown[min(offset, len(shown)):]
	var next *string
	if len(page) > limit {
		page = page[:limit]
		c := pageCursor(offset + limit)
		next = &c
	}
	rows := make([]wire.ControlMachine, 0, len(page))
	for _, f := range page {
		rows = append(rows, m.hostRow(f.record))
	}
	if plain {
		return rows, nil
	}
	counts := wire.MachineCounts{State: map[string]int64{}, Labels: map[string]int64{}, Provider: map[string]int64{},
		Os: map[string]int64{}, Version: map[string]int64{}}
	for _, f := range facts {
		if filter.matches(f, "state") && f.state != "" {
			counts.State[f.state]++
		}
		if filter.matches(f, "labels") {
			for _, l := range f.labels {
				counts.Labels[l]++
			}
		}
		if filter.matches(f, "provider") && f.provider != "" {
			counts.Provider[f.provider]++
		}
		if filter.matches(f, "os") && f.os != "" {
			counts.Os[f.os]++
		}
		if filter.matches(f, "version") && f.version != "" {
			counts.Version[f.version]++
		}
	}
	return wire.MachineList{Machines: rows, Next: next, Total: int64(len(shown)), Counts: counts}, nil
}

func slicesContains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

// stateRank puts the machines that need a look first, when sorted by state.
func stateRank(state string) int {
	switch state {
	case "failed":
		return 0
	case "needsUpdate":
		return 1
	case "offline":
		return 2
	case "connecting":
		return 3
	case "online":
		return 4
	}
	return 5
}

func sortMachines(list []machineFacts, by string) {
	byName := func(a, b machineFacts) int {
		if c := strings.Compare(strings.ToLower(a.record.Name), strings.ToLower(b.record.Name)); c != 0 {
			return c
		}
		return strings.Compare(a.record.ID, b.record.ID)
	}
	sort.SliceStable(list, func(i, j int) bool {
		a, b := list[i], list[j]
		switch by {
		case "state":
			if ra, rb := stateRank(a.state), stateRank(b.state); ra != rb {
				return ra < rb
			}
		case "lastSeen":
			if !a.lastSeen.Equal(b.lastSeen) {
				return a.lastSeen.After(b.lastSeen)
			}
		case "version":
			if c := compareVersions(a.version, b.version); c != 0 {
				return c < 0
			}
		}
		return byName(a, b) < 0
	})
}

// compareVersions orders version strings by their numbers (1.10 after 1.9); none sorts last.
func compareVersions(a, b string) int {
	if a == b {
		return 0
	}
	if a == "" {
		return 1
	}
	if b == "" {
		return -1
	}
	pa, pb := versionParts(a), versionParts(b)
	for i := 0; i < len(pa) && i < len(pb); i++ {
		if pa[i] != pb[i] {
			if pa[i] < pb[i] {
				return -1
			}
			return 1
		}
	}
	if len(pa) != len(pb) {
		if len(pa) < len(pb) {
			return -1
		}
		return 1
	}
	return strings.Compare(a, b)
}

func versionParts(v string) []int {
	var out []int
	for _, field := range strings.FieldsFunc(v, func(r rune) bool { return !unicode.IsDigit(r) }) {
		n, _ := strconv.Atoi(field)
		out = append(out, n)
	}
	return out
}

// pageCursor and pageOffset make and read the opaque cursor of a page: where it starts.
func pageCursor(offset int) string {
	return base64.RawURLEncoding.EncodeToString([]byte(strconv.Itoa(offset)))
}

func pageOffset(cursor *string) (int, error) {
	if cursor == nil || *cursor == "" {
		return 0, nil
	}
	raw, err := base64.RawURLEncoding.DecodeString(*cursor)
	if err == nil {
		if n, convErr := strconv.Atoi(string(raw)); convErr == nil && n >= 0 {
			return n, nil
		}
	}
	return 0, fail(invalidParams, "That page cursor is not one this control plane gave.")
}
