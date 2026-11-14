package hostsetup

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/agentloops-dev/agent-loops/Go/wire"
)

func reports(t *testing.T, l Layout) []wire.MachineSetupReport {
	t.Helper()
	data, _ := os.ReadFile(filepath.Join(l.Root, ReportsFile))
	var out []wire.MachineSetupReport
	for _, line := range bytes.Split(bytes.TrimSpace(data), []byte("\n")) {
		var r wire.MachineSetupReport
		if json.Unmarshal(line, &r) == nil {
			out = append(out, r)
		}
	}
	return out
}

func lastOf(rs []wire.MachineSetupReport, step wire.MachineSetupStepID) *wire.MachineSetupReport {
	var out *wire.MachineSetupReport
	for i := range rs {
		if rs[i].Step == step {
			out = &rs[i]
		}
	}
	return out
}

// fakeHost joins as soon as it is started and installs a runtime on the second look.
func fakeHost(t *testing.T, l Layout, started *int) Deps {
	t.Helper()
	return Deps{
		Poll: time.Millisecond, JoinWait: time.Second, RuntimeWait: time.Second,
		StartHost: func(binary, root, name string) (string, error) {
			*started++
			return "in the background", os.WriteFile(filepath.Join(root, "control-host.json"), []byte("{}"), 0o600)
		},
		Running: func(root string) bool { return *started > 0 },
		Call: func(method string, params any) (json.RawMessage, error) {
			switch method {
			case wire.CallRuntimesInstall.Name:
				p := "Downloading"
				return json.Marshal(wire.RuntimeStatus{Runtime: wire.Runtime{ID: params.(wire.RuntimeRequest).RuntimeID},
					Availability: wire.RuntimeAvailability{Installing: &wire.RuntimeAvailabilityInstalling{Progress: &p}}})
			case wire.CallRuntimesList.Name:
				var all []wire.RuntimeStatus
				for _, id := range []string{"claude", "codex"} {
					v := "1.2." + id
					all = append(all, wire.RuntimeStatus{Runtime: wire.Runtime{ID: id}, Version: &v,
						Availability: wire.RuntimeAvailability{Available: &wire.RuntimeAvailabilityAvailable{Path: "/x"}}})
				}
				return json.Marshal(all)
			}
			t.Fatalf("unexpected call %s", method)
			return nil, nil
		},
	}
}

func options(t *testing.T) (Options, Layout) {
	home := t.TempDir()
	exe := filepath.Join(t.TempDir(), "agentsd")
	os.WriteFile(exe, []byte("the host"), 0o700)
	// A code with the right shape is not needed: the join file is written only for a parsed one,
	// so a test that has no real code joins by an existing membership.
	return Options{Join: "agents-control:x", Name: "dev-2", Home: home, Executable: exe}, LayoutOf(home)
}

func TestRunWithAnAlreadyJoinedHostSetsUpAndIsSafeToRunAgain(t *testing.T) {
	o, l := options(t)
	os.MkdirAll(l.Root, 0o700)
	os.WriteFile(filepath.Join(l.Root, "control-host.json"), []byte("{}"), 0o600)
	project := t.TempDir()
	os.MkdirAll(filepath.Join(project, ".agents"), 0o700)
	os.WriteFile(filepath.Join(project, SetupScript), []byte("echo \"$AGENTS_SETUP $AGENTS_MACHINE $AGENTS_FROM_POOL\" > out.txt\necho warmed\n"), 0o600)
	o.Projects = []string{project}
	started := 1 // running already
	deps := fakeHost(t, l, &started)
	var out bytes.Buffer
	deps.Out = &out

	for run := 0; run < 2; run++ {
		if err := Run(context.Background(), o, deps); err != nil {
			t.Fatalf("run %d: %v\n%s", run, err, out.String())
		}
		rs := reports(t, l)
		for _, step := range []wire.MachineSetupStepID{wire.MachineSetupStepIDInstall, wire.MachineSetupStepIDCompare, wire.MachineSetupStepIDRuntimes, wire.MachineSetupStepIDProjects} {
			if r := lastOf(rs, step); r == nil || r.State != wire.MachineSetupStateDone {
				t.Fatalf("run %d: step %s = %+v", run, step, r)
			}
		}
		if got := *lastOf(rs, wire.MachineSetupStepIDRuntimes).Detail; got != "claude 1.2.claude · codex 1.2.codex" {
			t.Errorf("runtimes detail = %q", got)
		}
	}
	b, _ := os.ReadFile(filepath.Join(project, "out.txt"))
	if strings.TrimSpace(string(b)) != "1 dev-2 0" {
		t.Errorf("the script saw %q", b)
	}
	if started != 1 {
		t.Errorf("a running host was started again")
	}
}

