package hostsetup

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/agentloops-dev/agent-loops/Go/internal/controllink"
	"github.com/agentloops-dev/agent-loops/Go/wire"
)

// DefaultRuntimes are the runtimes installed when none is named: the two the host installs
// from the Node toolsets the control plane carries.
var DefaultRuntimes = []string{"claude", "codex"}

// Options is one run of the installer.
type Options struct {
	// Join is the host code (agents-control:…) the control plane made for this machine.
	Join string
	// Name is this machine's name; the host's name and AGENTS_MACHINE.
	Name string
	// Home is the machine user's home; the host lives in <Home>/.agents-server.
	Home string
	// Runtimes to install; DefaultRuntimes when empty.
	Runtimes []string
	// Projects are the folders on this machine whose set-up script runs.
	Projects []string
	// FromPool is a catch-up on a machine a warm pool made: AGENTS_FROM_POOL=1.
	FromPool bool
	// Executable is the running program, which becomes the host when none is installed.
	Executable string
}

// Deps are what the installer does outside itself, so a test can stand in for each.
type Deps struct {
	// StartHost starts the host with its root and name, as a systemd user service or detached.
	StartHost func(binary, root, name string) (how string, err error)
	// Call makes one call on the running host's socket and returns its result.
	Call func(method string, params any) (json.RawMessage, error)
	// Running says whether a host holds the root.
	Running func(root string) bool
	// Poll is how often a wait looks again.
	Poll time.Duration
	// JoinWait and RuntimeWait bound the waits for the host to join and a runtime to install.
	JoinWait, RuntimeWait time.Duration
	// ScriptTimeout bounds one project's script.
	ScriptTimeout time.Duration
	Out           io.Writer
}

func (d *Deps) defaults() {
	if d.Poll == 0 {
		d.Poll = time.Second
	}
	if d.JoinWait == 0 {
		d.JoinWait = time.Minute
	}
	if d.RuntimeWait == 0 {
		d.RuntimeWait = 10 * time.Minute
	}
	if d.ScriptTimeout == 0 {
		d.ScriptTimeout = ScriptTimeout
	}
	if d.Out == nil {
		d.Out = io.Discard
	}
}

// Layout is where the host is on this machine.
type Layout struct{ Dir, Bin, Root, Logs string }

// LayoutOf is the host's folders under home, as install.sh makes them.
func LayoutOf(home string) Layout {
	d := filepath.Join(home, ".agents-server")
	return Layout{Dir: d, Bin: filepath.Join(d, "bin"), Root: filepath.Join(d, "root"), Logs: filepath.Join(d, "root", "logs")}
}

func (l Layout) joined() bool {
	_, err := os.Stat(filepath.Join(l.Root, "control-host.json"))
	return err == nil
}

// Run does steps 3 to 6 and says each. It returns the first step's error of those that
// failed, or nil when all four are done. A step that failed does not stop the one beside it
// (5 and 6 run together), but steps 5 and 6 do not start when 3 or 4 failed.
func Run(ctx context.Context, o Options, d Deps) error {
	d.defaults()
	l := LayoutOf(o.Home)
	if o.Name == "" {
		o.Name, _ = os.Hostname()
	}
	rep, err := NewReporter(l.Root)
	if err != nil {
		return err
	}
	var outMu sync.Mutex
	say := func(format string, a ...any) {
		outMu.Lock()
		defer outMu.Unlock()
		fmt.Fprintf(d.Out, format+"\n", a...)
	}
	failed := func(step wire.MachineSetupStepID, err error) error {
		rep.Say(step, wire.MachineSetupStateFailed, "", err.Error())
		say("%s: %v", step, err)
		return fmt.Errorf("%s: %w", step, err)
	}

	// 3: Install Agents.
	rep.Say(wire.MachineSetupStepIDInstall, wire.MachineSetupStateRunning, "Checking this machine", "")
	detail, err := installAgents(ctx, o, d, l, rep)
	if err != nil {
		return failed(wire.MachineSetupStepIDInstall, err)
	}
	rep.Say(wire.MachineSetupStepIDInstall, wire.MachineSetupStateDone, detail, "")
	say("Install Agents: %s", detail)

	// 4: Compare digits. The code that joined this machine is one the control plane made and
	// kept until now, so the host that joined is the host that was made (#5359 makes the
	// digits themselves).
	rep.Say(wire.MachineSetupStepIDCompare, wire.MachineSetupStateDone, "Joined with the code the control plane made for this machine", "")

	// 5 and 6, together.
	var wg sync.WaitGroup
	var mu sync.Mutex
	var first error
	keep := func(err error) {
		mu.Lock()
		if first == nil {
			first = err
		}
		mu.Unlock()
	}
	wg.Add(2)
	go func() {
		defer wg.Done()
		if err := installRuntimes(ctx, o, d, rep, say); err != nil {
			keep(failed(wire.MachineSetupStepIDRuntimes, err))
		}
	}()
	go func() {
		defer wg.Done()
		if err := prepareProjects(ctx, o, d, l, rep, say); err != nil {
			keep(failed(wire.MachineSetupStepIDProjects, err))
		}
	}()
	wg.Wait()
	return first
}

