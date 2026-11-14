#!/usr/bin/env python3
"""Check a finished webcalc trial against the spec in agentflow-webcalc-kickoff.py.

This is the acceptance test the models never see and did not write: it builds the effort
branch in a temporary worktree (your checkout is not touched), starts the program, and
probes it black-box on the port the SPEC names (7070). It refuses to start when that port
is already taken, because a stale server would answer instead and the result would be
about the wrong program.

  scripts/agentflow-webcalc-probe.py --repo ~/af-webcalc-local --branch agentflow/effort-4 [--json out.json]

Checks come in four groups. "build" (go build, go test, listens on the port) and "api" (the
JSON contract) decide success; "ui" (the page driven in headless Chromium by agentflow-webcalc-page.mjs: typing, clicking, where its requests
go) also decides: a page a person cannot use is a failed build. "page" (the served HTML read as text) is reported
on its own line and never decides it.

Exit status: 0 when every check passes, 1 when a build or api check fails, 2 when only page
checks fail. --json writes every check (name, group, ok, detail) and the group totals.
The v1 spec (four operations and history) was scored by an earlier version of this script; efforts
3 to 6 were measured with it.
"""
import argparse
import json
import math
import os
import re
import shutil
import socket
import subprocess
import sys
import tempfile
import threading
import time
import urllib.error
import urllib.request

PORT = 7070  # from the spec: "runs locally on port 7070"

ap = argparse.ArgumentParser()
ap.add_argument("--repo", required=True)
ap.add_argument("--branch", required=True)
ap.add_argument("--json", metavar="PATH", help="write the full result as JSON")
args = ap.parse_args()
repo = os.path.expanduser(args.repo)


def port_open(port):
    with socket.socket() as s:
        s.settimeout(0.5)
        return s.connect_ex(("127.0.0.1", port)) == 0


if port_open(PORT):
    sys.exit(f"port {PORT} is already in use: stop whatever is listening before probing")

results = []  # (name, group, ok, detail)


def check(name, ok, detail="", group="api"):
    ok = bool(ok)
    results.append((name, group, ok, detail))
    print(("PASS " if ok else "FAIL ") + f"[{group}] " + name + ("" if ok or not detail else f"  [{detail}]"), flush=True)


def call(method, path, body=None, raw=None, headers=False):
    data = raw if raw is not None else (None if body is None else json.dumps(body).encode())
    req = urllib.request.Request(f"http://127.0.0.1:{PORT}{path}", method=method, data=data)
    if data is not None:
        req.add_header("Content-Type", "application/json")
    try:
        with urllib.request.urlopen(req, timeout=10) as r:
            return (r.status, r.read(), r.headers) if headers else (r.status, r.read())
    except urllib.error.HTTPError as e:
        return (e.code, e.read(), e.headers) if headers else (e.code, e.read())
    except Exception as e:  # connection refused and the like
        return (0, str(e).encode(), {}) if headers else (0, str(e).encode())


def js(raw):
    try:
        return json.loads(raw)
    except Exception:
        return None


def close(got, want):
    return isinstance(got, (int, float)) and not isinstance(got, bool) and math.isclose(got, want, rel_tol=1e-9, abs_tol=1e-9)


def result_is(path, body, want, label=None):
    code, raw = call("POST", path, body)
    r = (js(raw) or {}).get("result") if isinstance(js(raw), dict) else None
    check(label or f"POST {path} {json.dumps(body)} -> {want}", code == 200 and close(r, want), f"{code} {raw[:80]!r}")


def error_is(path, body, want_error, label=None):
    code, raw = call("POST", path, body)
    got = (js(raw) or {}).get("error") if isinstance(js(raw), dict) else None
    check(label or f"POST {path} {json.dumps(body)} -> 400 {{error: {want_error}}}", code == 400 and got == want_error, f"{code} {raw[:80]!r}")


def has_number(entry, *ns):
    return any(re.search(rf"(?<![0-9.]){re.escape(str(n))}(?![0-9.])", json.dumps(entry)) for n in ns)


