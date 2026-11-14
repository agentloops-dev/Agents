#!/usr/bin/env bash
# conflictRiskAnalysis on the command line (#5473, specs/5389-conflict-risk-analysis): which
# open pull requests would this change conflict with, and go, wait for #N or stack on #N.
# Advisory, never a gate: exit 0 for any advice, non-zero only for an error.
#
# The work is Go/internal/conflictrisk, the same core as agentsd's tool `conflict_risk_analysis`
# (an agent should call the tool; this is for a person or a script). This script only reads
# the open PR list from GitHub with gh, and hands it over.
#
#   scripts/conflict-risk.sh [<branch>|--worktree] [--json] [--pr <n>] [--depends 5420,5431]
#
# With no branch, the working tree (uncommitted and new files too). Lease "build" first: it
# runs Go.
set -euo pipefail
root=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
head="" json="" self="" depends=""
while [ $# -gt 0 ]; do
	case "$1" in
	--worktree) head="" ;;
	--json) json=--json ;;
	--self | --pr) self=$2; shift ;;
	--depends | --depends-on) depends=$2; shift ;;
	-h | --help) sed -n 2,12p "$0"; exit 0 ;;
	-*) echo "conflict-risk: unknown option $1" >&2; exit 2 ;;
	*) head=$1 ;;
	esac
	shift
done
# GitHub's own list; its head is fetched as refs/pull/N/head.
prs=$(gh pr list --state open --limit 100 --json number,baseRefName,autoMergeRequest,statusCheckRollup,labels |
	python3 -c '
import json, sys
out = []
for p in json.load(sys.stdin):
    checks = p.get("statusCheckRollup") or []
    green = bool(checks) and all((c.get("conclusion") or c.get("state")) in ("SUCCESS", "NEUTRAL", "SKIPPED") for c in checks)
    approved = any(l["name"] == "approved" for l in p.get("labels", []))
    out.append({"number": p["number"], "fetchRef": "refs/pull/%d/head" % p["number"],
                "base": p["baseRefName"], "landsSoon": approved or (green and bool(p.get("autoMergeRequest")))})
json.dump(out, sys.stdout)')
cd "$root"
# A branch that is a PR's head is that PR: leave it out.
if [ -z "$self" ] && [ -n "$head" ]; then
	self=$(gh pr list --head "$head" --state open --json number --jq '.[0].number // empty' 2>/dev/null || true)
fi
exec Go/scripts/go.sh run ./cmd/conflict-risk --repo "$root" ${head:+--head "$head"} ${self:+--self "$self"} \
	${depends:+--depends-on "$depends"} $json <<<"$prs"
