#!/bin/sh
# A scratch control plane's store with fake machines (#5282, epic #5281), for run-app and
# test-servers: writes N fake machines (default 1,000) into a store folder, before the
# scratch agents-control starts on it. They list as offline (no host is behind them), with
# mixed platforms, versions and implementations.
#
#   scripts/scale-fixture.sh STORE_DIR [N]
#
# The budgets it proves are in scripts/perf-budgets.json ("scale") and
# docs/how-to/keep-the-performance-budgets.md; Go/internal/controlplane/scalefixture_test.go
# holds the list's.
set -eu
[ $# -ge 1 ] || { echo "usage: scale-fixture.sh STORE_DIR [N]" >&2; exit 2; }
root=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
cd "$root/Go"
exec go run ./cmd/scale-seed -store "$1" -n "${2:-1000}"
