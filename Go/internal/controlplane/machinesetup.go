package controlplane

import (
	"context"
	"encoding/json"
	"sort"
	"strings"
	"time"

	"github.com/agentloops-dev/agent-loops/Go/internal/controlplane/store"
	"github.com/agentloops-dev/agent-loops/Go/wire"
)

// A machine's set-up (#5355, epic #5353, "The steps" of the machine set-up spec): eight steps
// the control plane keeps for each machine, in order, with each one's start and end. The
// installer on the machine (agents-host install --join) says each step as machine/setupStep;
// the control plane makes step 1 itself and calls BeginSetup. A failed machine is kept, not
// deleted, and machines/retrySetup runs the failed step and the ones after it.
//
// One object a machine, v1/machine-setup/<host>.json. The durations of finished steps, for
// "Slower than usual", are in v1/machine-setup-times.json, the last mostSetupTimes of each step.

const (
	machineSetupPrefix = "v1/machine-setup/"
	machineSetupTimes  = "v1/machine-setup-times.json"
	// mostSetupTimes bounds the durations kept for each step: the oldest go first.
	mostSetupTimes = 50
	// slowFactor and slowFloor: a running step is slower than usual after 3 times its median,
	// and at least 2 minutes.
	slowFactor = 3
	slowFloor  = 2 * time.Minute
)

// setupSteps are the eight steps, in order.
var setupSteps = []wire.MachineSetupStepID{
	wire.MachineSetupStepIDCreate, wire.MachineSetupStepIDJoin, wire.MachineSetupStepIDInstall,
	wire.MachineSetupStepIDCompare, wire.MachineSetupStepIDRuntimes, wire.MachineSetupStepIDProjects,
	wire.MachineSetupStepIDSignIn, wire.MachineSetupStepIDSmoke,
}

// storedSetupStep is a step as kept: times as times.
type storedSetupStep struct {
	ID       wire.MachineSetupStepID `json:"id"`
	State    wire.MachineSetupState  `json:"state"`
	Started  *time.Time              `json:"startedAt,omitempty"`
	Ended    *time.Time              `json:"endedAt,omitempty"`
	Detail   string                  `json:"detail,omitempty"`
	Error    string                  `json:"error,omitempty"`
	FromPool bool                    `json:"fromPool,omitempty"`
}

type storedSetup struct {
	Steps   []storedSetupStep `json:"steps"`
	Started time.Time         `json:"startedAt"`
	Ready   *time.Time        `json:"readyAt,omitempty"`
}

func machineSetupKey(host string) string {
	return machineSetupPrefix + strings.ToLower(host) + ".json"
}

func newStoredSetup(now time.Time) *storedSetup {
	out := &storedSetup{Started: now}
	for _, id := range setupSteps {
		out.Steps = append(out.Steps, storedSetupStep{ID: id, State: wire.MachineSetupStateWaiting})
	}
	return out
}

func stepIndex(id wire.MachineSetupStepID) int {
	for i, s := range setupSteps {
		if s == id {
			return i
		}
	}
	return -1
}

// canStart says whether step i may begin or end now: every step before it is done, except
// that steps 5 and 6 (runtimes and projects) run together.
func (st *storedSetup) canStart(i int) bool {
	for j := 0; j < i; j++ {
		if (i == 5 && j == 4) || (i == 4 && j == 5) {
			continue
		}
		if st.Steps[j].State != wire.MachineSetupStateDone {
			return false
		}
	}
	return true
}

func (st *storedSetup) failed() bool {
	for _, s := range st.Steps {
		if s.State == wire.MachineSetupStateFailed {
			return true
		}
	}
	return false
}