// installAgents is step 3. The host is this program's own copy, placed in ~/.agents-server/bin
// at its checksum, and started with the code, unless it is running and has joined already.
func installAgents(ctx context.Context, o Options, d Deps, l Layout, rep *Reporter) (string, error) {
	if l.joined() && d.Running(l.Root) {
		return "Agents is installed and running here already", nil
	}
	if _, err := controllink.ParseCode(o.Join); err != nil && !l.joined() {
		return "", fmt.Errorf("that is not a host code: %v", err)
	}
	if err := os.MkdirAll(l.Bin, 0o700); err != nil {
		return "", err
	}
	if err := os.MkdirAll(l.Root, 0o700); err != nil {
		return "", err
	}
	sum, err := placeBinary(o.Executable, l)
	if err != nil {
		return "", fmt.Errorf("could not put the host in place: %w", err)
	}
	if !l.joined() {
		// The host enrols with this code when it starts; a code it was refused is replaced.
		_ = os.Remove(filepath.Join(l.Root, "control-join-ask"))
		if err := os.WriteFile(filepath.Join(l.Root, "control-join-code"), []byte(strings.TrimSpace(o.Join)), 0o600); err != nil {
			return "", err
		}
	}
	how := "already running"
	if !d.Running(l.Root) {
		rep.Say(wire.MachineSetupStepIDInstall, wire.MachineSetupStateRunning, "Starting the host", "")
		if how, err = d.StartHost(filepath.Join(l.Bin, "current"), l.Root, o.Name); err != nil {
			return "", fmt.Errorf("the host did not start: %w", err)
		}
	}
	rep.Say(wire.MachineSetupStepIDInstall, wire.MachineSetupStateRunning, "Waiting for the host to join the control plane", "")
	deadline := time.Now().Add(d.JoinWait)
	for !l.joined() {
		if time.Now().After(deadline) {
			return "", fmt.Errorf("the host started but has not joined yet; its log is %s", filepath.Join(l.Root, "daemon.log"))
		}
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-time.After(d.Poll):
		}
	}
	return fmt.Sprintf("Host %s, %s, joined the control plane", sum[:8], how), nil
}

// placeBinary copies the program to bin/agentsd-<sha256> and points bin/current at it, unless
// current is that copy already. It returns the checksum.
func placeBinary(exe string, l Layout) (string, error) {
	data, err := os.ReadFile(exe)
	if err != nil {
		return "", err
	}
	h := sha256.Sum256(data)
	sum := hex.EncodeToString(h[:])
	target := "agentsd-" + sum
	if cur, err := os.Readlink(filepath.Join(l.Bin, "current")); err == nil && cur == target {
		if _, err := os.Stat(filepath.Join(l.Bin, target)); err == nil {
			return sum, nil
		}
	}
	tmp := filepath.Join(l.Bin, target+".tmp")
	if err := os.WriteFile(tmp, data, 0o700); err != nil {
		return "", err
	}
	if err := os.Rename(tmp, filepath.Join(l.Bin, target)); err != nil {
		return "", err
	}
	link := filepath.Join(l.Bin, "current.tmp")
	_ = os.Remove(link)
	if err := os.Symlink(target, link); err != nil {
		return "", err
	}
	return sum, os.Rename(link, filepath.Join(l.Bin, "current"))
}

// installRuntimes is step 5: each runtime through the host's own runtimes/install, which
// installs from the pinned toolsets, all at once, until each says it is available.
func installRuntimes(ctx context.Context, o Options, d Deps, rep *Reporter, say func(string, ...any)) error {
	ids := o.Runtimes
	if len(ids) == 0 {
		ids = DefaultRuntimes
	}
	step := wire.MachineSetupStepIDRuntimes
	rep.Say(step, wire.MachineSetupStateRunning, "Installing "+strings.Join(ids, ", "), "")
	versions := map[string]string{}
	var mu sync.Mutex
	progress := map[string]string{}
	line := func() string {
		var parts []string
		for _, id := range ids {
			if v, ok := versions[id]; ok {
				parts = append(parts, id+" "+v)
			} else if p := progress[id]; p != "" {
				parts = append(parts, id+": "+p)
			} else {
				parts = append(parts, id)
			}
		}
		return strings.Join(parts, " · ")
	}
	var wg sync.WaitGroup
	errs := make([]error, len(ids))
	for i, id := range ids {
		wg.Add(1)
		go func() {
			defer wg.Done()
			version, err := installRuntime(ctx, d, id, func(p string) {
				mu.Lock()
				progress[id] = p
				text := line()
				mu.Unlock()
				rep.Say(step, wire.MachineSetupStateRunning, text, "")
			})
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				errs[i] = fmt.Errorf("%s: %w", id, err)
				return
			}
			versions[id] = version
			rep.Say(step, wire.MachineSetupStateRunning, line(), "")
		}()
	}
	wg.Wait()
	var problems []string
	for _, err := range errs {
		if err != nil {
			problems = append(problems, err.Error())
		}
	}
	if len(problems) > 0 {
		return fmt.Errorf("%s", strings.Join(problems, "; "))
	}
	rep.Say(step, wire.MachineSetupStateDone, line(), "")
	say("Install runtimes: %s", line())
	return nil
}

