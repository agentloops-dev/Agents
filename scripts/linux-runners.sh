#!/usr/bin/env bash
# Start, stop or list the self-hosted Linux runners for CI (#2144), in Docker on this Mac
# (Colima), next to the xcode-27 runner. They carry the label `linux-self`.
#
#   scripts/linux-runners.sh up [N]     start N runners (default 3)
#   scripts/linux-runners.sh down       stop and remove them
#   scripts/linux-runners.sh status     list them
#
# Needs a runner registration token: RUNNER_TOKEN in the environment, or
# `agents-secret GITHUB_RUNNER_TOKEN`. Never print it. It does not switch any workflow:
# while the repo is public, a self-hosted runner would run code from fork pull
# requests, so the workflows stay on ubuntu-latest until #1064 makes it private.
# Image: myoung34/github-runner (MIT).
set -euo pipefail

repo="${RUNNER_REPO:-agentloops-dev/Agents}"
image="${RUNNER_IMAGE:-myoung34/github-runner:2.321.0}"
label="${RUNNER_LABEL:-linux-self}"
prefix="agents-linux-runner"

cmd="${1:-status}"
case "$cmd" in
  up)
    n="${2:-3}"
    token="${RUNNER_TOKEN:-$(agents-secret GITHUB_RUNNER_TOKEN)}"
    for i in $(seq 1 "$n"); do
      docker rm -f "$prefix-$i" >/dev/null 2>&1 || true
      docker run -d --restart unless-stopped --name "$prefix-$i" \
        -e REPO_URL="https://github.com/$repo" \
        -e RUNNER_NAME="$prefix-$i" \
        -e ACCESS_TOKEN="$token" \
        -e RUNNER_SCOPE=repo -e LABELS="$label" -e EPHEMERAL=true \
        "$image" >/dev/null
      echo "started $prefix-$i"
    done
    ;;
  down)
    docker ps -aq --filter "name=$prefix-" | xargs -r docker rm -f >/dev/null
    echo "removed"
    ;;
  status)
    docker ps -a --filter "name=$prefix-" --format '{{.Names}}\t{{.Status}}'
    ;;
  *)
    echo "usage: $0 up [N] | down | status" >&2
    exit 2
    ;;
esac
