#!/bin/bash
# Tests scripts/conflict-risk.sh (#5473) on a scratch repo: a clean PR, a conflicting PR that
# lands soon (wait), a conflicting PR the change depends on (stack), a stale conflicting PR
# (go), a regenerating file (ignored), and the working tree.
#
#   scripts/conflict-risk-test.sh
set -euo pipefail

here=$(cd "$(dirname "$0")" && pwd)
tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT
failed=0
export GIT_AUTHOR_NAME=t GIT_AUTHOR_EMAIL=t@t GIT_COMMITTER_NAME=t GIT_COMMITTER_EMAIL=t@t
export CONFLICT_RISK_NO_FETCH=1 CONFLICT_RISK_NOW=1800000000

git init -q --bare "$tmp/origin.git"
git init -q -b main "$tmp/repo"
cd "$tmp/repo"
git remote add origin "$tmp/origin.git"
seq 1 20 >a.txt; seq 1 20 >b.txt; seq 1 20 >c.txt; seq 1 20 >generated.ts
git add -A; git commit -qm base; git push -q origin main; git fetch -q origin

edit() { # branch file line text...
	git checkout -q -b "$1" origin/main
	sed -i "$3s/.*/$4/" "$2"
	git commit -qam "$1"
	git rev-parse HEAD
	git checkout -q main
}
lane=$(edit lane a.txt 5 lane)
sed -i '10s/.*/lane/' b.txt; git checkout -q lane; sed -i '10s/.*/lane/' b.txt; git commit -qam lane2; git checkout -q main
lane=$(git rev-parse lane)
git checkout -q lane; sed -i '3s/.*/lane/' generated.ts; sed -i '15s/.*/lane/' c.txt; git commit -qam lane3; git checkout -q main
git branch -f lane lane

soon=$(edit soon a.txt 5 other)        # conflicts in a.txt, lands soon
dep=$(edit dep b.txt 10 other)         # conflicts in b.txt, lane builds on it
git checkout -q -b gen origin/main; sed -i '3s/.*/other/' generated.ts; git commit -qam gen; gen=$(git rev-parse HEAD); git checkout -q main
git checkout -q -b clean origin/main; echo x >new.txt; git add new.txt; git commit -qm clean; clean=$(git rev-parse HEAD); git checkout -q main

cat >"$tmp/prs.json" <<JSON
[
 {"number":1,"headRefOid":"$soon","headRefName":"soon","baseRefName":"main","additions":300,"deletions":10,"updatedAt":"2027-01-15T07:00:00Z","labels":[],"autoMergeRequest":{"x":1},"statusCheckRollup":[{"conclusion":"SUCCESS"}]},
 {"number":2,"headRefOid":"$dep","headRefName":"dep","baseRefName":"main","additions":300,"deletions":10,"updatedAt":"2027-01-15T07:00:00Z","labels":[],"autoMergeRequest":null,"statusCheckRollup":[{"conclusion":"FAILURE"}]},
 {"number":3,"headRefOid":"$gen","headRefName":"gen","baseRefName":"main","additions":1,"deletions":1,"updatedAt":"2027-01-15T07:00:00Z","labels":[],"autoMergeRequest":null,"statusCheckRollup":[]},
 {"number":4,"headRefOid":"$clean","headRefName":"clean","baseRefName":"main","additions":1,"deletions":0,"updatedAt":"2027-01-15T07:00:00Z","labels":[],"autoMergeRequest":null,"statusCheckRollup":[]}
]
JSON
export CONFLICT_RISK_PRS_JSON=$tmp/prs.json

check() { # name expected actual
	if [ "$2" != "$3" ]; then echo "FAIL $1: want '$2' got '$3'" >&2; failed=1; else echo "ok $1"; fi
}
j() { bash "$here/conflict-risk.sh" --json "$@" | python3 -c "import json,sys; r=json.load(sys.stdin); $PYX"; }

PYX='print(r["advice"], r["advice_for"], [(p["number"],p["advice"],p["state"]) for p in r["prs"]])'
check "wait on the PR that lands soon" "wait 1 [(1, 'wait', 'conflict'), (2, 'go', 'conflict'), (3, 'go', 'clean'), (4, 'go', 'clean')]" "$(j lane)"
check "stack on a PR it depends on" "stack 2 [(1, 'wait', 'conflict'), (2, 'stack', 'conflict'), (3, 'go', 'clean'), (4, 'go', 'clean')]" "$(j --depends 2 lane)"
PYX='print([p["ignored"] for p in r["prs"] if p["number"]==3])'
check "regenerating file ignored" "[['generated.ts']]" "$(j lane)"

# A stale PR with a conflict: go, with a note.
sed -i 's/"number":1,\(.*\)"autoMergeRequest":{"x":1}/"number":1,\1"autoMergeRequest":null/' "$tmp/prs.json"
PYX='print(r["advice"], [p["advice"] for p in r["prs"]][:2])'
check "no wait when it does not land soon" "go ['go', 'go']" "$(j lane)"

# The lane's own PR is left out.
PYX='print([p["number"] for p in r["prs"]])'
check "own PR left out" "[2, 3, 4]" "$(j --pr 1 lane)"

# The working tree: uncommitted and untracked changes count.
git checkout -q -b wt origin/main
sed -i '5s/.*/wt/' a.txt
sed -i 's/"autoMergeRequest":null,"statusCheckRollup":\[{"conclusion":"SUCCESS"/"autoMergeRequest":{"x":1},"statusCheckRollup":[{"conclusion":"SUCCESS"/' "$tmp/prs.json"
PYX='print(r["advice"], r["advice_for"])'
check "worktree counts" "wait 1" "$(j --worktree)"
check "worktree left unchanged" " M a.txt" "$(git status --porcelain | head -1 | sed 's/^ */ /')"

# The default output ends with the advice.
check "plain output" "advice: wait #1" "$(bash "$here/conflict-risk.sh" --worktree | tail -1)"

exit "$failed"
