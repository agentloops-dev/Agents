#!/usr/bin/env bash
# Advise a lane or a lead before work collides with open PRs (#5473, epic #5388).
#
#   scripts/conflict-risk.sh [--json] [--depends N,N] [--pr N] <branch|--worktree>
#
# Runs `git merge-tree --write-tree --name-only` of the change (a branch, or the working tree
# of this checkout, committed to a temporary object) against origin/main and every open PR head,
# then gives one advice per PR and one overall: go, wait for #N, or stack on #N. The rules and
# the --json report are in specs/5389-conflict-risk-analysis/spec.md. The advice is advisory:
# the exit code is 0 for any advice and non-zero only for an error.
#
#   --depends N,N   the change builds on these PRs (a lead knows; a lane may say so)
#   --pr N          the change's own PR: left out of the list
#   --json          print the report instead of one line per PR
#
# Test hooks: CONFLICT_RISK_PRS_JSON=<file> is read instead of `gh pr list`;
# CONFLICT_RISK_NO_FETCH=1 skips every fetch; CONFLICT_RISK_NOW=<epoch> sets the clock.
set -euo pipefail
exec python3 -I - "$@" <<'PY'
import json, os, subprocess, sys, tempfile, time, fnmatch

REGEN = ["*_gen.go", "*/generated.ts", "generated.ts", "*tokens.css", "*Package.resolved", "go.sum", "*/go.sum"]
SMALL_FILES, SMALL_LINES, STALE_SECONDS = 3, 40, 24 * 3600


def die(msg):
    print("conflict-risk.sh: " + msg, file=sys.stderr)
    sys.exit(1)


def git(*args, env=None, ok=(0,)):
    p = subprocess.run(["git", *args], capture_output=True, text=True, env=env)
    if p.returncode not in ok:
        die("git %s: %s" % (" ".join(args), p.stderr.strip()))
    return p


def merge_tree(a, b):
    """Return (state, files) of merging a with b."""
    p = git("merge-tree", "--write-tree", "--name-only", "--no-messages", a, b, ok=(0, 1))
    if p.returncode == 0:
        return "clean", []
    lines = p.stdout.split("\n")[1:]
    return "conflict", [l for l in lines if l.strip()]


def regenerates(f):
    return any(fnmatch.fnmatch(f, g) for g in REGEN)


