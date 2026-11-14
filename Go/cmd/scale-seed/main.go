// scale-seed writes fake machines into a scratch control plane's store folder (#5282), before
// agents-control starts on it: scripts/scale-fixture.sh runs it.
//
//	scale-seed -store DIR [-n 1000]
package main

import (
	"context"
	"flag"
	"fmt"
	"os"

	"github.com/agentloops-dev/agent-loops/Go/internal/controlplane"
	"github.com/agentloops-dev/agent-loops/Go/internal/controlplane/store"
)

func main() {
	dir := flag.String("store", "", "the scratch control plane's store folder")
	n := flag.Int("n", 1000, "how many fake machines")
	flag.Parse()
	if *dir == "" {
		fmt.Fprintln(os.Stderr, "scale-seed: -store is needed")
		os.Exit(2)
	}
	if err := controlplane.SeedFakeMachines(context.Background(), store.NewFolder(*dir), *n); err != nil {
		fmt.Fprintln(os.Stderr, "scale-seed:", err)
		os.Exit(1)
	}
	fmt.Printf("scale-seed: %d fake machines in %s\n", *n, *dir)
}
