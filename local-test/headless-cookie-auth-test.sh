#!/usr/bin/env bash
# Throwaway proof-of-concept: does a same-machine, headless-Chrome-backed
# yt-dlp session survive where a pasted-cookie-file session did not?
#
# Not part of the app. Not wired into utuber/taskmaster. Just testing the
# theory: run headless Chrome on THIS box, log in once via a DevTools tunnel,
# then let yt-dlp read cookies live from that same profile.
#
# Usage:
#   ./test-headless-cookie-auth.sh [youtube-url]
# Defaults to the URL that was failing in manual E2 testing.

set -euo pipefail

URL="${1:-https://youtu.be/BI8DU38E_r0}"
PORT="${PORT:-9222}"
PROFILE_DIR="${PROFILE_DIR:-$HOME/.cache/utuber-cookie-test-profile}"
DOWNLOAD_DIR="$(mktemp -d /tmp/utuber-cookie-test-download.XXXXXX)"
CHROME_BIN="$(command -v google-chrome || command -v google-chrome-stable || command -v chromium || command -v chromium-browser)"

if [ -z "$CHROME_BIN" ]; then
  echo "No Chrome/Chromium binary found on PATH." >&2
  exit 1
fi
if ! command -v yt-dlp >/dev/null; then
  echo "yt-dlp not found on PATH." >&2
  exit 1
fi
if ! command -v jq >/dev/null; then
  echo "jq not found on PATH (needed to parse the DevTools target list)." >&2
  exit 1
fi

mkdir -p "$PROFILE_DIR"

echo "== Profile dir:    $PROFILE_DIR"
echo "== Debug port:     $PORT"
echo "== Download dir:   $DOWNLOAD_DIR"
echo "== Target URL:     $URL"
echo

# ---------------------------------------------------------------------------
# 1. Launch headless Chrome with a persistent profile and the DevTools port
#    bound to localhost only. No X/Wayland/VNC involved anywhere in this.
# ---------------------------------------------------------------------------
if curl -s "http://127.0.0.1:$PORT/json/version" >/dev/null 2>&1; then
  echo "Debug port $PORT already answering -- reusing the Chrome instance already"
  echo "running against this profile instead of starting a second one (a second"
  echo "Chrome pointed at the same --user-data-dir would just fail to launch)."
  CHROME_PID=""
else
  echo "Launching headless Chrome..."
  "$CHROME_BIN" \
    --headless=new \
    --disable-gpu \
    --no-first-run \
    --no-default-browser-check \
    --remote-debugging-address=127.0.0.1 \
    --remote-debugging-port="$PORT" \
    --user-data-dir="$PROFILE_DIR" \
    about:blank \
    >/tmp/utuber-cookie-test-chrome.log 2>&1 &
  CHROME_PID=$!
fi

cleanup() {
  echo
  if [ -n "$CHROME_PID" ]; then
    echo "Headless Chrome is still running (pid $CHROME_PID, profile $PROFILE_DIR)."
    echo "Leaving it up in case you want to rerun the yt-dlp step without logging in again."
    echo "Kill it with:  kill $CHROME_PID"
  else
    echo "Left the already-running Chrome instance (profile $PROFILE_DIR) alone."
    echo "Find its pid with:  pgrep -f 'remote-debugging-port=$PORT'"
  fi
}
trap cleanup EXIT

# Give Chrome a moment to open the debug port.
for _ in $(seq 1 20); do
  curl -s "http://127.0.0.1:$PORT/json/version" >/dev/null 2>&1 && break
  sleep 0.5
done
if ! curl -s "http://127.0.0.1:$PORT/json/version" >/dev/null 2>&1; then
  echo "Chrome's debug port never came up. Check /tmp/utuber-cookie-test-chrome.log" >&2
  exit 1
fi

# ---------------------------------------------------------------------------
# 2. Open a tab at Google sign-in and grab its DevTools inspector URL, which
#    renders the live remote tab in whatever browser you open it in -- no
#    VNC/X11, just this one HTTP URL over the SSH tunnel below.
# ---------------------------------------------------------------------------
# Newer Chrome requires PUT (not GET) for /json/new -- a CSRF hardening fix
# so a random webpage can't spawn tabs on your debug port just by linking to
# it. GET returns a plain-text error here, not JSON.
TAB_JSON="$(curl -s -X PUT "http://127.0.0.1:$PORT/json/new?https://accounts.google.com/ServiceLogin?service=youtube")"
TAB_ID="$(echo "$TAB_JSON" | jq -r '.id' 2>/dev/null)"
if [ -z "$TAB_ID" ] || [ "$TAB_ID" = "null" ]; then
  echo "Failed to open a new tab via the DevTools API. Raw response:" >&2
  echo "$TAB_JSON" >&2
  exit 1
fi
unused="$TAB_ID" # kept so the tab exists and shows up in chrome://inspect

echo
echo "############################################################"
echo "# 1. On YOUR OWN machine, open a tunnel:"
echo "#"
echo "#      ssh -L ${PORT}:localhost:${PORT} <user>@<this-host>"
echo "#"
echo "# 2. In a REAL Chrome browser on YOUR machine, go to:"
echo "#"
echo "#      chrome://inspect/#devices"
echo "#"
echo "#    Click 'Configure...' and add:   localhost:${PORT}"
echo "#    A 'Remote Target' should appear below with a Google sign-in"
echo "#    tab under it -- click its 'inspect' link."
echo "#"
echo "#    (Don't just navigate to http://localhost:${PORT}/devtools/...";
echo "#    directly -- if your browser force-upgrades http to https"
echo "#    (Chrome's 'Always use secure connections' setting, or an"
echo "#    extension like HTTPS-Everywhere), the debug WebSocket dies"
echo "#    immediately and DevTools just shows 'Reconnect when ready.'"
echo "#    chrome://inspect is Chrome's own trusted page and isn't"
echo "#    subject to that rewrite.)"
echo "#"
echo "# 3. Sign into the Google account there, complete YouTube's age"
echo "#    verification if it's prompted, and confirm you land on"
echo "#    youtube.com signed in."
echo "############################################################"
echo
read -rp "Press Enter once you're signed in... "

# ---------------------------------------------------------------------------
# 3. yt-dlp reads cookies live from the on-disk profile -- same machine,
#    same IP as the login that just happened.
# ---------------------------------------------------------------------------
echo
echo "Running yt-dlp against the profile now..."
echo

set +e
yt-dlp \
  --cookies-from-browser "chrome:${PROFILE_DIR}" \
  -o "${DOWNLOAD_DIR}/%(title)s.%(ext)s" \
  -v \
  "$URL"
STATUS=$?
set -e

echo
if [ "$STATUS" -eq 0 ]; then
  echo "RESULT: SUCCESS. Downloaded into $DOWNLOAD_DIR:"
  ls -la "$DOWNLOAD_DIR"
else
  echo "RESULT: FAILED (yt-dlp exit code $STATUS). See the -v output above."
fi