def main():
    args = sys.argv[1:]
    as_json, target, depends, own = False, None, set(), None
    i = 0
    while i < len(args):
        a = args[i]
        if a == "--json":
            as_json = True
        elif a == "--depends":
            i += 1
            depends = {int(x) for x in args[i].replace("#", "").split(",") if x}
        elif a == "--pr":
            i += 1
            own = int(args[i].lstrip("#"))
        elif a in ("-h", "--help"):
            print("usage: conflict-risk.sh [--json] [--depends N,N] [--pr N] <branch|--worktree>")
            return
        elif target is None:
            target = a
        else:
            die("unexpected argument " + a)
        i += 1
    if not target:
        die("usage: conflict-risk.sh [--json] [--depends N,N] [--pr N] <branch|--worktree>")
    no_fetch = bool(os.environ.get("CONFLICT_RISK_NO_FETCH"))
    now = int(os.environ.get("CONFLICT_RISK_NOW", time.time()))

    if not no_fetch:
        git("fetch", "-q", "origin", "main")
    main_sha = git("rev-parse", "origin/main").stdout.strip()

    branch_name = None
    if target == "--worktree":
        top = git("rev-parse", "--show-toplevel").stdout.strip()
        idx = git("rev-parse", "--git-path", "index").stdout.strip()
        with tempfile.TemporaryDirectory() as tmp:
            env = dict(os.environ, GIT_INDEX_FILE=os.path.join(tmp, "index"))
            if os.path.exists(os.path.join(top, idx) if not os.path.isabs(idx) else idx):
                src = idx if os.path.isabs(idx) else os.path.join(top, idx)
                open(env["GIT_INDEX_FILE"], "wb").write(open(src, "rb").read())
            subprocess.run(["git", "-C", top, "add", "-A"], env=env, check=True, capture_output=True)
            tree = subprocess.run(["git", "-C", top, "write-tree"], env=env, check=True, capture_output=True, text=True).stdout.strip()
        head0 = git("rev-parse", "HEAD").stdout.strip()
        head = git("-c", "user.name=x", "-c", "user.email=x@x", "commit-tree", tree, "-p", head0, "-m", "conflict-risk worktree").stdout.strip()
    else:
        branch_name = target
        head = git("rev-parse", "--verify", target + "^{commit}").stdout.strip()

    if os.environ.get("CONFLICT_RISK_PRS_JSON"):
        prs = json.load(open(os.environ["CONFLICT_RISK_PRS_JSON"]))
    else:
        p = subprocess.run(["gh", "pr", "list", "--state", "open", "--limit", "100", "--json",
                            "number,headRefOid,headRefName,baseRefName,additions,deletions,statusCheckRollup,autoMergeRequest,labels,updatedAt"],
                           capture_output=True, text=True)
        if p.returncode:
            die("gh pr list: " + p.stderr.strip())
        prs = json.loads(p.stdout)

    prs = [p for p in prs if p["number"] != own and p["headRefOid"] != head and p.get("headRefName") != branch_name]
    if not no_fetch:
        missing = [p for p in prs if subprocess.run(["git", "cat-file", "-e", p["headRefOid"] + "^{commit}"], capture_output=True).returncode]
        if missing:
            subprocess.run(["git", "fetch", "-q", "origin"] + ["+refs/pull/%d/head:refs/conflict-risk/%d" % (p["number"], p["number"]) for p in missing], capture_output=True)

    _, lane_main = merge_tree(head, main_sha)
    lane_main_set = set(lane_main)
    rows = []
    for p in prs:
        n, ph = p["number"], p["headRefOid"]
        if subprocess.run(["git", "cat-file", "-e", ph + "^{commit}"], capture_output=True).returncode:
            rows.append(dict(number=n, head=ph, base=p.get("baseRefName", "main"), state="error", files=[], ignored=[], lines=0, lands_soon=False, advice="go"))
            continue
        _, pm = merge_tree(ph, main_sha)
        state, conflict = merge_tree(head, ph)
        pair = [f for f in conflict if f not in lane_main_set and f not in set(pm)]
        ignored = [f for f in pair if regenerates(f)]
        files = [f for f in pair if not regenerates(f)]
        state = "conflict" if files else "clean"
        lines = p.get("additions", 0) + p.get("deletions", 0)
        labels = {l["name"] for l in p.get("labels", [])}
        checks = p.get("statusCheckRollup") or []
        green = bool(checks) and all((c.get("conclusion") or c.get("state")) in ("SUCCESS", "NEUTRAL", "SKIPPED") for c in checks)
        lands_soon = (bool(p.get("autoMergeRequest")) and green) or "approved" in labels
        stale = False
        if p.get("updatedAt"):
            try:
                stale = now - time.mktime(time.strptime(p["updatedAt"], "%Y-%m-%dT%H:%M:%SZ")) + time.timezone > STALE_SECONDS
            except ValueError:
                pass
        near = lands_soon
        depends_on = n in depends
        if not files:
            advice = "go"
        elif depends_on and not near:
            advice = "stack"
        elif lands_soon and not (len(files) <= SMALL_FILES and lines <= SMALL_LINES):
            advice = "wait"
        else:
            advice = "go"
        rows.append(dict(number=n, head=ph, base=p.get("baseRefName", "main"), state=state, files=files,
                         ignored=ignored, lines=lines, lands_soon=lands_soon, advice=advice,
                         **({"note": "order unknown, rebase after the other lands"} if advice == "go" and files else {})))

    overall, advice_for = "go", None
    stacks = [r for r in rows if r["advice"] == "stack"]
    waits = [r for r in rows if r["advice"] == "wait"]
    if stacks:
        overall, advice_for = "stack", stacks[0]["number"]
    elif waits:
        overall, advice_for = "wait", max(waits, key=lambda r: r["lines"])["number"]
    report = dict(version=1, head=head, main=main_sha, main_conflicts=lane_main, advice=overall,
                  advice_for=advice_for, prs=rows)
    if as_json:
        print(json.dumps(report, indent=2))
        return
    if lane_main:
        print("rebase now: conflicts with origin/main in " + ", ".join(lane_main))
    for r in rows:
        if r["state"] == "conflict":
            print("#%d %s: conflict in %s" % (r["number"], r["advice"], ", ".join(r["files"])))
        elif r["state"] == "error":
            print("#%d error: head not fetched" % r["number"])
    print("advice: " + overall + (" #%d" % advice_for if advice_for else ""))


main()
PY
