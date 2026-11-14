package hostsetup

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/agentloops-dev/agent-loops/Go/internal/procgroup"
)

// SetupScript is a project's own set-up, in its repository (spec, "The project's set-up script").
const SetupScript = ".agents/setup.sh"

// ScriptTimeout is how long a set-up script may run.
const ScriptTimeout = 10 * time.Minute

// tailLines is how many of a script's last lines a failure says.
const tailLines = 5

// scriptEnv is the environment a script runs in: the machine user's basics and the four
// AGENTS_ names, and nothing else, so it holds no credential of the installer's (spec: "No
// credentials").
func scriptEnv(project, machine string, fromPool bool) []string {
	env := []string{"AGENTS_SETUP=1", "AGENTS_PROJECT=" + project, "AGENTS_MACHINE=" + machine, "AGENTS_FROM_POOL=0"}
	if fromPool {
		env[3] = "AGENTS_FROM_POOL=1"
	}
	for _, name := range []string{"HOME", "USER", "LOGNAME", "PATH", "LANG", "LC_ALL", "TERM", "TMPDIR", "SHELL"} {
		if v, ok := os.LookupEnv(name); ok {
			env = append(env, name+"="+v)
		}
	}
	return env
}

// tail keeps the last lines written to it, and each line as it comes.
type tail struct {
	mu    sync.Mutex
	lines []string
	each  func(string)
}

func (t *tail) add(line string) {
	line = strings.TrimSpace(line)
	if line == "" {
		return
	}
	t.mu.Lock()
	t.lines = append(t.lines, line)
	if len(t.lines) > tailLines {
		t.lines = t.lines[1:]
	}
	t.mu.Unlock()
	if t.each != nil {
		t.each(line)
	}
}

func (t *tail) last() string {
	t.mu.Lock()
	defer t.mu.Unlock()
	return strings.Join(t.lines, "\n")
}

// RunScript runs project's .agents/setup.sh with `bash -e`, as this user, in the project's
// folder. Its output goes in full to log and, line by line, to progress. It returns false and
// nil when the project has no script. A non-zero exit, or more than timeout, is an error that
// holds the last lines of output.
func RunScript(ctx context.Context, project, machine string, fromPool bool, timeout time.Duration, log *os.File, progress func(string)) (bool, error) {
	script := filepath.Join(project, SetupScript)
	if info, err := os.Stat(script); err != nil || info.IsDir() {
		return false, nil
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "bash", "-e", script)
	cmd.Dir = project
	cmd.Env = scriptEnv(project, machine, fromPool)
	procgroup.Own(cmd)
	out, err := cmd.StdoutPipe()
	if err != nil {
		return true, err
	}
	cmd.Stderr = cmd.Stdout
	if err := cmd.Start(); err != nil {
		return true, fmt.Errorf("could not start bash: %w", err)
	}
	seen := &tail{each: progress}
	sc := bufio.NewScanner(out)
	sc.Buffer(make([]byte, 64*1024), 1<<20)
	for sc.Scan() {
		if log != nil {
			_, _ = log.Write(append(bytes.Clone(sc.Bytes()), '\n'))
		}
		seen.add(sc.Text())
	}
	err = cmd.Wait()
	procgroup.End(cmd)
	switch {
	case errors.Is(ctx.Err(), context.DeadlineExceeded):
		return true, fmt.Errorf("%s ran longer than %s and was stopped.\n%s", SetupScript, timeout, seen.last())
	case err != nil:
		return true, fmt.Errorf("%s failed (%v).\n%s", SetupScript, err, seen.last())
	}
	return true, nil
}