func TestInstallRefusesWhatIsNotAHostCode(t *testing.T) {
	o, l := options(t)
	started := 0
	err := Run(context.Background(), o, fakeHost(t, l, &started))
	if err == nil || !strings.Contains(err.Error(), "not a host code") {
		t.Fatalf("err = %v", err)
	}
	r := lastOf(reports(t, l), wire.MachineSetupStepIDInstall)
	if r == nil || r.State != wire.MachineSetupStateFailed || r.Error == nil {
		t.Fatalf("install = %+v", r)
	}
	if lastOf(reports(t, l), wire.MachineSetupStepIDRuntimes) != nil {
		t.Error("runtimes started after a failed install")
	}
}

func TestAFailedScriptFailsStepSixWithItsLastLinesAndStepFiveStillRuns(t *testing.T) {
	o, l := options(t)
	os.MkdirAll(l.Root, 0o700)
	os.WriteFile(filepath.Join(l.Root, "control-host.json"), []byte("{}"), 0o600)
	bad := t.TempDir()
	os.MkdirAll(filepath.Join(bad, ".agents"), 0o700)
	os.WriteFile(filepath.Join(bad, SetupScript), []byte("echo one\nfalse\necho never\n"), 0o600)
	o.Projects = []string{bad, filepath.Join(t.TempDir(), "missing")}
	started := 1
	err := Run(context.Background(), o, fakeHost(t, l, &started))
	if err == nil {
		t.Fatal("no error")
	}
	rs := reports(t, l)
	p := lastOf(rs, wire.MachineSetupStepIDProjects)
	if p.State != wire.MachineSetupStateFailed || !strings.Contains(*p.Error, "one") || strings.Contains(*p.Error, "never") ||
		!strings.Contains(*p.Error, "is not on this machine") {
		t.Errorf("projects = %+v", p)
	}
	if r := lastOf(rs, wire.MachineSetupStepIDRuntimes); r.State != wire.MachineSetupStateDone {
		t.Errorf("runtimes = %+v", r)
	}
}

func TestRunScriptHoldsNoCredentialsAndStopsAtTheTimeout(t *testing.T) {
	t.Setenv("GITHUB_TOKEN", "secret")
	project := t.TempDir()
	os.MkdirAll(filepath.Join(project, ".agents"), 0o700)
	os.WriteFile(filepath.Join(project, SetupScript), []byte(`[ -z "$GITHUB_TOKEN" ] || exit 7
exec sleep 30
`), 0o600)
	start := time.Now()
	had, err := RunScript(context.Background(), project, "m", false, 300*time.Millisecond, nil, nil)
	if !had || err == nil || !strings.Contains(err.Error(), "ran longer than") {
		t.Fatalf("had=%v err=%v", had, err)
	}
	if time.Since(start) > 10*time.Second {
		t.Error("the timeout did not stop it")
	}
	had, err = RunScript(context.Background(), t.TempDir(), "m", false, time.Second, nil, nil)
	if had || err != nil {
		t.Errorf("a project with no script: had=%v err=%v", had, err)
	}
}

func TestPlaceBinaryIsRepeatable(t *testing.T) {
	o, l := options(t)
	os.MkdirAll(l.Bin, 0o700)
	a, err := placeBinary(o.Executable, l)
	if err != nil {
		t.Fatal(err)
	}
	b, err := placeBinary(o.Executable, l)
	if err != nil || a != b {
		t.Fatalf("%v %s %s", err, a, b)
	}
	if got, _ := os.Readlink(filepath.Join(l.Bin, "current")); got != "agentsd-"+a {
		t.Errorf("current -> %s", got)
	}
}
