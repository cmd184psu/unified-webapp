#!/usr/bin/env python3
"""Start the Go web calculator trial on the RUNNING agentflow service, so it can be watched in the UI.

Uses the service's existing backends and candidate lists (nothing is overwritten): set those
up in the UI first. Creates a git repo, registers the project `webcalc`, drafts the FRD from
one sentence, approves the FRD, generates and approves the plan, waits for the automatic
compile, approves the queue and starts the run. Then it exits; watch the rest in the UI.

  scripts/agentflow-webcalc-kickoff.py [--url http://127.0.0.1:8787]
        [--host agentflow.builder.cmdhome.net] [--pin-file ~/.unified_common.pin]
        [--repo ~/af-webcalc] [--slug webcalc] [--scope local|any] [--lineup global|local] [--wait]

--host is sent as the Host header: the service routes modules by host name.
--lineup local sets this project's own candidate lists from the registered backends' tiers instead
of using the global lists: planner, architect, critic and compiler take tier 1 then 2 (a model
not yet rated counts as 2); workers take tier 2, then 1; reviewers take tier 1, then 2. Only
non-hosted backends are used, and light (tier 3) models only with --include-light.
--wait stays until the effort completes or a task halts, printing status changes.
"""
import argparse
import http.cookiejar
import json
import os
import subprocess
import sys
import time
import urllib.error
import urllib.request

SENTENCE = (
    "Build a full-featured calculator web app in Go that runs locally on port 7070, with no authentication and no TLS. "
    "The Go server in the repository root (package main, started with `go run .`) serves a single page with digit buttons, "
    "a decimal point, the operators add, subtract, multiply and divide, equals, clear, backspace, keyboard input, and a list "
    "of the last ten calculations. All four arithmetic operations must be performed by Go API calls, never in JavaScript: "
    "POST /api/add, /api/subtract, /api/multiply and /api/divide each take JSON {\"a\": number, \"b\": number} and return "
    "{\"result\": number}; dividing by zero returns HTTP 400 with {\"error\": \"division by zero\"}; invalid JSON returns "
    "HTTP 400 with an error. The server keeps the history of the last ten calculations in memory and serves it from GET "
    "/api/history (newest first), and clears it with DELETE /api/history. The page is served from GET /. Include Go tests "
    "for the four operations, the error cases and the history.")

ap = argparse.ArgumentParser()
ap.add_argument("--url", default="http://127.0.0.1:8787")
ap.add_argument("--host", default="agentflow.builder.cmdhome.net")
ap.add_argument("--pin-file", default=os.path.expanduser("~/.unified_common.pin"))
ap.add_argument("--repo", default=os.path.expanduser("~/af-webcalc"))
ap.add_argument("--slug", default="webcalc")
ap.add_argument("--scope", default="any", choices=("local", "any"),
                help="model scope: any lets hosted backends (the free opencode models) be used")
ap.add_argument("--lineup", default="global", choices=("global", "local"),
                help="local: build this project's candidate lists from the non-hosted backends by tier")
ap.add_argument("--include-light", action="store_true", help="with --lineup local: let tier 3 models work as workers")
ap.add_argument("--exclude-backend", action="append", default=["kimi-waste"],
                help="with --lineup local: backend to leave out (repeatable; default kimi-waste, too slow for a worker)")
ap.add_argument("--backend-order", default="ollama-m5,ollama-win,ollama-m4",
                help="with --lineup local: within a tier, try these backends first, in this order (fastest first)")
ap.add_argument("--dry-run", action="store_true", help="with --lineup local: print the candidate lists and stop")
ap.add_argument("--resume", type=int, metavar="EFFORT",
                help="carry on an effort this script already started (same --slug and --repo): approve the gates it has reached and wait")
ap.add_argument("--wait", action="store_true")
args = ap.parse_args()
LONG = 6 * 3600  # planning and compiling on local models take hours, not minutes

jar = http.cookiejar.CookieJar()
opener = urllib.request.build_opener(urllib.request.HTTPCookieProcessor(jar))


def login():
    try:
        pin = open(args.pin_file).read().strip()
    except OSError:
        return
    api("POST", "/api/auth/login", {"method": "pin", "pin": pin}, _retry=False)


def api(method, path, body=None, want=None, _retry=True):
    req = urllib.request.Request(args.url + path, method=method,
                                 data=None if body is None else json.dumps(body).encode())
    req.add_header("Host", args.host)
    req.add_header("Content-Type", "application/json")
    try:
        with opener.open(req, timeout=60) as r:
            code, raw = r.status, r.read()
    except urllib.error.HTTPError as e:
        code, raw = e.code, e.read()
    if code == 401 and _retry and path != "/api/auth/login":  # the session expired during a long wait: log in again
        login()
        return api(method, path, body, want, _retry=False)
    if want is not None and code != want:
        sys.exit(f"{method} {path} -> {code} (want {want}): {raw[:300]!r}")
    try:
        return json.loads(raw) if raw else None
    except ValueError:
        return raw


