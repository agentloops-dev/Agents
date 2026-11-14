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
		raw := call(t, window, "", "machines/list", map[string]any{})
		times = append(times, time.Since(start))
		var rows []wire.ControlMachine
		if err := json.Unmarshal(raw, &rows); err != nil {
			t.Fatal(err)
		}
		if len(rows) < n {
			t.Fatalf("machines/list: %d rows, want at least %d", len(rows), n)
		}
	}
	sort.Slice(times, func(i, j int) bool { return times[i] < times[j] })
	median := times[len(times)/2]
	// machines/list takes no limit yet (the wire's pages are #5281's second step), so this
	// times the whole list: 95 ms under -race when measured. The budget of 50 ms is for a
	// limit of 50; that step changes the call here to {"limit": 50} and the ceiling to 50 ms.
	const budget, ceiling = 50 * time.Millisecond, 500 * time.Millisecond
	t.Logf("machines/list, all %d machines: median %v (budget for a limit of 50: %v)", n, median, budget)
	if os.Getenv("CI") == "" && median > ceiling {
		t.Fatalf("machines/list at %d machines: median %v is over %v", n, median, ceiling)
	}
}
