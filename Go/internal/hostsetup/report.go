// Package hostsetup is `agentsd install --join` (#5356, epic #5353): the one installer that
// sets a machine up. It runs steps 3 to 6 of the machine set-up spec on the machine (install
// Agents, compare digits, install runtimes, send and prepare projects, 5 and 6 together) and
// says each step as it starts, goes on and ends. It is safe to run again: it does only what
// is missing.
//
// It says each step by appending a line to <root>/setup-reports.jsonl. The host, once it has
// joined, sends each line to the control plane as machine/setupStep (agentsd's
// setupforward.go), so a step said before the host had joined is said when it has.
package hostsetup

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"

	"github.com/agentloops-dev/agent-loops/Go/wire"
)

// ReportsFile is the file the installer says its steps in, in the host's root.
const ReportsFile = "setup-reports.jsonl"

// Reporter appends steps to the reports file. It is safe for several goroutines (steps 5 and
// 6 run together).
type Reporter struct {
	mu   sync.Mutex
	path string
	last map[wire.MachineSetupStepID]string
}

// NewReporter starts a run's reports afresh: lines of an earlier run are not said again.
func NewReporter(root string) (*Reporter, error) {
	if err := os.MkdirAll(root, 0o700); err != nil {
		return nil, err
	}
	path := filepath.Join(root, ReportsFile)
	if err := os.WriteFile(path, nil, 0o600); err != nil {
		return nil, err
	}
	return &Reporter{path: path, last: map[wire.MachineSetupStepID]string{}}, nil
}

// Say records one step's state. The same words said twice in a row are said once.
func (r *Reporter) Say(step wire.MachineSetupStepID, state wire.MachineSetupState, detail, problem string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	report := wire.MachineSetupReport{Step: step, State: state}
	if detail != "" {
		report.Detail = &detail
	}
	if problem != "" {
		report.Error = &problem
	}
	line, err := json.Marshal(report)
	if err != nil || r.last[step] == string(line) {
		return
	}
	r.last[step] = string(line)
	f, err := os.OpenFile(r.path, os.O_APPEND|os.O_WRONLY|os.O_CREATE, 0o600)
	if err != nil {
		return
	}
	defer f.Close()
	_, _ = f.Write(append(line, '\n'))
}