def wait_for(what, fn, timeout, every=5):
    end = time.time() + timeout
    while time.time() < end:
        v = fn()
        if v:
            return v
        time.sleep(every)
    sys.exit(f"timed out waiting for {what}")


def say(m):
    print(m, flush=True)


# login (a PIN session is "anonymous", which is fine)
login()

health = api("GET", "/api/health", None, 200)
say(f"service ok; opencode {health.get('opencode', {}).get('status')}")
have = {c.get("role") for c in (api("GET", "/api/candidates", None, 200) or []) if c.get("scope") == "global"}
missing = [r for r in ("planner", "architect", "critic", "compiler", "worker", "reviewer") if r not in have]
if missing:
    sys.exit("no candidate configured for: " + ", ".join(missing) + " (add them in the UI first)")

if args.lineup == "local":
    pool = []  # (tier, backend, model); tier 0 (not rated) counts as 2
    for b in api("GET", "/api/backends", None, 200):
        if b.get("hosted") or b.get("paid") or b["name"] in args.exclude_backend:
            continue
        pool += [(m.get("tier") or 2, b["name"], m["model"]) for m in b["models"]]
    order = [x for x in args.backend_order.split(",") if x]
    rank = lambda be: order.index(be) if be in order else len(order)
    pick = lambda tiers: [{"backend": be, "model": m} for t in tiers
                          for (tt, be, m) in sorted(pool, key=lambda x: (rank(x[1]), x[1], x[2])) if tt == t]
    workers = (2, 3, 1) if args.include_light else (2, 1)
    lineup = {"planner": pick((1, 2)), "architect": pick((1, 2)), "critic": pick((1, 2)), "compiler": pick((1, 2)),
              "worker": pick(workers), "reviewer": pick((1, 2))}
    for role, lst in lineup.items():
        if not lst:
            sys.exit(f"--lineup local: no non-hosted model fits the {role} role")
        say(f"{role}: " + ", ".join(f"{c['backend'].replace('ollama-', '')}/{c['model']}" for c in lst))
    if args.dry_run:
        sys.exit(0)

# a fresh repo with one commit
if not os.path.isdir(os.path.join(args.repo, ".git")):
    os.makedirs(args.repo, exist_ok=True)
    g = lambda *a: subprocess.run(["git", "-c", "user.name=trial", "-c", "user.email=trial@localhost", *a],
                                  cwd=args.repo, check=True, capture_output=True)
    g("init", "-q", "-b", "main")
    open(os.path.join(args.repo, "go.mod"), "w").write("module webcalc\n\ngo 1.22\n")
    open(os.path.join(args.repo, ".gitignore"), "w").write("webcalc\n*.exe\n")
    g("add", "-A")
    g("commit", "-q", "-m", "initial")

slug = args.slug
if not args.resume:
    api("POST", "/api/projects", {"slug": slug, "repo_path": args.repo, "model_scope": args.scope}, 201)
if args.lineup == "local" and not args.resume:
    for role, lst in lineup.items():
        api("PUT", f"/api/projects/{slug}/candidates/{role}", {"candidates": lst}, 200)

eid = args.resume or api("POST", f"/api/projects/{slug}/efforts", {"title": "web calculator"}, 201)["id"]
base = f"/api/projects/{slug}/efforts/{eid}"
say(f"project {slug}, effort {eid}: open the agentflow UI to watch")
status = lambda: api("GET", base, None, 200)["status"]

if not args.resume:
    api("POST", base + "/frd/draft", {"description": SENTENCE, "polish": False}, 202)
    wait_for("draft", lambda: (api("GET", base + "/frd/draft") or {}).get("state") in ("idle", "failed"), LONG)
    api("POST", base + "/frd/approve", None, 200)
    api("POST", base + "/plan/generate", None, 202)
# From here on the same steps serve a new effort and a resumed one: each gate is
# approved only if the effort is waiting at it.
AFTER_PLAN = ("plan_ready", "queue_draft", "queue_approved", "running", "complete")
wait_for("plan_ready", lambda: status() in AFTER_PLAN, LONG)
if status() == "plan_ready":
    api("POST", base + "/plan/approve", None, 200)
wait_for("queue_draft", lambda: status() in AFTER_PLAN[1:], LONG)
if status() == "queue_draft":
    queue = api("GET", base + "/queue", None, 200)
    say("queue: " + "; ".join(f"{t['key']} {t['title']}" for t in queue["tasks"]))
    api("POST", base + "/queue/approve", {"hash": queue["hash"]}, 200)
api("POST", f"/api/projects/{slug}/run/start", None)  # already running is fine
say("run started")

if args.wait:
    last = None
    while True:
        s = status()
        ts = api("GET", base + "/tasks") or []
        line = s + " " + " ".join(f"{t['key']}={t.get('status')}" for t in ts if isinstance(t, dict))
        if line != last:
            say(line)
            last = line
        if s in ("complete", "abandoned") or any(isinstance(t, dict) and t.get("halted") for t in ts):
            break
        time.sleep(10)
say("when complete: cd " + args.repo + " && git checkout agentflow/effort-" + str(eid) + " && go run .   (then open http://localhost:7070)")
