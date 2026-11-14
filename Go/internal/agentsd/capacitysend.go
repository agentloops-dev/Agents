package agentsd

import (
	"context"
	"log"
	"math"
	"time"

	"github.com/agentloops-dev/agent-loops/Go/internal/safe"
	"github.com/agentloops-dev/agent-loops/Go/wire"
)

// machine/capacity (#5374, epic #5372): this machine tells the control plane its level and
// its 5-minute load per core, so machines/list carries them and Auto can rank machines.
// It is sent when the level moves, when the load moved by capacityLoadStep, and at least
// every capacityResend, which keeps the control plane's figure under the two minutes it
// trusts (specs/5373-auto-machine).
const (
	capacityLoadStep = 0.3
	capacityResend   = 60 * time.Second
)

// capacitySent is what the control plane was last told.
type capacitySent struct {
	level wire.CapacityLevel
	load5 float64
	at    time.Time
	sent  bool
}

// capacityDue says whether a look is worth telling: nothing told yet, a new level, a load
// that moved by the step, or the last tell is old.
func capacityDue(last capacitySent, level wire.CapacityLevel, load5 float64, now time.Time) bool {
	return !last.sent || last.level != level || math.Abs(last.load5-load5) >= capacityLoadStep ||
		now.Sub(last.at) >= capacityResend
}

// sendCapacitySoon tells the control plane, if this machine has one and the look is due.
func (d *Daemon) sendCapacitySoon(level wire.CapacityLevel, load5 float64) {
	ask := d.askControl.Load()
	if ask == nil {
		return
	}
	now := time.Now()
	d.capMu.Lock()
	if !capacityDue(d.capSent, level, load5, now) {
		d.capMu.Unlock()
		return
	}
	d.capSent = capacitySent{level: level, load5: load5, at: now, sent: true}
	d.capMu.Unlock()
	report := wire.CapacityReport{Level: level, LoadPerCore5: &load5}
	safe.Go("machine/capacity", func() {
		ctx, cancel := context.WithTimeout(d.ctx, reportPatience)
		defer cancel()
		if _, err := (*ask)(ctx, wire.CallMachineCapacity.Name, report); err != nil {
			log.Printf("machine/capacity: not sent, tried again at the next look: %v", err)
			d.capMu.Lock()
			d.capSent.sent = false
			d.capMu.Unlock()
		}
	})
}
