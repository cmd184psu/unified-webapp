#!/usr/bin/env bash
# Smoke-test the smbedit module's HTTP API against a running unified-webapp
# server. Exercises every read endpoint plus a non-destructive round trip on
# /api/globals and /api/shares (PUT back what GET returned). Never touches
# /api/save-and-restart -- that writes /etc/samba/smb.conf and restarts smbd.
#
# Usage:
#   bash local-test/setup.sh                              # once, seeds data dirs
#   go run ./cmd/server -config local-test/config.json &   # in another shell
#   bash local-test/smbedit-verify.sh                      # defaults to smbedit.test:8080
#   bash local-test/smbedit-verify.sh http://localhost:9000
#
# If smbedit.test isn't in /etc/hosts yet, skip editing it and pass:
#   bash local-test/smbedit-verify.sh http://smbedit.test:8091 127.0.0.1
# The second argument, if given, is used as a curl --resolve target so the
# hostname resolves to it without touching /etc/hosts.
set -euo pipefail

BASE="${1:-http://smbedit.test:8080}"
RESOLVE_IP="${2:-}"
TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT

RESOLVE_ARGS=()
if [ -n "$RESOLVE_IP" ]; then
  host_port="$(echo "$BASE" | sed -E 's#^https?://##')"
  RESOLVE_ARGS=(--resolve "${host_port}:${RESOLVE_IP}")
fi

PASS=0
FAIL=0

check() {
  local desc="$1" method="$2" path="$3" data="${4:-}" want_status="${5:-200}"
  local args=("${RESOLVE_ARGS[@]}" -s -o "$TMP/body" -w '%{http_code}' -X "$method" "$BASE$path")
  if [ -n "$data" ]; then
    args+=(-H 'Content-Type: application/json' --data "$data")
  fi
  local status
  status="$(curl "${args[@]}")" || status="000"
  if [ "$status" = "$want_status" ]; then
    echo "  ok    $desc ($method $path -> $status)"
    PASS=$((PASS + 1))
  else
    echo "  FAIL  $desc ($method $path -> $status, want $want_status)"
    echo "        body: $(head -c 300 "$TMP/body")"
    FAIL=$((FAIL + 1))
  fi
}

echo "== smbedit API smoke test against $BASE =="

check "version"           GET  /api/version
check "full config"       GET  /api/config
check "shares list"       GET  /api/shares
check "globals list"      GET  /api/globals
check "folder picker"     GET  /api/folders
check "smb.conf preview"  GET  /api/preview

echo "== non-destructive round trips =="

globals="$(curl "${RESOLVE_ARGS[@]}" -s "$BASE/api/globals")"
check "globals round trip" PUT /api/globals "$globals"

shares="$(curl "${RESOLVE_ARGS[@]}" -s "$BASE/api/shares")"
check "shares round trip"  PUT /api/shares "$shares"

echo "== staged import (does not persist) =="
cat > "$TMP/sample-smb.conf" <<'EOF'
[global]
workgroup = TESTGROUP

[smoke]
path = /tmp
writable = yes
guest ok = no
EOF
import_payload="$(python3 -c 'import json,sys; print(json.dumps({"path": sys.argv[1]}))' "$TMP/sample-smb.conf")"
check "import (staged)" POST /api/import "$import_payload"

echo
echo "== $PASS passed, $FAIL failed =="
[ "$FAIL" -eq 0 ]
