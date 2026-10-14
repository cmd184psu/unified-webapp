#!/usr/bin/env bash
# Operator-run live smoke for agentflow's opencode adapter. NOT part of `make`
# or `go test`. Gated: without AGENTFLOW_LIVE_SMOKE=1 it prints a note and exits 0.
#
# It runs one real opencode stage through the production adapter and proves
#   AC-OC1  a >= 500 KB prompt reaches the model intact (end marker survives)
#   AC-OC2  a marker plugin in a throwaway HOME's global opencode config is not loaded
#
# Environment
#   AGENTFLOW_LIVE_SMOKE=1        required, enables the run
#   AGENTFLOW_SMOKE_HOST          backend base URL (e.g. http://gpu-host:11434). Unset: the
#                                 script starts the local stub model server (no model, no cost)
#   AGENTFLOW_SMOKE_MODEL         model id served by the backend (stub mode: stub-model)
#   AGENTFLOW_SMOKE_API_TYPE      ollama (default), openai_compatible or anthropic
#   AGENTFLOW_KEY_SMOKE           API key, if the backend needs one (forwarded to opencode)
#   AGENTFLOW_SMOKE_OPENCODE      opencode binary (default ~/.opencode/bin/opencode)
#   AGENTFLOW_SMOKE_PROMPT_KB     prompt size in KB (default 500, about 125k tokens: the
#                                 live model needs a context window that large)
#
# Stub run (proves the script itself, spends nothing):
#   AGENTFLOW_LIVE_SMOKE=1 scripts/agentflow-smoke.sh
# Live run against a backend you chose:
#   AGENTFLOW_LIVE_SMOKE=1 AGENTFLOW_SMOKE_HOST=http://host:11434 AGENTFLOW_SMOKE_MODEL=qwen3:32b \
#     scripts/agentflow-smoke.sh
set -euo pipefail

if [ "${AGENTFLOW_LIVE_SMOKE:-}" != "1" ]; then
	echo "agentflow-smoke: AGENTFLOW_LIVE_SMOKE is not 1; nothing to do (operator-run only)."
	exit 0
fi

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$root"
tmp="$(mktemp -d)"
stub_pid=""
cleanup() {
	[ -n "$stub_pid" ] && kill "$stub_pid" 2>/dev/null || true
	rm -rf "$tmp"
}
trap cleanup EXIT

if [ -z "${AGENTFLOW_SMOKE_HOST:-}" ]; then
	echo "agentflow-smoke: no AGENTFLOW_SMOKE_HOST, using the local stub model server"
	port="$(python3 -c 'import socket;s=socket.socket();s.bind(("127.0.0.1",0));print(s.getsockname()[1])')"
	go build -o "$tmp/stub" ./internal/agentflow/runner/opencode/testdata/stub
	echo '[{"kind":"text","text":"SMOKE-STUB-ANSWER"}]' >"$tmp/script.json"
	"$tmp/stub" -addr "127.0.0.1:$port" -script "$tmp/script.json" -log "$tmp/stub.log" >/dev/null &
	stub_pid=$!
	for _ in $(seq 50); do
		(exec 3<>"/dev/tcp/127.0.0.1/$port") 2>/dev/null && break
		sleep 0.1
	done
	export AGENTFLOW_SMOKE_HOST="http://127.0.0.1:$port"
	export AGENTFLOW_SMOKE_MODEL="${AGENTFLOW_SMOKE_MODEL:-stub-model}"
	export AGENTFLOW_SMOKE_STUB_LOG="$tmp/stub.log"
elif [ -z "${AGENTFLOW_SMOKE_MODEL:-}" ]; then
	echo "agentflow-smoke: AGENTFLOW_SMOKE_MODEL is required with AGENTFLOW_SMOKE_HOST" >&2
	exit 2
fi

go run ./internal/agentflow/runner/opencode/testdata/smoke
echo "agentflow-smoke: OK"
