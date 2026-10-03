#!/usr/bin/env bash
# A-OG: owner curl acceptance of the CertMachine API extension
# (docs/frd/FRD-haproxy-editor.md section 13.5; docs/guides/certmachine.md).
#
# Just run it from the repo root and paste the API key when asked:
#
#     bash docs/plans/haproxy-editor/a-og-curl-checks.sh
#
# Everything is figured out for you:
#   * the CertMachine address comes from host_routing in ./unified-webapp.json
#   * the key is asked for without echoing it (never put on a command line)
#   * a cert to test on is picked from CertMachine's active certs
#   * the re-issue check creates its OWN throwaway cert, edits it, and deletes
#     it again (it never touches your real certs)
#
# Optional environment overrides (rarely needed):
#   BASE=https://host     CertMachine address (default: from CONFIG, else certmachine.cmdhome.net)
#   CONFIG=path           config to read host_routing from (default ./unified-webapp.json)
#   KEY=...               the API key (prompted for if unset); or KEYFILE=path holding it
#   FQDN=name             test against this cert instead of the first active one
#   REISSUE=0             skip the re-issue check (it is on by default)
#   CACERT=path           trust this CA file;  INSECURE=1  curl -k (quick local try only)
#
# Prints PASS/FAIL per check and exits non-zero if any check failed.

CONFIG="${CONFIG:-./unified-webapp.json}"
if [ -z "${BASE:-}" ]; then
  BASE=$(python3 - "$CONFIG" <<'PY' 2>/dev/null
import json, sys
try:
    hr = json.load(open(sys.argv[1])).get("host_routing", {})
    hosts = [h for h, m in hr.items() if m == "certmachine"]
    print("https://" + hosts[0] if hosts else "")
except Exception:
    print("")
PY
)
  BASE="${BASE:-https://certmachine.cmdhome.net}"
fi
BASE="${BASE%/}"

if [ -z "${KEY:-}" ] && [ -n "${KEYFILE:-}" ]; then KEY=$(tr -d '\r\n' < "$KEYFILE"); fi
if [ -z "${KEY:-}" ]; then
  printf 'CertMachine API key for %s (input hidden): ' "$BASE" >&2
  IFS= read -rs KEY; echo >&2
fi
[ -n "$KEY" ] || { echo "no API key given"; exit 2; }

TMP="$(mktemp -d)"; chmod 700 "$TMP"
# The key goes in a private header file, so it never appears in a process list.
printf 'Authorization: Bearer %s\n' "$KEY" > "$TMP/auth"; chmod 600 "$TMP/auth"; unset KEY
CURL=(curl -sS --max-time 30 -H "@$TMP/auth")
[ -n "${CACERT:-}" ] && CURL+=(--cacert "$CACERT")
[ "${INSECURE:-}" = "1" ] && CURL+=(-k)

THROWAWAY_IDS=""; THROWAWAY_FQDN=""
cleanup() {
  # delete the throwaway cert rows the re-issue check created (best effort, always runs)
  if [ -n "$THROWAWAY_FQDN" ]; then
    for id in $THROWAWAY_IDS; do
      "${CURL[@]}" -o /dev/null -X DELETE "$BASE/api/certs/$id?confirm=$THROWAWAY_FQDN" 2>/dev/null
    done
    "${CURL[@]}" "$BASE/api/certs?fqdn=$THROWAWAY_FQDN" 2>/dev/null \
      | python3 -c "import sys,json; [print(c['id']) for c in json.load(sys.stdin)['certs']]" 2>/dev/null \
      | while read -r id; do "${CURL[@]}" -o /dev/null -X DELETE "$BASE/api/certs/$id?confirm=$THROWAWAY_FQDN" 2>/dev/null; done
  fi
  rm -rf "$TMP"
}
trap cleanup EXIT

fails=0
pass() { echo "PASS  $*"; }
fail() { echo "FAIL  $*"; fails=$((fails + 1)); }
json() { python3 -c "import sys,json; d=json.load(sys.stdin); $1"; }
hdr()  { awk -v k="$(echo "$1" | tr 'A-Z' 'a-z')" 'BEGIN{FS=": "} tolower($1)==k {gsub(/\r/,"",$2); print $2; exit}' "$2"; }

echo "== CertMachine A-OG checks against $BASE"

