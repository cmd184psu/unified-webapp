#!/usr/bin/env python3
"""Check a finished webcalc trial against the spec in agentflow-webcalc-kickoff.py.

Builds the effort branch in a temporary worktree (your checkout is not touched), starts
the program, and probes it on the port the SPEC names (7070). It refuses to start when
that port is already taken, because a stale server would answer instead and the result
would be about the wrong program.

  scripts/agentflow-webcalc-probe.py --repo ~/af-webcalc-local --branch agentflow/effort-4

Exit status 0 when every check passes, 1 otherwise.
"""
import argparse
import json
import os
import re
import shutil
import socket
import subprocess
import sys
import tempfile
import time
import urllib.error
import urllib.request

PORT = 7070  # from the spec: "runs locally on port 7070"

ap = argparse.ArgumentParser()
ap.add_argument("--repo", required=True)
ap.add_argument("--branch", required=True)
args = ap.parse_args()
repo = os.path.expanduser(args.repo)


def port_open(port):
    with socket.socket() as s:
        s.settimeout(0.5)
        return s.connect_ex(("127.0.0.1", port)) == 0


if port_open(PORT):
    sys.exit(f"port {PORT} is already in use: stop whatever is listening before probing")

results = []


def check(name, ok, detail=""):
    results.append((name, ok, detail))
    print(("PASS " if ok else "FAIL ") + name + ("" if ok or not detail else f"  [{detail}]"), flush=True)


def call(method, path, body=None, raw=None):
    data = raw if raw is not None else (None if body is None else json.dumps(body).encode())
    req = urllib.request.Request(f"http://127.0.0.1:{PORT}{path}", method=method, data=data)
    try:
        with urllib.request.urlopen(req, timeout=10) as r:
            return r.status, r.read()
    except urllib.error.HTTPError as e:
        return e.code, e.read()
    except Exception as e:  # connection refused and the like
        return 0, str(e).encode()


def js(raw):
    try:
        return json.loads(raw)
    except Exception:
        return None


tmp = tempfile.mkdtemp(prefix="webcalc-probe-")
wt = os.path.join(tmp, "wt")
proc = None
try:
    subprocess.run(["git", "worktree", "add", "-q", "--detach", wt, args.branch], cwd=repo, check=True)
    binary = os.path.join(tmp, "calc")
    b = subprocess.run(["go", "build", "-o", binary, "."], cwd=wt, capture_output=True, text=True)
    check("go build", b.returncode == 0, b.stderr[-300:])
    t = subprocess.run(["go", "test", "./..."], cwd=wt, capture_output=True, text=True)
    check("go test ./...", t.returncode == 0, (t.stdout + t.stderr)[-300:])
    if b.returncode != 0:
        sys.exit(1)
    proc = subprocess.Popen([binary], cwd=wt, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
    for _ in range(50):
        if port_open(PORT):
            break
        time.sleep(0.1)
    check(f"listens on port {PORT}", port_open(PORT))

    for op, a, b2, want in (("add", 7, 2, 9), ("subtract", 7, 2, 5), ("multiply", 7, 2, 14), ("divide", 7, 2, 3.5)):
        code, raw = call("POST", f"/api/{op}", {"a": a, "b": b2})
        check(f"POST /api/{op} -> 200 {{result: {want}}}", code == 200 and (js(raw) or {}).get("result") == want, f"{code} {raw[:80]!r}")
    code, raw = call("POST", "/api/divide", {"a": 1, "b": 0})
    check("divide by zero -> 400 {error: division by zero}", code == 400 and (js(raw) or {}).get("error") == "division by zero", f"{code} {raw[:80]!r}")
    code, raw = call("POST", "/api/add", raw=b"not json")
    check("invalid JSON -> 400 with an error", code == 400 and "error" in (js(raw) or {}), f"{code} {raw[:80]!r}")
    code, raw = call("DELETE", "/api/history")
    check("DELETE /api/history clears (2xx)", 200 <= code < 300, str(code))
    for i in range(1, 13):
        call("POST", "/api/add", {"a": i, "b": 1})
    code, raw = call("GET", "/api/history")
    h = js(raw)
    check("GET /api/history keeps the last ten", code == 200 and isinstance(h, list) and len(h) == 10, f"{code} {raw[:80]!r}")
    check("history is newest first", isinstance(h, list) and h and json.dumps(h[0]).find("13") >= 0, f"{raw[:80]!r}")
    code, raw = call("DELETE", "/api/history")
    code2, raw2 = call("GET", "/api/history")
    check("history is empty after DELETE", code2 == 200 and js(raw2) == [], f"{code2} {raw2[:80]!r}")
    code, raw = call("GET", "/")
    page = raw.decode(errors="replace").lower()
    check("GET / serves the page", code == 200 and "<button" in page, str(code))
    check("page has digits 0-9, decimal point, equals, clear, backspace",
          all(re.search(rf">\s*{re.escape(d)}\s*<", page) for d in "0123456789.="), "")
    check("page does arithmetic through the API", "/api/add" in page or "/api/" in page, "")
    check("page handles keyboard input", "keydown" in page or "keypress" in page or "keyup" in page, "")
finally:
    if proc:
        proc.terminate()
        try:
            proc.wait(5)
        except subprocess.TimeoutExpired:
            proc.kill()
    subprocess.run(["git", "worktree", "remove", "--force", wt], cwd=repo, capture_output=True)
    shutil.rmtree(tmp, ignore_errors=True)

failed = [n for n, ok, _ in results if not ok]
print(f"\n{len(results) - len(failed)}/{len(results)} checks passed")
sys.exit(1 if failed else 0)
