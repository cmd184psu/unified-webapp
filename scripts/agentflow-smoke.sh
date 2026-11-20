#!/usr/bin/env bash
# One-command smoke for a running agentflow instance: stand-ins only, no models, no tokens,
# under 10 minutes. Prints PASS/FAIL per scenario; exits 1 when any failed.
#
#   scripts/agentflow-smoke.sh                         # dev, http://127.0.0.1:8788
#   scripts/agentflow-smoke.sh -url http://127.0.0.1:8787 -db <data dir>/agentflow.db
#
# Scenarios: 1 worker/verify/review/merge, 2 question and answer, 3 drain restart and seat
# adoption (dev only unless -restart yes: it pauses every running project until the
# restart), 4 cancel, 5 silence, 6 notify text and debounce, 7 halt text, 8 queue seat
# nudge, 9 classic opencode worker on a local stub model.
#
# It uses only its own smoke-<ts>-* projects, backends and seat profiles and af-smoke-<ts>-*
# tmux sessions, wraps notify_command for the run (the previous command still runs) and
# restores it, and moves its temp files into a fresh temp dir at the end. -keep leaves
# everything for inspection. The opencode adapter's own live check is
# scripts/agentflow-opencode-smoke.sh.
set -euo pipefail
root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$root"
bin="$(mktemp -d /tmp/af-smoke-bin.XXXXXX)"
go build -o "$bin/stub" ./internal/agentflow/runner/opencode/testdata/stub
go build -o "$bin/agentflow-smoke" ./cmd/agentflow-smoke
status=0
"$bin/agentflow-smoke" -stub "$bin/stub" "$@" || status=$?
trash="$(mktemp -d /tmp/af-smoke-bin-done.XXXXXX)"
mv "$bin" "$trash/"
exit "$status"