# 0. reachable and authorised
code=$("${CURL[@]}" -o "$TMP/all.json" -w '%{http_code}' "$BASE/api/certs"); rc=$?
if [ "$rc" = 60 ] || [ "$rc" = 35 ]; then
  echo "TLS error talking to $BASE. Add CACERT=<CertMachine root CA file> (download it from $BASE/api/ca/root.crt) or INSECURE=1 for a quick try."; exit 1; fi
if [ "$code" = 200 ]; then pass "authorised list works (GET /api/certs = 200)"; else
  fail "GET /api/certs = $code (401 means the API key is wrong or the service was not restarted after adding it)"; exit 1; fi
total=$(json 'print(len(d["certs"]))' < "$TMP/all.json")

# pick the cert to test on: FQDN if given, else the first ACTIVE cert that is not a wildcard
if [ -z "${FQDN:-}" ]; then
  FQDN=$(json '
c=[x["fqdn"] for x in d["certs"] if x["status"]=="active" and not x["fqdn"].startswith("*")]
print(c[0] if c else "")' < "$TMP/all.json")
  [ -n "$FQDN" ] || { echo "CertMachine has no active cert to test on; create one or set FQDN="; exit 1; }
  echo "      testing on the first active cert: $FQDN (set FQDN=... to choose another)"
fi

# 1. FR-C1 filters
"${CURL[@]}" -o "$TMP/active.json" "$BASE/api/certs?fqdn=$FQDN&status=active"
n=$(json 'print(len(d["certs"]))' < "$TMP/active.json")
if [ "$n" = 1 ]; then pass "fqdn+status=active returns exactly one cert"; else fail "fqdn+status=active returned $n certs (want 1)"; fi
ID=$(json 'print(d["certs"][0]["id"])' < "$TMP/active.json" 2>/dev/null)
FP=$(json 'print(d["certs"][0]["fingerprint"])' < "$TMP/active.json" 2>/dev/null)
[ -n "$ID" ] || { echo "cannot continue without an active cert id"; exit 1; }
echo "      using cert id $ID, fingerprint $FP"

upper=$(echo "$FQDN" | tr 'a-z' 'A-Z')
m=$("${CURL[@]}" "$BASE/api/certs?fqdn=$upper&status=active" | json 'print(len(d["certs"]))')
[ "$m" = 1 ] && pass "fqdn filter is case-insensitive" || fail "fqdn filter case-insensitive: got $m"
code=$("${CURL[@]}" -o /dev/null -w '%{http_code}' "$BASE/api/certs?status=bogus")
[ "$code" = 400 ] && pass "status=bogus is 400" || fail "status=bogus = $code (want 400)"
e=$("${CURL[@]}" "$BASE/api/certs?fqdn=no-such-name.invalid" | tr -d ' \n')
echo "$e" | grep -q '"certs":\[\]' && pass "no match returns an empty array" || fail "no match body: $e"
echo "      unfiltered list has $total certs"

# 2. FR-C2 / FR-C3 headers on the three file downloads, ETag == sha256 of the body
for f in haproxy.pem cert.pem key.pem; do
  "${CURL[@]}" -D "$TMP/h.$f" -o "$TMP/b.$f" "$BASE/api/certs/$ID/files/$f"
  etag=$(hdr etag "$TMP/h.$f" | tr -d '"')
  want="sha256-$(sha256sum "$TMP/b.$f" | cut -d' ' -f1)"
  [ "$etag" = "$want" ] && pass "$f: ETag equals sha256 of the body" || fail "$f: ETag '$etag' != '$want'"
  [ "$(hdr x-cert-id "$TMP/h.$f")" = "$ID" ] && pass "$f: X-Cert-Id = $ID" || fail "$f: X-Cert-Id = '$(hdr x-cert-id "$TMP/h.$f")'"
  [ "$(hdr x-cert-fingerprint "$TMP/h.$f")" = "$FP" ] && pass "$f: X-Cert-Fingerprint matches the list" || fail "$f: X-Cert-Fingerprint mismatch"
done

# 3. FR-C4 conditional requests and HEAD
ETAG=$(hdr etag "$TMP/h.haproxy.pem")           # includes the quotes
for variant in "exact:$ETAG" "weak:W/$ETAG" "star:*"; do
  name=${variant%%:*}; val=${variant#*:}
  : > "$TMP/inm"   # curl creates no output file for a bodiless 304, so start with an empty one
  code=$("${CURL[@]}" -o "$TMP/inm" -w '%{http_code}' -H "If-None-Match: $val" "$BASE/api/certs/$ID/files/haproxy.pem")
  size=$(wc -c < "$TMP/inm" | tr -d ' ')
  [ "$code" = 304 ] && [ "$size" = 0 ] && pass "If-None-Match ($name) -> 304 with no body" || fail "If-None-Match ($name) -> $code, $size body bytes"
done
code=$("${CURL[@]}" -o /dev/null -w '%{http_code}' -H 'If-None-Match: "sha256-0000"' "$BASE/api/certs/$ID/files/haproxy.pem")
[ "$code" = 200 ] && pass "non-matching If-None-Match -> 200" || fail "non-matching If-None-Match -> $code"
"${CURL[@]}" -I -D "$TMP/h.head" -o /dev/null "$BASE/api/certs/$ID/files/haproxy.pem"
[ "$(hdr etag "$TMP/h.head")" = "$ETAG" ] && [ -n "$(hdr x-cert-fingerprint "$TMP/h.head")" ] && pass "HEAD returns the same ETag and identity headers (no body)" || fail "HEAD headers missing or different"

# 4. unchanged on purpose: no X-Cert-*/ETag on the CA root or the bundle; 404 for an unknown id
for p in "api/ca/root.crt" "api/certs/$ID/bundle"; do
  "${CURL[@]}" -D "$TMP/h.o" -o /dev/null "$BASE/$p"
  [ -z "$(hdr x-cert-id "$TMP/h.o")" ] && [ -z "$(hdr etag "$TMP/h.o")" ] && pass "$p carries no X-Cert-Id / ETag (unchanged)" || fail "$p unexpectedly carries X-Cert-Id or ETag"
done
code=$("${CURL[@]}" -D "$TMP/h.404" -o /dev/null -w '%{http_code}' "$BASE/api/certs/999999999/files/haproxy.pem")
[ "$code" = 404 ] && [ -z "$(hdr etag "$TMP/h.404")" ] && pass "unknown id -> 404 with no ETag" || fail "unknown id -> $code"

# 5. an Edit that re-issues (new SANs) makes the filtered list return the NEW id.
#    Uses a throwaway cert this script creates and deletes itself. Skip with REISSUE=0.
if [ "${REISSUE:-1}" != "0" ]; then
  THROWAWAY_FQDN="a-og-throwaway-$RANDOM.example.local"
  echo "== re-issue check on a throwaway cert ($THROWAWAY_FQDN, created and removed by this script)"
  code=$("${CURL[@]}" -o "$TMP/new.json" -w '%{http_code}' -X POST -H 'Content-Type: application/json' \
    -d "{\"fqdn\":\"$THROWAWAY_FQDN\"}" "$BASE/api/certs")
  if [ "$code" != 201 ]; then fail "could not create the throwaway cert (POST /api/certs = $code); skipping the re-issue check"
  else
    tid=$(json 'print(d["cert"]["id"])' < "$TMP/new.json"); THROWAWAY_IDS="$tid"
    body="{\"fqdn\":\"$THROWAWAY_FQDN\",\"dnsSans\":[\"extra.$THROWAWAY_FQDN\"],\"ipSans\":[],\"validityDays\":30}"
    code=$("${CURL[@]}" -o "$TMP/edit.json" -w '%{http_code}' -X POST -H 'Content-Type: application/json' -d "$body" "$BASE/api/certs/$tid/edit")
    "${CURL[@]}" "$BASE/api/certs?fqdn=$THROWAWAY_FQDN&status=active" > "$TMP/t1.json"
    nid=$(json 'print(d["certs"][0]["id"])' < "$TMP/t1.json" 2>/dev/null)
    THROWAWAY_IDS="$tid $nid"
    if [ "$code" = 201 ] && [ -n "$nid" ] && [ "$nid" != "$tid" ]; then pass "re-issue: edit returned 201 and the filtered list now returns the new id ($tid -> $nid)"; else fail "re-issue: edit=$code, id $tid -> $nid"; fi
  fi
fi

echo
if [ "$fails" = 0 ]; then echo "ALL CHECKS PASSED (A-OG curl acceptance)"; else echo "$fails CHECK(S) FAILED"; exit 1; fi
