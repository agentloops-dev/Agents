package providers

import (
	"encoding/json"
	"fmt"
	"testing"

	"github.com/agentloops-dev/agent-loops/Go/wire"
)

// machines/listKept pages, filters and counts (#5283).
func TestListKeptPagesFiltersAndCounts(t *testing.T) {
	t.Parallel()
	var all []KeptRequest
	for i := range 450 {
		kind := "prompt"
		if i%3 == 0 {
			kind = "start"
		}
		all = append(all, KeptRequest{Host: wire.MachineRef(fmt.Sprintf("h%d", i%5)), Kind: kind, Text: fmt.Sprintf("job %d", i)})
	}
	plain, err := listKept(all, nil)
	if err != nil || len(plain.Kept) != keptCap || plain.Next == nil {
		t.Fatalf("no params: %d, next %v, %v", len(plain.Kept), plain.Next, err)
	}
	next, _ := json.Marshal(wire.KeptListQuery{Cursor: plain.Next})
	rest, err := listKept(all, next)
	if err != nil || len(rest.Kept) != keptCap || rest.Kept[0].Text != "job 200" {
		t.Fatalf("second page: %d %v", len(rest.Kept), err)
	}
	filtered, _ := listKept(all, json.RawMessage(`{"kind":"start","query":"job 4"}`))
	for _, r := range filtered.Kept {
		if r.Kind != "start" {
			t.Fatalf("%+v passed kind=start", r)
		}
	}
	if filtered.Counts.Kind["prompt"] == 0 || filtered.Counts.Kind["start"] != int64(len(filtered.Kept)) {
		t.Fatalf("counts %v for %d rows", filtered.Counts.Kind, len(filtered.Kept))
	}
	if _, err := listKept(all, json.RawMessage(`{"cursor":"%%"}`)); err == nil {
		t.Fatal("a bad cursor was taken")
	}
}
