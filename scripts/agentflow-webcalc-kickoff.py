#!/usr/bin/env python3
"""Start the Go web calculator trial on the RUNNING agentflow service, so it can be watched in the UI.

Uses the service's existing backends and candidate lists (nothing is overwritten): set those
up in the UI first. Creates a git repo, registers the project `webcalc`, drafts the FRD from
one sentence, approves the FRD, generates and approves the plan, waits for the automatic
compile, approves the queue and starts the run. Then it exits; watch the rest in the UI.

  scripts/agentflow-webcalc-kickoff.py [--url https://agentflow.builder.cmdhome.net]
        [--host agentflow.builder.cmdhome.net] [--pin-file ~/.unified_common.pin]
        [--repo ~/af-webcalc] [--slug webcalc] [--scope local|any] [--lineup global|local] [--wait]

--host is sent as the Host header: the service routes modules by host name.
--lineup hybrid uses the free hosted models for planning, compiling and review and the local models for the workers (a hosted model needs --scope any). --lineup free uses only hosted, unpaid backends for every role (the free opencode models, in a fixed rotation so a
reviewer is not the worker's own model). --lineup local sets this project's own candidate lists from the registered backends' tiers instead
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

# Spec v2. scripts/agentflow-webcalc-probe.py checks exactly what this promises: change both together.
SENTENCE = (
    "Build a full-featured calculator web app in Go that runs locally on port 7070, with no authentication and no TLS. "
    "The Go server in the repository root (package main, started with `go run .`) serves a single page with digit buttons, "
    "a decimal point, the operators add, subtract, multiply and divide, equals, clear, backspace, keyboard input, and a list "
    "of the last ten calculations. All arithmetic must be performed by Go API calls, never in JavaScript, and the page "
    "must call the API on the same origin it was served from, so it works from any browser that can reach the server. "
    "A person must be able to reach everything the API offers from the page: the display accepts typed input, so any "
    "expression that POST /api/evaluate accepts can be typed and evaluated, and buttons also exist for ^, parentheses, pi, "
    "sqrt, sin, cos, tan, modulo and factorial; an error from the API is shown as its message. "
    "The calculation endpoints accept only POST and /api/history accepts only GET and DELETE; any other method returns HTTP 405. "
    "Every API response has Content-Type application/json. "
    "POST /api/add, /api/subtract, /api/multiply, /api/divide, /api/power (a to the power b) and /api/modulo (the remainder of a "
    "divided by b, with the sign of a, like Go's math.Mod) each take JSON {\"a\": number, \"b\": number} and return {\"result\": number}; "
    "POST /api/sqrt, /api/sin, /api/cos and /api/tan (angles in radians) take {\"a\": number} and return {\"result\": number}. "
    "POST /api/factorial takes {\"a\": number}, which must be an integer from 0 to 170, and returns its factorial. "
    "POST /api/primes takes {\"count\": integer from 1 to 1000} and returns {\"result\": [the first count prime numbers, ascending]}; "
    "because its result is a list, it is not recorded in the history. "
    "Invalid JSON, or a missing or non-numeric field, returns HTTP 400 with {\"error\": message}. "
    "Dividing or taking a modulo by zero returns HTTP 400 with {\"error\": \"division by zero\"}; the square root of a negative number, "
    "a factorial or primes argument outside its range, or a power whose result is not a finite number (for example 0 to the power -1), "
    "returns HTTP 400 with {\"error\": \"domain error\"}. "
    "POST /api/evaluate takes {\"expression\": string} and returns {\"result\": number}: the expression uses decimal numbers, the constant pi, "
    "+ - * /, ^ (power: right-associative, binds tighter than * and /, and tighter than a unary minus before its base, so -2^2 is -4 "
    "and 2^3^2 is 512; the exponent may carry a unary minus, so 2^-1 is 0.5), parentheses, unary minus (before a number, a function or a "
    "parenthesised group), and the functions sqrt, sin, cos and tan applied to a parenthesised expression, with the usual precedence "
    "and left-to-right associativity for the rest; whitespace is ignored. Anything else (an unknown name, a missing operand or "
    "parenthesis, an empty expression) returns HTTP 400 with {\"error\": \"invalid expression\"}; a division by zero inside it returns "
    "{\"error\": \"division by zero\"} and a domain error inside it (such as sqrt(-1)) returns {\"error\": \"domain error\"}. "
    "The server keeps the last ten successful calculations in memory (a failed call is not recorded) and serves them from GET "
    "/api/history, newest first, as a JSON list of {\"expression\": string, \"result\": number}; for /api/evaluate the expression "
    "is the string as received, for the other endpoints it is readable text showing the operation and its operands, for example "
    "\"7 + 2\". DELETE /api/history clears it. The history must be safe for concurrent requests. The page is served from GET /. "
    "Include Go tests for the operations, the expression evaluator, the error cases and the history.")

ap = argparse.ArgumentParser()
ap.add_argument("--url", default="https://agentflow.builder.cmdhome.net")
ap.add_argument("--host", default="agentflow.builder.cmdhome.net")
ap.add_argument("--pin-file", default=os.path.expanduser("~/.unified_common.pin"))
ap.add_argument("--repo", default=os.path.expanduser("~/af-webcalc"))
ap.add_argument("--slug", default="webcalc")
ap.add_argument("--scope", default="any", choices=("local", "any"),
                help="model scope: any lets hosted backends (the free opencode models) be used")
ap.add_argument("--hosted-order", default="nemotron-3-ultra-free,glm-5.3-flash-free,big-pickle,deepseek-v4.1-flash-free",
                help="with --lineup free or hybrid: hosted models to put first, in this order (the best scorers of Check reviewers); the rest follow")
ap.add_argument("--hosted-exclude", default="flint-40b-free,longcat-2.5-preview-free",
                help="with --lineup free or hybrid: hosted models to leave out (flaky or withdrawn on the last Check models / Check reviewers)")
ap.add_argument("--lineup", default="global", choices=("global", "local", "free", "hybrid"),
                help="local: build this project's candidate lists from the non-hosted backends by tier; free: only the hosted backends that cost nothing (the free opencode models)")
ap.add_argument("--include-light", action="store_true", help="with --lineup local: let tier 3 models work as workers")
ap.add_argument("--exclude-backend", action="append", default=["kimi-waste"],
                help="with --lineup local: backend to leave out (repeatable; default kimi-waste, too slow for a worker)")
ap.add_argument("--backend-order", default="ollama-m5,ollama-win,ollama-m4",
                help="with --lineup local: within a tier, try these backends first, in this order (fastest first)")
ap.add_argument("--dry-run", action="store_true", help="with --lineup local: print the candidate lists and stop")
ap.add_argument("--resume", type=int, metavar="EFFORT",
                help="carry on an effort this script already started (same --slug and --repo): approve the gates it has reached and wait")
ap.add_argument("--skip-preflight", action="store_true", help="do not check the models and the lineup before starting")
ap.add_argument("--title", default="web calculator", help="the effort's title: put the trial in it so efforts differ on screen")
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
    # The proxy in front of the service can blink (503 "no server", a DNS miss): a
    # trial runs for hours, so wait those out for up to five minutes instead of dying.
    for attempt in range(30):
        try:
            with opener.open(req, timeout=60) as r:
                code, raw = r.status, r.read()
        except urllib.error.HTTPError as e:
            code, raw = e.code, e.read()
        except (urllib.error.URLError, TimeoutError, ConnectionError):
            if attempt == 29:
                raise
            time.sleep(10)
            continue
        if code in (502, 503, 504) and attempt < 29:
            time.sleep(10)
            continue
        break
    if code == 401 and _retry and path != "/api/auth/login":  # the session expired during a long wait: log in again
        login()
        return api(method, path, body, want, _retry=False)
    if want is not None and code != want:
        sys.exit(f"{method} {path} -> {code} (want {want}): {raw[:300]!r}")
    try:
        return json.loads(raw) if raw else None
    except ValueError:
        return raw


def stopped_on_us():
    """What the service says has stopped on its own in this effort (GET /api/attention)."""
    try:
        items = api("GET", "/api/attention", None, 200) or []
    except SystemExit:
        return None
    mine = [i for i in items if i.get("project") == args.slug and (not args.resume or i.get("effort") == args.resume)]
    return mine[0] if mine else None


def wait_for(what, fn, timeout, every=5):
    end = time.time() + timeout
    n = 0
    while time.time() < end:
        v = fn()
        if v:
            return v
        n += 1
        if n % 6 == 0:  # about every half minute: stop at once when the service says it needs a person
            item = stopped_on_us()
            if item:
                sys.exit(f"stopped while waiting for {what}: {item['message']}")
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

if args.lineup in ("free", "hybrid"):
    free = [(b["name"], m["model"]) for b in api("GET", "/api/backends", None, 200)
            if b.get("hosted") and not b.get("paid") and not b.get("retired") for m in b["models"] if not m.get("paid")]
    if not free:
        sys.exit("--lineup free: no hosted unpaid backend with a model")
    free = [x for x in free if x[1] not in args.hosted_exclude.split(",")]
    horder = [x for x in args.hosted_order.split(",") if x]
    free.sort(key=lambda x: horder.index(x[1]) if x[1] in horder else len(horder))
    # each role starts at a different model, so the reviewer is not the worker's own model
    # (the reviewer starts at the best model: it is the role a weak model hurts most)
    lineup = {role: [{"backend": be, "model": m} for be, m in free[i % len(free):] + free[:i % len(free)]]
              for role, i in zip(("planner", "architect", "critic", "compiler", "worker", "reviewer"), (0, 1, 2, 3, 4, 0))}
    free_lineup = lineup
    if args.lineup == "free":
        for role, lst in lineup.items():
            say(f"{role}: " + ", ".join(f"{c['backend']}/{c['model']}" for c in lst))
        if args.dry_run:
            sys.exit(0)

if args.lineup in ("local", "hybrid"):
    pool = []  # (tier, backend, model); tier 0 (not rated) counts as 2
    for b in api("GET", "/api/backends", None, 200):
        if b.get("hosted") or b.get("paid") or b["name"] in args.exclude_backend:
            continue
        pool += [(m.get("tier") or 2, b["name"], m["model"]) for m in b["models"] if not m.get("too_big")]  # too big for its host: Extend windows marked it
    order = [x for x in args.backend_order.split(",") if x]
    rank = lambda be: order.index(be) if be in order else len(order)
    pick = lambda tiers: [{"backend": be, "model": m} for t in tiers
                          for (tt, be, m) in sorted(pool, key=lambda x: (rank(x[1]), x[1], x[2])) if tt == t]
    workers = (2, 3, 1) if args.include_light else (2, 1)
    lineup = {"planner": pick((1, 2)), "architect": pick((1, 2)), "critic": pick((1, 2)), "compiler": pick((1, 2)),
              "worker": pick(workers), "reviewer": pick((1, 2))}
    if args.lineup == "hybrid":  # the free hosted models plan, compile and review; the local models do the work
        lineup = {**free_lineup, "worker": lineup["worker"]}
    for role, lst in lineup.items():
        if not lst:
            sys.exit(f"--lineup {args.lineup}: no model fits the {role} role")
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
if args.lineup in ("local", "free", "hybrid") and not args.resume:
    for role, lst in lineup.items():
        api("PUT", f"/api/projects/{slug}/candidates/{role}", {"candidates": lst}, 200)

if not args.resume and not args.skip_preflight:
    # Before anything starts: try every hosted model with a one-word prompt, then ask the service
    # whether every role has a model and every worker a reviewer. A run that cannot get through
    # should not begin.
    lu = globals().get("lineup") or {}  # the global lineup sets none: the service's own lists apply
    used = sorted({c["backend"] for lst in lu.values() for c in lst})
    hosted = {b["name"] for b in (api("GET", "/api/backends", None, 200) or []) if b.get("hosted")}
    for name in [n for n in used if n in hosted]:
        api("POST", f"/api/backends/{name}/check", None, 202)
    for name in [n for n in used if n in hosted]:
        while api("GET", f"/api/backends/{name}/check", None, 200)["state"] == "running":
            time.sleep(5)
        for c in api("GET", f"/api/backends/{name}/check", None, 200)["results"]:
            say(f"check {name}/{c['Model']}: " + ("answers" if c["OK"] else f"FAILED ({c['Class']})"))
    dead = {(name, c["Model"]) for name in used if name in hosted
            for c in api("GET", f"/api/backends/{name}/check", None, 200)["results"] if not c["OK"]}
    if dead and lu:  # a model that failed its check is taken off this project's lists, not left to be skipped one run at a time
        for role, lst in lu.items():
            keep = [c for c in lst if (c["backend"], c["model"]) not in dead]
            if keep and len(keep) != len(lst):
                api("PUT", f"/api/projects/{slug}/candidates/{role}", {"candidates": keep}, 200)
                lu[role] = keep
    pre = api("GET", f"/api/projects/{slug}/preflight", None, 200)
    for w in pre["warnings"]:
        say("preflight warning: " + w)
    if not pre["ok"]:
        sys.exit("preflight failed, nothing was started:\n  " + "\n  ".join(pre["problems"]))
    say("preflight ok")

eid = args.resume or api("POST", f"/api/projects/{slug}/efforts", {"title": args.title}, 201)["id"]
base = f"/api/projects/{slug}/efforts/{eid}"
say(f"project {slug}, effort {eid}: open the agentflow UI to watch")
status = lambda: api("GET", base, None, 200)["status"]

if not args.resume:
    api("POST", base + "/frd/draft", {"description": SENTENCE, "polish": False}, 202)
    wait_for("draft", lambda: (api("GET", base + "/frd/draft") or {}).get("state") in ("idle", "failed"), LONG)
    d = api("GET", base + "/frd/draft") or {}
    if d.get("state") == "failed":  # do not approve the empty FRD behind a failed draft
        sys.exit(f"the FRD draft failed: {d.get('error')}")
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
    last = told = None
    while True:
        s = status()
        ts = api("GET", base + "/tasks") or []
        line = s + " " + " ".join(f"{t['key']}={t.get('status')}" for t in ts if isinstance(t, dict))
        if line != last:
            say(line)
            last = line
        if s in ("complete", "abandoned") or any(isinstance(t, dict) and t.get("halted") for t in ts):
            break
        item = stopped_on_us()  # a task can wait on the service's attention list without halting: say so, once
        if item and item["message"] != told:
            told = item["message"]
            say("needs attention: " + told)
        time.sleep(10)
say("when complete: cd " + args.repo + " && git checkout agentflow/effort-" + str(eid) + " && go run .   (then open http://localhost:7070)")
