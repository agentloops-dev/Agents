package agentsd

import (
	"bytes"
	"context"
	"encoding/json"
	"log"
	"os"
	"path/filepath"
	"time"

	"github.com/agentloops-dev/agent-loops/Go/internal/hostsetup"
	"github.com/agentloops-dev/agent-loops/Go/internal/safe"
	"github.com/agentloops-dev/agent-loops/Go/wire"
)

// setupForwardEvery is how often the host looks for a step the installer has said.
const setupForwardEvery = 2 * time.Second

// setupForwardTries is how often one report is sent again when the control plane did not take
// it, before it is let go: a report refused for good (a step said twice) is not worth more.
const setupForwardTries = 5

// StartSetupReports sends each step `agentsd install --join` says (setup-reports.jsonl in the
// root, #5356) to the control plane as machine/setupStep. The installer starts this host, so
// what it said before the host had joined is sent when the host has.
func (d *Daemon) StartSetupReports() {
	safe.Go("setup reports", func() {
		d.forwardSetupReports(d.ctx, filepath.Join(d.Store.Loc.Root, hostsetup.ReportsFile), setupForwardEvery)
	})
}

// forwardSetupReports tails file until ctx ends.
func (d *Daemon) forwardSetupReports(ctx context.Context, file string, every time.Duration) {
	var offset int64
	tries := 0
	prelude := false
	for {
		select {
		case <-ctx.Done():
			return
		case <-time.After(every):
		}
		ask := d.askControl.Load()
		if ask == nil {
			continue
		}
		data, err := os.ReadFile(file)
		if err != nil {
			continue
		}
		if int64(len(data)) < offset {
			// The installer began a new run.
			offset, prelude = 0, false
		}
		for {
			rest := data[offset:]
			end := bytes.IndexByte(rest, '\n')
			if end < 0 {
				break
			}
			var report wire.MachineSetupReport
			if json.Unmarshal(rest[:end], &report) != nil {
				offset += int64(end) + 1
				continue
			}
			if !prelude {
				// This host has joined, so its machine was made and reached: an installer
				// pasted on a machine the control plane did not make says so too. A step said
				// again, or done already, is not an error to the control plane.
				for _, step := range []wire.MachineSetupStepID{wire.MachineSetupStepIDCreate, wire.MachineSetupStepIDJoin} {
					_, _ = (*ask)(ctx, wire.CallMachineSetupStep.Name, wire.MachineSetupReport{Step: step, State: wire.MachineSetupStateDone})
				}
				prelude = true
			}
			if _, err := (*ask)(ctx, wire.CallMachineSetupStep.Name, report); err != nil {
				if tries++; tries < setupForwardTries {
					break // the same line again at the next look
				}
				log.Printf("setup: step %s was not taken by the control plane: %v", report.Step, err)
			}
			tries = 0
			offset += int64(end) + 1
		}
	}
}
