package agentsd

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"log"
	"sync"
	"time"

	"github.com/agentloops-dev/agent-loops/Go/internal/lineage"
	"github.com/agentloops-dev/agent-loops/Go/wire"
)

// The control plane's copy of the deploys (#5496, spec 5239-deploy-model): this machine stays
// the system of record (lineage-deploys.json, #5241) and sends the control plane every deploy
// it keeps, as deploys/delivery's figures need them (deliveryFacts), so Reports ▸ Planning ▸
// Delivery works for a group or org scope and while the machine sleeps (principle 1), as the
// build timings' copy does (#5232). Only refs, names, states and times (principle 7).
//
//   - Each send is whole, in place of the copy: the store keeps at most deploysKept deploys,
//     so a send is at most that many facts, and a dropped or changed deploy goes the same way
//     as a new one.
//   - It goes a few seconds after a deploy event or a deploy's brings are worked out, on the
//     hourly clock (a health signal seen in a deploy), and when a control plane says what it
//     can do on a new link; only when it differs from the last send, but always on a new link.
//   - Sending works out what the deploys of the last 90 days bring, deliveryAsksAtOnce at a
//     time, so the copy has lead time without a page open on this machine.

const deploySyncAfter = 5 * time.Second

type deploySyncBook struct {
	deploySyncMu    sync.Mutex
	deploySyncTimer *time.Timer
	// deploySent is the hash of the last send the control plane took, once deployOnce.
	deploySent [sha256.Size]byte
	deployOnce bool
	// deploySending keeps one send at a time.
	deploySending sync.Mutex
}

// deploySyncSoon sends within deploySyncAfter, with what else changes by then.
func (d *Daemon) deploySyncSoon() {
	if d.askControl.Load() == nil {
		return
	}
	d.deploySyncMu.Lock()
	defer d.deploySyncMu.Unlock()
	if d.deploySyncTimer == nil {
		d.deploySyncTimer = time.AfterFunc(deploySyncAfter, func() {
			d.deploySyncMu.Lock()
			d.deploySyncTimer = nil
			d.deploySyncMu.Unlock()
			d.sendDeploys(false)
		})
	}
}

// sendDeploys sends every deploy this machine keeps, if this host has a control plane that
// takes them and they changed since the last send (again, with again). A failed send is tried
// again on the next change, the next hour or the next link.
func (d *Daemon) sendDeploys(again bool) {
	ask := d.askControl.Load()
	if ask == nil || !d.controlHas(wire.CapDeploysCopy1) {
		return
	}
	d.deploySending.Lock()
	defer d.deploySending.Unlock()
	since := wire.DateOf(nowFunc().Add(-lineage.DeliveryFactDays * 24 * time.Hour))
	facts, pending := d.deliveryFacts(since)
	send := wire.DeploysSync{Deploys: facts}
	if pending {
		send.Resolving = &pending
	}
	data, err := json.Marshal(send)
	if err != nil {
		log.Printf("deploys/sync: not sent: %v", err)
		return
	}
	sum := sha256.Sum256(data)
	d.deploySyncMu.Lock()
	same := d.deployOnce && d.deploySent == sum
	d.deploySyncMu.Unlock()
	if same && !again {
		return
	}
	ctx, cancel := context.WithTimeout(d.ctx, reportPatience)
	_, err = (*ask)(ctx, wire.CallDeploysSync.Name, send)
	cancel()
	if err != nil {
		log.Printf("deploys/sync: not sent, tried again later: %v", err)
		return
	}
	d.deploySyncMu.Lock()
	d.deploySent, d.deployOnce = sum, true
	d.deploySyncMu.Unlock()
}

// closeDeploySync stops a send that waits; the next start sends again.
func (d *Daemon) closeDeploySync() {
	d.deploySyncMu.Lock()
	if d.deploySyncTimer != nil {
		d.deploySyncTimer.Stop()
		d.deploySyncTimer = nil
	}
	d.deploySyncMu.Unlock()
}
