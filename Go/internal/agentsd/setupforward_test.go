package agentsd

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/agentloops-dev/agent-loops/Go/internal/hostsetup"
	"github.com/agentloops-dev/agent-loops/Go/wire"
)

func TestSetupReportsAreSentInOrderAfterTheHostHasJoined(t *testing.T) {
	file := filepath.Join(t.TempDir(), hostsetup.ReportsFile)
	rep, err := hostsetup.NewReporter(filepath.Dir(file))
	if err != nil {
		t.Fatal(err)
	}
	rep.Say(wire.MachineSetupStepIDInstall, wire.MachineSetupStateRunning, "Starting", "")
	rep.Say(wire.MachineSetupStepIDInstall, wire.MachineSetupStateDone, "Up", "")

	d := &Daemon{}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go d.forwardSetupReports(ctx, file, time.Millisecond)
	time.Sleep(20 * time.Millisecond) // not joined: nothing is lost

	var mu sync.Mutex
	var got []string
	refuse := true
	var ask askControl = func(_ context.Context, method string, params any) (json.RawMessage, error) {
		r := params.(wire.MachineSetupReport)
		mu.Lock()
		defer mu.Unlock()
		if method != wire.CallMachineSetupStep.Name {
			t.Errorf("method %s", method)
		}
		if r.Step == wire.MachineSetupStepIDInstall && r.State == wire.MachineSetupStateRunning && refuse {
			refuse = false
			return nil, errors.New("the control plane did not answer")
		}
		got = append(got, string(r.Step)+":"+string(r.State))
		return json.RawMessage("{}"), nil
	}
	d.askControl.Store(&ask)
	want := []string{"create:done", "join:done", "install:running", "install:done"}
	for i := 0; i < 200; i++ {
		mu.Lock()
		n := len(got)
		mu.Unlock()
		if n >= len(want) {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(got) < len(want) {
		t.Fatalf("got %v", got)
	}
	for i, w := range want {
		if got[i] != w {
			t.Fatalf("got %v, want %v", got, want)
		}
	}
	_ = os.Remove(file)
}
