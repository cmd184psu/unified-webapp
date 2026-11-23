#!/usr/bin/env bash
# Dev-only: pair two local agentflow dev instances (8788 and 8799). NOT production, NOT builder.
set -euo pipefail
A=http://127.0.0.1:8788
B=http://127.0.0.1:8799
echo "1) invite on A"; CODE=$(curl -s -X POST $A/api/peers/invite -d '{"name":"dev-b"}' | python3 -c 'import sys,json;print(json.load(sys.stdin)["code"])')
echo "code=$CODE"
echo "2) accept on B"; curl -s -X POST $B/api/peers/accept -d "{\"a_url\":\"$A\",\"code\":\"$CODE\",\"name\":\"dev-a\",\"b_url\":\"$B\"}"
echo
echo "3) status A->B"; curl -s $A/api/peers/dev-b/status; echo
echo "4) revoke on A"; curl -s -X DELETE $A/api/peers/dev-b; echo