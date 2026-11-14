package controlplane

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/agentloops-dev/agent-loops/Go/internal/controlplane/records"
	"github.com/agentloops-dev/agent-loops/Go/internal/controlplane/store"
)

// SeedFakeMachines writes n fake machines into a control plane's store (#5282): the scale
// fixture for the epic on many machines per user (#5281). They have mixed platforms,
// versions and implementations, and the capabilities of a host of this build, so
// machines/list and machine/hello answer for them as for real ones. None has a link, so
// each one lists as offline: a scratch control plane with no host behind a machine can
// say no other state. Calling it again writes the same machines (ids fake-0001, …).
func SeedFakeMachines(ctx context.Context, st store.Store, n int) error {
	r := records.New(st)
	if err := r.Load(ctx); err != nil {
		return err
	}
	platforms := []string{"darwin arm64", "linux aarch64", "linux x86_64", "windows x86_64"}
	versions := []string{"0.9.0", "0.9.1", "0.10.0"}
	impls := []string{"go", "go", "swift"}
	for i := 1; i <= n; i++ {
		impl, _ := json.Marshal(impls[i%len(impls)])
		h := records.MachineRecord{
			ID:       fmt.Sprintf("fake-%04d", i),
			Name:     fmt.Sprintf("Fake machine %04d", i),
			Platform: platforms[i%len(platforms)],
			Version:  versions[i%len(versions)],
			Extra:    map[string]json.RawMessage{"implementation": impl},
		}
		if err := r.SaveHost(ctx, h); err != nil {
			return err
		}
	}
	return nil
}
