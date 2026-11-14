package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/agentloops-dev/agent-loops/Go/internal/agentsd/store"
	"github.com/agentloops-dev/agent-loops/Go/internal/hostsetup"
)

const installUsage = `usage: agentsd install --join <host code> [--name NAME] [--runtime ID]... [--project DIR]... [--from-pool]

Sets this machine up (#5356): installs this host, joins the control plane, installs the
runtimes, and runs each project's .agents/setup.sh. Each step is reported to the control plane
as it ends. Run it again to do only what is missing.`

// installCommand is `agentsd install --join <code>`, the one installer.
func installCommand(args []string) int {
	var o hostsetup.Options
	for i := 0; i < len(args); i++ {
		next := func() string {
			if i+1 < len(args) {
				i++
				return args[i]
			}
			return ""
		}
		switch a := args[i]; {
		case a == "--join":
			o.Join = next()
		case a == "--name":
			o.Name = next()
		case a == "--runtime":
			o.Runtimes = append(o.Runtimes, next())
		case a == "--project":
			o.Projects = append(o.Projects, next())
		case a == "--from-pool":
			o.FromPool = true
		case strings.HasPrefix(a, "--join="):
			o.Join = strings.TrimPrefix(a, "--join=")
		default:
			fmt.Fprintln(os.Stderr, installUsage)
			return 2
		}
	}
	if o.Join == "" {
		fmt.Fprintln(os.Stderr, installUsage)
		return 2
	}
	var err error
	if o.Home, err = os.UserHomeDir(); err != nil {
		fmt.Fprintln(os.Stderr, "no home folder:", err)
		return 1
	}
	if o.Executable, err = os.Executable(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	for i, p := range o.Projects {
		o.Projects[i], _ = filepath.Abs(p)
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	socket := store.At(hostsetup.LayoutOf(o.Home).Root).Socket()
	deps := hostsetup.Deps{
		StartHost: startHost,
		Running:   func(root string) bool { return running(store.At(root).Lock(), 0) },
		Call: func(method string, params any) (json.RawMessage, error) {
			return callSocketResult(socket, method, params)
		},
		Out: os.Stdout,
	}
	if err := hostsetup.Run(ctx, o, deps); err != nil {
		fmt.Fprintln(os.Stderr, "The machine is not set up:", err)
		return 1
	}
	fmt.Println("This machine is set up.")
	return 0
}

// startHost starts the host as install.sh does: a systemd user service where systemd runs
// (an /run/systemd/system folder; a codespace's systemctl is a shim, #3366), else detached.
func startHost(binary, root, name string) (string, error) {
	home, _ := os.UserHomeDir()
	if _, err := os.Stat("/run/systemd/system"); err == nil {
		if _, err := exec.LookPath("systemctl"); err == nil && exec.Command("systemctl", "--user", "show-environment").Run() == nil {
			unit := filepath.Join(home, ".config", "systemd", "user", "agents-host.service")
			text := fmt.Sprintf(`[Unit]
Description=Agent Loops host (%s)
After=network-online.target

[Service]
ExecStart=%%h/.agents-server/bin/current --root %%h/.agents-server/root --serve --control-network --host-name %q
Restart=always
RestartSec=5

[Install]
WantedBy=default.target
`, name, name)
			if err := os.MkdirAll(filepath.Dir(unit), 0o700); err != nil {
				return "", err
			}
			if err := os.WriteFile(unit, []byte(text), 0o600); err != nil {
				return "", err
			}
			for _, step := range [][]string{{"daemon-reload"}, {"enable", "agents-host.service"}, {"restart", "agents-host.service"}} {
				if out, err := exec.Command("systemctl", append([]string{"--user"}, step...)...).CombinedOutput(); err != nil {
					return "", fmt.Errorf("systemctl %s: %v: %s", step[0], err, strings.TrimSpace(string(out)))
				}
			}
			return "as the systemd user service agents-host", nil
		}
	}
	cmd := exec.Command(binary, "--root", root, "--serve", "--detach", "--control-network", "--host-name", name)
	if out, err := cmd.CombinedOutput(); err != nil {
		return "", fmt.Errorf("%v: %s", err, strings.TrimSpace(string(out)))
	}
	return "in the background", nil
}
