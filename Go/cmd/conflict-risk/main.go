// Command conflict-risk is conflictRiskAnalysis on the command line (#5473): the open requests
// come as JSON on stdin (a list of conflictrisk.PR), from whatever forge the project uses.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/agentloops-dev/agent-loops/Go/internal/conflictrisk"
)

func main() {
	var o conflictrisk.Options
	var depends, ignore string
	asJSON := false
	flag.StringVar(&o.Repo, "repo", ".", "a folder of the clone")
	flag.StringVar(&o.Head, "head", "", "the branch or commit to check; empty is the working tree")
	flag.StringVar(&o.Main, "main", "", "the default branch's ref")
	flag.StringVar(&o.Remote, "remote", "origin", "where the requests' heads are fetched from")
	flag.IntVar(&o.Self, "self", 0, "the lane's own request number")
	flag.StringVar(&depends, "depends-on", "", "request numbers this change builds on, comma separated")
	flag.StringVar(&ignore, "ignore", "", "more regenerating files, comma separated globs")
	flag.BoolVar(&asJSON, "json", false, "print the report as JSON")
	flag.Parse()
	for _, n := range strings.Split(depends, ",") {
		if v, err := strconv.Atoi(strings.TrimSpace(n)); err == nil {
			o.DependsOn = append(o.DependsOn, v)
		}
	}
	for _, g := range strings.Split(ignore, ",") {
		if g = strings.TrimSpace(g); g != "" {
			o.Ignore = append(o.Ignore, g)
		}
	}
	if err := json.NewDecoder(os.Stdin).Decode(&o.PRs); err != nil {
		fmt.Fprintln(os.Stderr, "conflict-risk: stdin is not a JSON list of requests:", err)
		os.Exit(2)
	}
	rep, err := conflictrisk.Analyse(context.Background(), o)
	if err != nil {
		fmt.Fprintln(os.Stderr, "conflict-risk:", err)
		os.Exit(1)
	}
	if asJSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		enc.Encode(rep)
		return
	}
	fmt.Println(rep.Words())
}