// report applies one step's report at now and returns the duration of a step that is done.
func (st *storedSetup) report(r wire.MachineSetupReport, now time.Time) (time.Duration, bool, error) {
	i := stepIndex(r.Step)
	if i < 0 {
		return 0, false, fail(invalidParams, "There is no set-up step %q.", r.Step)
	}
	step := &st.Steps[i]
	detail, problem := "", ""
	if r.Detail != nil {
		detail = *r.Detail
	}
	if r.Error != nil {
		problem = *r.Error
	}
	if r.FromPool != nil && *r.FromPool {
		step.FromPool = true
	}
	if step.State == r.State {
		// Said again, as a progress line.
		step.Detail = detail
		step.Error = problem
		return 0, false, nil
	}
	if step.State == wire.MachineSetupStateDone {
		return 0, false, fail(invalidParams, "Step %s is done already.", r.Step)
	}
	switch r.State {
	case wire.MachineSetupStateWaiting:
		return 0, false, fail(invalidParams, "A step is made to wait again by Try Again.")
	case wire.MachineSetupStateFailed:
		// It stops the run: a step that never began has its start at the failure.
	default:
		if st.failed() || !st.canStart(i) {
			return 0, false, fail(invalidParams, "Step %s cannot be %s before the steps ahead of it are done.", r.Step, r.State)
		}
	}
	if step.Started == nil {
		t := now
		step.Started = &t
	}
	step.State, step.Detail, step.Error = r.State, detail, problem
	var took time.Duration
	finished := false
	if r.State == wire.MachineSetupStateDone || r.State == wire.MachineSetupStateFailed {
		t := now
		step.Ended = &t
		if r.State == wire.MachineSetupStateDone {
			took, finished = now.Sub(*step.Started), true
		}
	} else {
		step.Ended = nil
	}
	if st.Steps[len(st.Steps)-1].State == wire.MachineSetupStateDone && st.canStart(len(st.Steps)) {
		t := now
		st.Ready = &t
	}
	return took, finished, nil
}

// retry makes the first failed step, and every one after it, wait again.
func (st *storedSetup) retry() bool {
	for i := range st.Steps {
		if st.Steps[i].State != wire.MachineSetupStateFailed {
			continue
		}
		for j := i; j < len(st.Steps); j++ {
			st.Steps[j] = storedSetupStep{ID: st.Steps[j].ID, State: wire.MachineSetupStateWaiting}
		}
		st.Ready = nil
		return true
	}
	return false
}

// medianOf is the middle of ds, or zero for none.
func medianOf(ds []float64) time.Duration {
	if len(ds) == 0 {
		return 0
	}
	sorted := append([]float64(nil), ds...)
	sort.Float64s(sorted)
	return time.Duration(sorted[len(sorted)/2] * float64(time.Second))
}

// wire is the set-up as clients see it at now, with a running step marked slower when it is
// past 3 times its median (at least 2 minutes).
func (st *storedSetup) wire(now time.Time, times map[wire.MachineSetupStepID][]float64) *wire.MachineSetup {
	out := &wire.MachineSetup{StartedAt: wire.DateOf(st.Started), Steps: []wire.MachineSetupStep{}}
	if st.Ready != nil {
		out.ReadyAt = optional(wire.DateOf(*st.Ready))
	}
	for _, s := range st.Steps {
		row := wire.MachineSetupStep{ID: s.ID, State: s.State, Detail: optional(s.Detail), Error: optional(s.Error), FromPool: optional(s.FromPool)}
		if s.Started != nil {
			row.StartedAt = optional(wire.DateOf(*s.Started))
		}
		if s.Ended != nil {
			row.EndedAt = optional(wire.DateOf(*s.Ended))
		}
		if s.State == wire.MachineSetupStateRunning && s.Started != nil {
			limit := slowFactor * medianOf(times[s.ID])
			if limit < slowFloor {
				limit = slowFloor
			}
			// With no past runs there is no median to be slower than.
			if len(times[s.ID]) > 0 && now.Sub(*s.Started) > limit {
				row.Slower = optional(true)
			}
		}
		out.Steps = append(out.Steps, row)
	}
	return out
}

func (s *Service) readMachineSetup(ctx context.Context, host string) (*storedSetup, string, error) {
	object, err := s.cfg.Store.Get(ctx, machineSetupKey(host))
	if err != nil || object == nil {
		return nil, "", err
	}
	var stored storedSetup
	if json.Unmarshal(object.Data, &stored) != nil || len(stored.Steps) != len(setupSteps) {
		return nil, object.ETag, nil
	}
	return &stored, object.ETag, nil
}

func (s *Service) readSetupTimes(ctx context.Context) map[wire.MachineSetupStepID][]float64 {
	out := map[wire.MachineSetupStepID][]float64{}
	if object, err := s.cfg.Store.Get(ctx, machineSetupTimes); err == nil && object != nil {
		_ = json.Unmarshal(object.Data, &out)
	}
	return out
}