tmp = tempfile.mkdtemp(prefix="webcalc-probe-")
wt = os.path.join(tmp, "wt")
proc = None
try:
    subprocess.run(["git", "worktree", "add", "-q", "--detach", wt, args.branch], cwd=repo, check=True)
    binary = os.path.join(tmp, "calc")
    b = subprocess.run(["go", "build", "-o", binary, "."], cwd=wt, capture_output=True, text=True)
    check("go build", b.returncode == 0, b.stderr[-300:], "build")
    t = subprocess.run(["go", "test", "./..."], cwd=wt, capture_output=True, text=True)
    check("go test ./...", t.returncode == 0, (t.stdout + t.stderr)[-300:], "build")
    if b.returncode != 0:
        raise SystemExit
    proc = subprocess.Popen([binary], cwd=wt, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
    for _ in range(50):
        if port_open(PORT):
            break
        time.sleep(0.1)
    up = port_open(PORT)
    check(f"listens on port {PORT}", up, "", "build")
    if not up:
        raise SystemExit

    # The four basic operations, and the numbers a sloppy implementation gets wrong.
    for op, a, b2, want in (("add", 7, 2, 9), ("subtract", 7, 2, 5), ("multiply", 7, 2, 14), ("divide", 7, 2, 3.5)):
        result_is(f"/api/{op}", {"a": a, "b": b2}, want)
    result_is("/api/add", {"a": 0.1, "b": 0.2}, 0.3, "add 0.1 + 0.2 is 0.3 (floats)")
    result_is("/api/subtract", {"a": 2, "b": 5}, -3, "subtract to a negative result")
    result_is("/api/divide", {"a": -7, "b": 2}, -3.5, "divide a negative number")
    result_is("/api/multiply", {"a": 1e15, "b": 10}, 1e16, "multiply to a large result")
    error_is("/api/divide", {"a": 1, "b": 0}, "division by zero", "divide by zero -> 400 {error: division by zero}")

    # More operations.
    result_is("/api/power", {"a": 2, "b": 10}, 1024)
    result_is("/api/power", {"a": 2, "b": -1}, 0.5, "power with a negative exponent")
    error_is("/api/power", {"a": 0, "b": -1}, "domain error", "0 to the power -1 -> 400 {error: domain error}")
    result_is("/api/sqrt", {"a": 16}, 4)
    error_is("/api/sqrt", {"a": -1}, "domain error", "sqrt of a negative -> 400 {error: domain error}")
    result_is("/api/modulo", {"a": 7, "b": 3}, 1)
    result_is("/api/modulo", {"a": 7.5, "b": 2}, 1.5, "modulo of floats")
    result_is("/api/modulo", {"a": -7, "b": 3}, -1, "modulo keeps the sign of the dividend")
    error_is("/api/modulo", {"a": 7, "b": 0}, "division by zero", "modulo by zero -> 400 {error: division by zero}")

    result_is("/api/sin", {"a": 0}, 0)
    result_is("/api/sin", {"a": math.pi / 2}, 1, "sin(pi/2) is 1")
    result_is("/api/cos", {"a": 0}, 1)
    result_is("/api/cos", {"a": math.pi}, -1, "cos(pi) is -1")
    result_is("/api/tan", {"a": math.pi / 4}, 1, "tan(pi/4) is 1")
    result_is("/api/tan", {"a": 0}, 0)
    result_is("/api/factorial", {"a": 0}, 1, "0! is 1")
    result_is("/api/factorial", {"a": 5}, 120)
    result_is("/api/factorial", {"a": 20}, 2432902008176640000, "20! is exact")
    result_is("/api/factorial", {"a": 170}, 7.257415615307994e306, "170! is the largest that fits")
    for bad in (-1, 2.5, 171):
        error_is("/api/factorial", {"a": bad}, "domain error", f"factorial of {bad} -> 400 {{error: domain error}}")
    code, raw = call("POST", "/api/primes", {"count": 10})
    check("primes: the first ten", code == 200 and (js(raw) or {}).get("result") == [2, 3, 5, 7, 11, 13, 17, 19, 23, 29], f"{code} {raw[:80]!r}")
    code, raw = call("POST", "/api/primes", {"count": 1})
    check("primes: the first one is [2]", code == 200 and (js(raw) or {}).get("result") == [2], f"{code} {raw[:80]!r}")
    code, raw = call("POST", "/api/primes", {"count": 1000})
    r = (js(raw) or {}).get("result")
    check("primes: 1000 of them, the last is 7919", code == 200 and isinstance(r, list) and len(r) == 1000 and r[-1] == 7919, f"{code} {raw[:40]!r}")
    for bad in (0, 1001, 2.5, -3):
        error_is("/api/primes", {"count": bad}, "domain error", f"primes count {bad} -> 400 {{error: domain error}}")

    # Bad requests.
    code, raw = call("POST", "/api/add", raw=b"not json")
    check("invalid JSON -> 400 with an error", code == 400 and "error" in (js(raw) or {}), f"{code} {raw[:80]!r}")
    for body in ({"a": 1}, {"a": "x", "b": 2}, {}):
        code, raw = call("POST", "/api/add", body)
        check(f"missing or non-numeric field {json.dumps(body)} -> 400 with an error", code == 400 and "error" in (js(raw) or {}), f"{code} {raw[:80]!r}")
    code, raw = call("POST", "/api/sqrt", {})
    check("sqrt without a -> 400 with an error", code == 400 and "error" in (js(raw) or {}), f"{code} {raw[:80]!r}")
    code, raw = call("GET", "/api/add")
    check("GET on a POST endpoint -> 405", code == 405, str(code))
    code, raw = call("POST", "/api/history", {})
    check("POST on /api/history -> 405", code == 405, str(code))
    code, raw, hd = call("POST", "/api/add", {"a": 1, "b": 1}, headers=True)
    check("API answers with Content-Type application/json", "application/json" in (hd.get("Content-Type") or ""), str(hd.get("Content-Type")))

    # /api/evaluate: precedence, parentheses, unary minus, associativity, errors.
    for expr, want in (("2+3*4", 14), ("(2+3)*4", 20), ("-3+5", 2), ("2*-3", -6), ("10/4", 2.5), ("1.5+1.5", 3),
                       (" 2 + 2 ", 4), ("2*(3+4)*5", 70), ("-(2+3)", -5), ("2--3", 5), ("8/2/2", 2), ("2-3-4", -5),
                       ("((7))", 7), ("0.1*3", 0.3),
                       ("2^3", 8), ("2^3^2", 512), ("-2^2", -4), ("(-2)^2", 4), ("2*3^2", 18), ("2^-1", 0.5), ("sqrt(16)", 4),
                       ("sqrt(9)+1", 4), ("2*sqrt(2+2)", 4), ("sin(0)", 0), ("cos(0)", 1), ("cos(pi)", -1), ("2*pi", 2 * math.pi),
                       ("sin(pi/2)", 1), ("tan(pi/4)", 1), ("-sqrt(4)", -2), ("sqrt(sqrt(16))", 2)):
        result_is("/api/evaluate", {"expression": expr}, want, f"evaluate {expr!r} -> {want}")
    for expr in ("2+", "(1+2", "1+2)", "abc", "", "2 3", "*2", "2**3", "1..2", "sin", "sin()", "foo(1)", "2^", "pi pi", "sqrt 4", "SQRT(4)"):
        error_is("/api/evaluate", {"expression": expr}, "invalid expression", f"evaluate {expr!r} -> 400 {{error: invalid expression}}")
    error_is("/api/evaluate", {"expression": "1/0"}, "division by zero", "evaluate '1/0' -> 400 {error: division by zero}")
    error_is("/api/evaluate", {"expression": "4/(2-2)"}, "division by zero", "evaluate '4/(2-2)' -> 400 {error: division by zero}")
    error_is("/api/evaluate", {"expression": "sqrt(-1)"}, "domain error", "evaluate 'sqrt(-1)' -> 400 {error: domain error}")
    error_is("/api/evaluate", {"expression": "0^-1"}, "domain error", "evaluate '0^-1' -> 400 {error: domain error}")
    code, raw = call("POST", "/api/evaluate", {})
    check("evaluate without expression -> 400 with an error", code == 400 and "error" in (js(raw) or {}), f"{code} {raw[:80]!r}")

    # History: shape, failed calls left out, newest first, capped at ten, cleared by DELETE.
    code, raw = call("DELETE", "/api/history")
    check("DELETE /api/history clears (2xx)", 200 <= code < 300, str(code))
    call("POST", "/api/evaluate", {"expression": "2+3*4"})
    code, raw = call("GET", "/api/history")
    h = js(raw)
    check("history entry is {expression, result} and keeps the evaluate text as sent",
          code == 200 and isinstance(h, list) and len(h) == 1 and h[0].get("expression") == "2+3*4" and close(h[0].get("result"), 14), f"{code} {raw[:100]!r}")
    call("POST", "/api/divide", {"a": 1, "b": 0})
    call("POST", "/api/evaluate", {"expression": "2+"})
    call("POST", "/api/add", raw=b"nope")
    call("POST", "/api/primes", {"count": 5})
    code, raw = call("GET", "/api/history")
    check("failed calls and the primes list are not recorded", isinstance(js(raw), list) and len(js(raw)) == 1, raw[:100].decode(errors="replace"))
    call("DELETE", "/api/history")
    for i in range(1, 13):
        call("POST", "/api/add", {"a": i, "b": 1})
    code, raw = call("GET", "/api/history")
    h = js(raw)
    check("GET /api/history keeps the last ten", code == 200 and isinstance(h, list) and len(h) == 10, f"{code} {raw[:80]!r}")
    # The calls were add(i, 1) for i = 1..12: the newest entry mentions 12 (or its result 13), the oldest
    # one kept (the third call) mentions 3 (or 4).
    check("history is newest first", isinstance(h, list) and len(h) == 10 and has_number(h[0], 12, 13) and has_number(h[-1], 3, 4), raw[:80].decode(errors="replace"))
    check("every history entry has expression (string) and result (number)",
          isinstance(h, list) and all(isinstance(e.get("expression"), str) and isinstance(e.get("result"), (int, float)) for e in h if isinstance(e, dict)) and all(isinstance(e, dict) for e in h), "")
    call("DELETE", "/api/history")
    code2, raw2 = call("GET", "/api/history")
    check("history is empty after DELETE", code2 == 200 and js(raw2) == [], f"{code2} {raw2[:80]!r}")

    # Many callers at once: the history must stay consistent and the server must keep answering.
    call("DELETE", "/api/history")
    codes = []
    lock = threading.Lock()

    def hammer(n):
        c, _ = call("POST", "/api/add", {"a": n, "b": 1})
        with lock:
            codes.append(c)
    threads = [threading.Thread(target=hammer, args=(n,)) for n in range(40)]
    for th in threads:
        th.start()
    for th in threads:
        th.join()
    code, raw = call("GET", "/api/history")
    check("40 concurrent calls: all answered 200, history has exactly ten", codes.count(200) == 40 and code == 200 and isinstance(js(raw), list) and len(js(raw)) == 10,
          f"{codes.count(200)} ok, history {raw[:60]!r}")

    # The page: a script cannot run its JavaScript, so these look at the HTML only and never decide success.
    code, raw = call("GET", "/")
    page = raw.decode(errors="replace").lower()
    check("GET / serves the page", code == 200 and "<button" in page, str(code), "page")
    check("page has digits 0-9, decimal point, equals, clear, backspace",
          all(re.search(rf">\s*{re.escape(d)}\s*<", page) for d in "0123456789.=") and "clear" in page.replace("\"", " ") and ("backspace" in page or "⌫" in page or "←" in page), "", "page")
    check("page does arithmetic through the API", "/api/" in page, "", "page")
    check("page handles keyboard input", any(k in page for k in ("keydown", "keypress", "keyup")), "", "page")
    check("page shows the history list", "history" in page, "", "page")
    # Behaviour, as a person would use it: a headless Chromium types on the real keyboard, clicks, and reads the display.
    # The HTML checks above only look for words; these catch a page that never reaches the API, calls another host,
    # cannot take typed input, or has no way to enter ^ and parentheses.
    page_script = os.path.join(os.path.dirname(os.path.abspath(__file__)), "agentflow-webcalc-page.mjs")
    try:
        out = subprocess.run(["node", page_script, f"http://127.0.0.1:{PORT}/"], capture_output=True, text=True, timeout=120)
        for c in json.loads(out.stdout.strip().splitlines()[-1])["checks"]:
            check(c["name"], c["ok"], c.get("detail", ""), "ui")
    except Exception as exc:  # no node, no browser, or unreadable output: say so, as a failed page check
        check("page behaviour in a browser", False, f"could not run {page_script}: {exc}", "ui")
except SystemExit:
    pass
finally:
    if proc:
        proc.terminate()
        try:
            proc.wait(5)
        except subprocess.TimeoutExpired:
            proc.kill()
    subprocess.run(["git", "worktree", "remove", "--force", wt], cwd=repo, capture_output=True)
    shutil.rmtree(tmp, ignore_errors=True)

totals = {}
for _, g, ok, _ in results:
    t = totals.setdefault(g, {"passed": 0, "total": 0})
    t["total"] += 1
    t["passed"] += 1 if ok else 0
decisive_failed = [n for n, g, ok, _ in results if g != "page" and not ok]
page_failed = [n for n, g, ok, _ in results if g == "page" and not ok]
api = totals.get("api", {"passed": 0, "total": 0})
build = totals.get("build", {"passed": 0, "total": 0})
page = totals.get("page", {"passed": 0, "total": 0})
ui = totals.get("ui", {"passed": 0, "total": 0})
print(f"\nbuild {build['passed']}/{build['total']}   API {api['passed']}/{api['total']}   UI (browser) {ui['passed']}/{ui['total']}   page text {page['passed']}/{page['total']}")
print("RESULT: " + ("SUCCESS" if not decisive_failed else "FAILED") + (" (page checks failed)" if not decisive_failed and page_failed else ""))
if args.json:
    with open(os.path.expanduser(args.json), "w") as f:
        json.dump({"branch": args.branch, "success": not decisive_failed, "totals": totals,
                   "checks": [{"name": n, "group": g, "ok": ok, "detail": d} for n, g, ok, d in results]}, f, indent=2)
sys.exit(1 if decisive_failed else (2 if page_failed else 0))
