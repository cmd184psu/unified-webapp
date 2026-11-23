#!/usr/bin/env bash
# Dev-only: pair two local agentflow dev instances (8788 and 8799) and, when
# PROJECT and EFFORT are given, delegate that approved effort and collect the
# result. NOT production, NOT builder.
set -euo pipefail
A=${A:-http://127.0.0.1:8788}
B=${B:-http://127.0.0.1:8799}
echo "1) invite on A"; CODE=$(curl -s -X POST $A/api/peers/invite -d '{"name":"dev-b"}' | python3 -c 'import sys,json;print(json.load(sys.stdin)["code"])')
echo "code=$CODE"
echo "2) accept on B"; curl -s -X POST $B/api/peers/accept -d "{\"a_url\":\"$A\",\"code\":\"$CODE\",\"name\":\"dev-a\",\"b_url\":\"$B\"}"
echo
echo "3) status A->B"; curl -s $A/api/peers/dev-b/status; echo
echo "4) revoke on A"; curl -s -X DELETE $A/api/peers/dev-b; echo

if [[ -z "${PROJECT:-}" || -z "${EFFORT:-}" ]]; then
  echo "PROJECT and EFFORT not set: pairing only."
  echo "For delegation: PROJECT=<slug> EFFORT=<id> TASKS='[\"T001\"]' $0"
  exit 0
fi
TASKS=${TASKS:-[]}
echo "5) invite/accept again (step 4 revoked the pair)"
CODE=$(curl -s -X POST $A/api/peers/invite -d '{"name":"dev-b"}' | python3 -c 'import sys,json;print(json.load(sys.stdin)["code"])')
curl -s -X POST $B/api/peers/accept -d "{\"a_url\":\"$A\",\"code\":\"$CODE\",\"name\":\"dev-a\",\"b_url\":\"$B\"}" >/dev/null
echo "6) delegate $EFFORT to dev-b"
DEL=$(curl -s -X POST $A/api/peers/dev-b/delegate -d "{\"project\":\"$PROJECT\",\"effort\":$EFFORT,\"tasks\":$TASKS,\"reason\":\"dev demo\"}")
echo "$DEL"
ID=$(printf '%s' "$DEL" | python3 -c 'import sys,json;print(json.load(sys.stdin).get("id",""))')
[[ -n "$ID" ]] || { echo "delegation failed"; exit 1; }
echo "7) status (owner polls the peer)"; curl -s $A/api/delegations/$ID; echo
echo "8) peer status"; curl -s $B/api/peers/dev-a/status; echo
echo "9) cancel on the owner"; curl -s -X POST $A/api/peers/dev-b/delegations/$ID/cancel; echo
echo "10) collect (fails until the peer has task branches, which is expected here)"; curl -s -X POST $A/api/delegations/$ID/collect; echo