// noteSetupTime keeps how long a step took, for the median. A lost write costs one sample.
func (s *Service) noteSetupTime(ctx context.Context, id wire.MachineSetupStepID, took time.Duration) {
	for try := 0; try < 2; try++ {
		object, err := s.cfg.Store.Get(ctx, machineSetupTimes)
		if err != nil {
			return
		}
		times, when := map[wire.MachineSetupStepID][]float64{}, store.Absent
		if object != nil {
			_ = json.Unmarshal(object.Data, &times)
			when = store.Matching(object.ETag)
		}
		list := append(times[id], took.Seconds())
		if len(list) > mostSetupTimes {
			list = list[len(list)-mostSetupTimes:]
		}
		times[id] = list
		data, _ := json.Marshal(times)
		if _, err := s.cfg.Store.Put(ctx, machineSetupTimes, data, when); err == nil || !isConflict(err) {
			return
		}
	}
}

// changeMachineSetup reads a machine's set-up (made now when it has none and create is true),
// changes it and writes it, again once when another write came between.
func (s *Service) changeMachineSetup(ctx context.Context, host string, create bool, change func(*storedSetup) error) (*storedSetup, error) {
	for try := 0; ; try++ {
		stored, tag, err := s.readMachineSetup(ctx, host)
		if err != nil {
			return nil, err
		}
		when := store.Absent
		if tag != "" {
			when = store.Matching(tag)
		}
		if stored == nil {
			if !create {
				return nil, nil
			}
			stored = newStoredSetup(time.Now())
		}
		if err := change(stored); err != nil {
			return nil, err
		}
		data, err := json.Marshal(stored)
		if err != nil {
			return nil, err
		}
		if _, err = s.cfg.Store.Put(ctx, machineSetupKey(host), data, when); err == nil {
			return stored, nil
		}
		if !isConflict(err) || try > 0 {
			return nil, err
		}
	}
}

// BeginSetup starts a machine's set-up, all eight steps waiting. It does nothing when the
// machine has one already.
func (s *Service) BeginSetup(ctx context.Context, host string) error {
	_, err := s.changeMachineSetup(ctx, host, true, func(*storedSetup) error { return nil })
	if err == nil {
		s.setupChanged(ctx, host)
	}
	return err
}

// setupStep is machine/setupStep: a step the installer on the machine says. A machine whose
// person pasted the installer has no set-up yet: its first report makes one.
func (s *Service) setupStep(ctx context.Context, host string, report wire.MachineSetupReport) error {
	var took time.Duration
	var finished bool
	_, err := s.changeMachineSetup(ctx, host, true, func(st *storedSetup) (err error) {
		took, finished, err = st.report(report, time.Now())
		return err
	})
	if err != nil {
		if _, ok := err.(*RPCError); ok {
			return err
		}
		return fail(storeUnavailable, "The machine's set-up could not be kept: %v", err)
	}
	if finished {
		s.noteSetupTime(ctx, report.Step, took)
	}
	s.setupChanged(ctx, host)
	return nil
}

// retrySetup is machines/retrySetup.
func (s *Service) retrySetup(ctx context.Context, host string) error {
	retried := false
	stored, err := s.changeMachineSetup(ctx, host, false, func(st *storedSetup) error {
		retried = st.retry()
		if !retried {
			return fail(invalidParams, "No step of this machine's set-up has failed.")
		}
		return nil
	})
	if err != nil {
		if _, ok := err.(*RPCError); ok {
			return err
		}
		return fail(storeUnavailable, "The machine's set-up could not be kept: %v", err)
	}
	if stored == nil {
		return fail(noSuchHost, "This machine has no set-up.")
	}
	s.setupChanged(ctx, host)
	return nil
}

// machineSetup is a machine's set-up for its row, or none.
func (s *Service) machineSetup(ctx context.Context, host string) *wire.MachineSetup {
	stored, _, err := s.readMachineSetup(ctx, host)
	if err != nil || stored == nil {
		return nil
	}
	return stored.wire(time.Now(), s.readSetupTimes(ctx))
}

// setupChanged tells the clients, as any change of a machine is told.
func (s *Service) setupChanged(ctx context.Context, host string) {
	described := map[string]any{}
	for k, v := range s.router.Described(host) {
		described[k] = v
	}
	if setup := s.machineSetup(ctx, host); setup != nil {
		described["setup"] = setup
	}
	s.router.BroadcastHost(host, "control/hostChanged", described)
}
