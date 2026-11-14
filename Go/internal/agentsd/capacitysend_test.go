package agentsd

import (
	"testing"
	"time"

	"github.com/agentloops-dev/agent-loops/Go/wire"
)

func TestCapacityDue(t *testing.T) {
	now := time.Now()
	last := capacitySent{level: wire.CapacityLevelOk, load5: 0.5, at: now, sent: true}
	cases := []struct {
		name  string
		last  capacitySent
		level wire.CapacityLevel
		load  float64
		after time.Duration
		want  bool
	}{
		{"never sent", capacitySent{}, wire.CapacityLevelOk, 0.5, 0, true},
		{"nothing moved", last, wire.CapacityLevelOk, 0.6, time.Second, false},
		{"level moved", last, wire.CapacityLevelNear, 0.5, time.Second, true},
		{"load moved by the step", last, wire.CapacityLevelOk, 0.8, time.Second, true},
		{"old", last, wire.CapacityLevelOk, 0.5, capacityResend, true},
	}
	for _, c := range cases {
		if got := capacityDue(c.last, c.level, c.load, now.Add(c.after)); got != c.want {
			t.Errorf("%s: %v, want %v", c.name, got, c.want)
		}
	}
}