// installRuntime asks the host to install one runtime and waits until it says it is
// available, or why not. It returns the version, "" when the runtime does not say.
func installRuntime(ctx context.Context, d Deps, id string, progress func(string)) (string, error) {
	status := func(method string, params any) (wire.RuntimeStatus, error) {
		raw, err := d.Call(method, params)
		if err != nil {
			return wire.RuntimeStatus{}, err
		}
		var s wire.RuntimeStatus
		return s, json.Unmarshal(raw, &s)
	}
	s, err := status(wire.CallRuntimesInstall.Name, wire.RuntimeRequest{RuntimeID: id})
	if err != nil {
		return "", err
	}
	deadline := time.Now().Add(d.RuntimeWait)
	for {
		a := s.Availability
		switch {
		case a.Available != nil || a.NeedsSignIn != nil:
			// Installed; signing in is step 7.
			if s.Version != nil {
				return *s.Version, nil
			}
			return "installed", nil
		case a.InstallFailed != nil:
			return "", fmt.Errorf("%s", a.InstallFailed.Reason)
		case a.Failed != nil:
			return "", fmt.Errorf("it did not start after it was installed")
		case a.Installing != nil && a.Installing.Progress != nil:
			progress(*a.Installing.Progress)
		}
		if time.Now().After(deadline) {
			return "", fmt.Errorf("it was not installed in %s", d.RuntimeWait)
		}
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-time.After(d.Poll):
		}
		raw, err := d.Call(wire.CallRuntimesList.Name, wire.Empty{})
		if err != nil {
			return "", err
		}
		var all []wire.RuntimeStatus
		if err := json.Unmarshal(raw, &all); err != nil {
			return "", err
		}
		for _, r := range all {
			if r.Runtime.ID == id {
				s = r
			}
		}
	}
}

// prepareProjects is step 6: each project folder's .agents/setup.sh, one after another, so
// two scripts never fight over one cache. The folders are sent by the control plane before
// this step; one that is not here fails the step, and the others still run.
func prepareProjects(ctx context.Context, o Options, d Deps, l Layout, rep *Reporter, say func(string, ...any)) error {
	step := wire.MachineSetupStepIDProjects
	if len(o.Projects) == 0 {
		rep.Say(step, wire.MachineSetupStateDone, "No projects to prepare", "")
		say("Send projects: none")
		return nil
	}
	rep.Say(step, wire.MachineSetupStateRunning, fmt.Sprintf("0 of %d projects", len(o.Projects)), "")
	_ = os.MkdirAll(l.Logs, 0o700)
	var problems []string
	ran := 0
	for i, project := range o.Projects {
		name := filepath.Base(project)
		if info, err := os.Stat(project); err != nil || !info.IsDir() {
			problems = append(problems, name+": the folder is not on this machine")
			continue
		}
		logFile, _ := os.OpenFile(filepath.Join(l.Logs, "setup-"+name+".log"), os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
		had, err := RunScript(ctx, project, o.Name, o.FromPool, d.ScriptTimeout, logFile, func(line string) {
			rep.Say(step, wire.MachineSetupStateRunning, fmt.Sprintf("%s (%d of %d): %s", name, i+1, len(o.Projects), line), "")
		})
		if logFile != nil {
			logFile.Close()
		}
		switch {
		case err != nil:
			problems = append(problems, name+": "+err.Error())
		case had:
			ran++
		}
	}
	if len(problems) > 0 {
		return fmt.Errorf("%s", strings.Join(problems, "\n"))
	}
	text := fmt.Sprintf("%d projects ready", len(o.Projects))
	if ran > 0 {
		text += fmt.Sprintf(", %d set-up scripts ran", ran)
	}
	rep.Say(step, wire.MachineSetupStateDone, text, "")
	say("Send projects: %s", text)
	return nil
}
