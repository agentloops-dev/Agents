package controlplane

import (
	"context"
	"encoding/json"
	"os"
	"sort"
	"testing"
	"time"

	"github.com/agentloops-dev/agent-loops/Go/internal/controllink"
	"github.com/agentloops-dev/agent-loops/Go/internal/controlplane/store"
	"github.com/agentloops-dev/agent-loops/Go/wire"
)

// The scale fixture (#5282): 1,000 fake machines, and the budget for listing them
// (scripts/perf-budgets.json, "scale"). The count is held on every run; the time only off
// CI, whose shared runners measure the load (docs/how-to/keep-the-performance-budgets.md).
func TestScaleFixtureListsAThousandMachinesWithinBudget(t *testing.T) {
	t.Parallel()
	const n = 1000
	st := store.NewMemory()
	if err := SeedFakeMachines(context.Background(), st, n); err != nil {
		t.Fatal(err)
	}
	p := startPlane(t, st, controllink.NewPrivateKey())
	_, window := p.client("Window")
	var times []time.Duration
	for range 7 {
		start := time.Now()
		raw := call(t, window, "", "machines/list", map[string]any{"limit": 50})
		times = append(times, time.Since(start))
		var page wire.MachineList
		if err := json.Unmarshal(raw, &page); err != nil {
			t.Fatal(err)
		}
		if len(page.Machines) != 50 || page.Total != n || page.Next == nil {
			t.Fatalf("machines/list limit 50: %d rows, total %d, next %v", len(page.Machines), page.Total, page.Next)
		}
	}
	sort.Slice(times, func(i, j int) bool { return times[i] < times[j] })
	median := times[len(times)/2]
	// The budget of scripts/perf-budgets.json, "scale": a page of 50 within 50 ms. Under -race
	// on a loaded box the ceiling is looser; the time is only held off CI.
	const budget = 50 * time.Millisecond
	t.Logf("machines/list, limit 50 of %d machines: median %v (budget %v)", n, median, budget)
	if os.Getenv("CI") == "" && median > 4*budget {
		t.Fatalf("machines/list at %d machines: median %v is over %v", n, median, 4*budget)
	}
	// With no arguments it is the array, up to the cap, as older clients read it.
	var rows []wire.ControlMachine
	if err := json.Unmarshal(call(t, window, "", "machines/list", map[string]any{}), &rows); err != nil || len(rows) != machineListCap {
		t.Fatalf("machines/list with no arguments: %d rows (%v), want %d", len(rows), err, machineListCap)
	}
}

// Paging, filters, sorts and counts (#5283) over the fixture's machines.
func TestMachinesListFiltersSortsPagesAndCounts(t *testing.T) {
	t.Parallel()
	st := store.NewMemory()
	if err := SeedFakeMachines(context.Background(), st, 1000); err != nil {
		t.Fatal(err)
	}
	p := startPlane(t, st, controllink.NewPrivateKey())
	_, window := p.client("Window")
	list := func(q map[string]any) wire.MachineList {
		t.Helper()
		var page wire.MachineList
		if err := json.Unmarshal(call(t, window, "", "machines/list", q), &page); err != nil {
			t.Fatal(err)
		}
		return page
	}

	// Every machine once, by name, through the cursors.
	seen, cursor := map[string]bool{}, ""
	for pages := 0; ; pages++ {
		q := map[string]any{"limit": 200}
		if cursor != "" {
			q["cursor"] = cursor
		}
		page := list(q)
		for _, m := range page.Machines {
			if seen[string(m.ID)] {
				t.Fatalf("%s on two pages", m.ID)
			}
			seen[string(m.ID)] = true
		}
		if page.Next == nil {
			if pages != 4 {
				t.Fatalf("%d pages, want 5", pages+1)
			}
			break
		}
		cursor = *page.Next
	}
	if len(seen) != 1000 {
		t.Fatalf("%d machines through the pages", len(seen))
	}

	// A filter, a query, and the counts leave out only their own filter.
	page := list(map[string]any{"os": "linux", "version": "0.10.0", "limit": 1000})
	for _, m := range page.Machines {
		if m.Version != "0.10.0" || len(m.Platform) < 5 || m.Platform[:5] != "linux" {
			t.Fatalf("%+v passed os=linux version=0.10.0", m)
		}
	}
	if int64(len(page.Machines)) != page.Total || page.Total == 0 || page.Total >= 1000 {
		t.Fatalf("total %d, rows %d", page.Total, len(page.Machines))
	}
	if page.Counts.Os["linux"] != page.Total || page.Counts.Os["darwin"] == 0 {
		t.Fatalf("os counts %v: linux is the filter's own, so darwin counts what picking it would show", page.Counts.Os)
	}
	if page.Counts.Version["0.10.0"] == 0 || page.Counts.Version["0.9.0"] == 0 {
		t.Fatalf("counts %+v", page.Counts)
	}
	one := list(map[string]any{"query": "FAKE MACHINE 0042"})
	if one.Total != 1 || one.Machines[0].ID != "fake-0042" {
		t.Fatalf("query: %+v", one.Total)
	}
	if none := list(map[string]any{"state": "online"}); none.Total != 0 || none.Counts.State["offline"] != 1000 {
		t.Fatalf("online: total %d, counts %v", none.Total, none.Counts.State)
	}

	// Sorts: by version, the numbers in order (0.10.0 after 0.9.1).
	byVersion := list(map[string]any{"sort": "version", "limit": 200})
	if byVersion.Machines[0].Version != "0.9.0" || list(map[string]any{"sort": "version", "limit": 200, "cursor": pageCursor(900)}).Machines[99].Version != "0.10.0" {
		t.Fatalf("version sort starts at %s", byVersion.Machines[0].Version)
	}
	if byName := list(map[string]any{"sort": "name", "limit": 2}); byName.Machines[0].ID != "fake-0001" {
		t.Fatalf("name sort: %v", byName.Machines[0].ID)
	}

	// A bad sort or cursor is refused.
	if _, err := window.Call(context.Background(), "", "machines/list", map[string]any{"sort": "size"}); err == nil {
		t.Fatal("a sort that is not one was taken")
	}
	if _, err := window.Call(context.Background(), "", "machines/list", map[string]any{"cursor": "!!"}); err == nil {
		t.Fatal("a cursor that is not one was taken")
	}
}
