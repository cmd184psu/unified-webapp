#!/usr/bin/env python3
"""Operator-run, API-only end-to-end check of agentflow against a REAL model.

It starts its own server (fresh data directory, no auth, its own port), then
drives only the HTTP API, in the order the UI does: add the backend (probing
the host for its models), role candidates, a project on a fresh git repo, an
effort, a drafted FRD from one sentence, plan, automatic compile, queue
approval, run to completion, then checks the integration branch (build, vet,
tests, the command's output), and finally unregisters the project and checks
nothing is left. No browser. Not part of `make` or `go test`.

  AGENTFLOW_LIVE_E2E=1 AGENTFLOW_E2E_HOST=http://host:11434 \
    AGENTFLOW_E2E_MODEL=qwen3:32b [AGENTFLOW_E2E_REVIEW_MODEL=...] \
    scripts/agentflow-e2e-live.py

Environment
  AGENTFLOW_LIVE_E2E=1           required
  AGENTFLOW_E2E_HOST             Ollama base URL (required)
  AGENTFLOW_E2E_MODEL            model for planner/architect/critic/compiler/worker (required)
  AGENTFLOW_E2E_REVIEW_MODEL     reviewer model; must differ from the worker's (default: first
                                 other model the host reports)
  AGENTFLOW_E2E_KEEP=1           keep the scratch directory afterwards
"""
import json
import os
import shutil
import subprocess
import sys
import tempfile
import time
import urllib.error
import urllib.request

if os.environ.get("AGENTFLOW_LIVE_E2E") != "1":
    print("agentflow-e2e-live: AGENTFLOW_LIVE_E2E is not 1; nothing to do (operator-run only).")
    sys.exit(0)

HOST = os.environ.get("AGENTFLOW_E2E_HOST", "")
MODEL = os.environ.get("AGENTFLOW_E2E_MODEL", "")
if not HOST or not MODEL:
    print("agentflow-e2e-live: AGENTFLOW_E2E_HOST and AGENTFLOW_E2E_MODEL are required", file=sys.stderr)
    sys.exit(2)

ROOT = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
SCRATCH = tempfile.mkdtemp(prefix="af-e2e-", dir=os.path.expanduser("~"))
DATA = os.path.join(SCRATCH, "data")
REPO = os.path.join(SCRATCH, "repo")
PORT = 0
T0 = time.time()
FAILS = []
server = None


def say(msg):
    print(f"[{time.time() - T0:6.0f}s] {msg}", flush=True)


def check(cond, what):
    if cond:
        say(f"ok   {what}")
    else:
        say(f"FAIL {what}")
        FAILS.append(what)
    return cond


def api(method, path, body=None, want=None):
    data = None if body is None else json.dumps(body).encode()
    req = urllib.request.Request(f"http://127.0.0.1:{PORT}{path}", data=data, method=method)
    req.add_header("Content-Type", "application/json")
    try:
        with urllib.request.urlopen(req, timeout=60) as resp:
            code, raw = resp.status, resp.read()
    except urllib.error.HTTPError as e:
        code, raw = e.code, e.read()
    if want is not None and code != want:
        say(f"FAIL {method} {path} -> {code}, want {want}: {raw[:300]!r}")
        FAILS.append(f"{method} {path} -> {code}")
        raise SystemExit(finish())
    try:
        return code, json.loads(raw) if raw else None
    except ValueError:
        return code, raw


def wait_for(what, fn, timeout, every=5):
    end = time.time() + timeout
    last = None
    while time.time() < end:
        last = fn()
        if last:
            return last
        time.sleep(every)
    say(f"FAIL timeout waiting for {what}")
    FAILS.append(f"timeout: {what}")
    raise SystemExit(finish())


def git(*args, cwd=REPO):
    return subprocess.run(["git", "-c", "user.name=e2e", "-c", "user.email=e2e@localhost", *args],
                          cwd=cwd, capture_output=True, text=True, check=True).stdout.strip()


def finish():
    if server:
        server.terminate()
        try:
            server.wait(10)
        except subprocess.TimeoutExpired:
            server.kill()
    if FAILS:
        say(f"{len(FAILS)} FAILURE(S): " + "; ".join(FAILS))
    else:
        say("ALL CHECKS PASSED")
    if os.environ.get("AGENTFLOW_E2E_KEEP") == "1":
        say(f"kept {SCRATCH}")
    else:
        shutil.rmtree(SCRATCH, ignore_errors=True)
    return 1 if FAILS else 0


def main():
    global PORT, server
    # --- a fresh repository (like af-test), outside the server's directories
    os.makedirs(REPO)
    git("init", "-q", "-b", "main")
    open(os.path.join(REPO, "go.mod"), "w").write("module e2e\n\ngo 1.22\n")
    open(os.path.join(REPO, "calc.go"), "w").write("package e2e\n\n// Add returns a+b.\nfunc Add(a, b int) int { return a + b }\n")
    open(os.path.join(REPO, "calc_test.go"), "w").write(
        'package e2e\n\nimport "testing"\n\nfunc TestAdd(t *testing.T) {\n\tif Add(2, 3) != 5 {\n\t\tt.Fatal("bad")\n\t}\n}\n')
    git("add", "-A")
    git("commit", "-q", "-m", "initial")

    # --- the server: built from this checkout, own data dir, own port, no auth
    os.makedirs(os.path.join(SCRATCH, "bin"))
    binary = os.path.join(SCRATCH, "bin", "server")
    subprocess.run(["go", "build", "-o", binary, "./cmd/server"], cwd=ROOT, check=True)
    import socket
    s = socket.socket()
    s.bind(("127.0.0.1", 0))
    PORT = s.getsockname()[1]
    s.close()
    cfg = {"port": PORT, "host_routing": {"127.0.0.1": "agentflow", "localhost": "agentflow"},
           "server": {"origin_check": "off", "sse_max_subscribers": 8, "shared_static_dir": os.path.join(ROOT, "web/shared")},
           "taskmaster": {"static_dir": os.path.join(ROOT, "web/taskmaster"), "db_path": os.path.join(SCRATCH, "tm.db")},
           "agentflow": {"static_dir": os.path.join(ROOT, "web/agentflow"), "data_dir": DATA}}
    json.dump(cfg, open(os.path.join(SCRATCH, "config.json"), "w"))
    server = subprocess.Popen([binary, "-config", os.path.join(SCRATCH, "config.json")], cwd=ROOT,
                              stdout=open(os.path.join(SCRATCH, "server.log"), "w"), stderr=subprocess.STDOUT)
    def up():
        try:
            return api("GET", "/api/health")[0] == 200
        except (urllib.error.URLError, ConnectionError, OSError):
            return False
    wait_for("server", up, 60, 1)
    code, health = api("GET", "/api/health")
    check(health.get("opencode", {}).get("status") == "ok", f"health: opencode {health.get('opencode')}")

    # --- backend, found by probing the host (no typed model names)
    code, probe = api("POST", "/api/backends/probe", {"host": HOST, "api_type": "ollama"}, 200)
    models = probe["models"]
    check(MODEL in models, f"probe lists {MODEL} among {len(models)} models")
    review = os.environ.get("AGENTFLOW_E2E_REVIEW_MODEL") or next((m for m in models if m != MODEL), "")
    check(review and review != MODEL, f"reviewer model {review} differs from worker model")
    ctx = probe.get("contexts", {})
    api("POST", "/api/backends", {"name": "live", "host": HOST, "api_type": "ollama", "slots": 1, "hosted": False,
        "paid": False, "models": [{"model": m, "paid": False, "max_concurrent": 0, "context_tokens": ctx.get(m, 0)}
                                  for m in sorted({MODEL, review})]}, 201)
    for role, m in {"planner": MODEL, "architect": review, "critic": MODEL, "compiler": MODEL,
                    "worker": MODEL, "reviewer": review}.items():
        api("PUT", f"/api/candidates/{role}", {"candidates": [{"backend": "live", "model": m}]}, 200)
    code, defn = api("GET", "/api/settings")
    check(code == 200, "settings readable")

    # --- project, effort, FRD drafted from one sentence
    api("POST", "/api/projects", {"slug": "e2e", "repo_path": REPO, "model_scope": "local"}, 201)
    code, dod = api("GET", "/api/projects/e2e/definition-of-done", None, 200)
    check(len(dod.get("items", [])) >= 3, f"definition of done has {len(dod.get('items', []))} items ({dod.get('source')})")
    code, ef = api("POST", "/api/projects/e2e/efforts", {"title": "calc ops"}, 201)
    eid = ef["id"]
    base = f"/api/projects/e2e/efforts/{eid}"
    sentence = ("Add Subtract and Multiply functions to calc.go with tests, and a command in cmd/calc/main.go "
                "that takes two integers as arguments and prints their sum, difference and product.")
    api("POST", base + "/frd/draft", {"description": sentence, "polish": False}, 202)
    st = wait_for("draft", lambda: (lambda c, d: d if d and d.get("state") in ("idle", "failed") else None)(*api("GET", base + "/frd/draft")), 900)
    check(st["state"] == "idle", f"draft finished ({st.get('error', '')})")
    code, frd = api("GET", base + "/frd", None, 200)
    doc = frd.get("json") or frd
    reqs, acs = doc.get("requirements", []), doc.get("acceptance_criteria", [])
    check(len(reqs) >= 2 and all(r.get("id") for r in reqs), f"FRD drafted with {len(reqs)} requirements, ids filled")
    check(len(acs) >= 1 and all(a.get("id") for a in acs), f"FRD has {len(acs)} criteria, ids filled")
    check(not any("tests pass" in (a.get("criterion", "").lower()) for a in acs), "no boilerplate 'tests pass' criterion")
    say("FRD: " + json.dumps({"objective": doc.get("objective"), "requirements": [r["description"] for r in reqs],
                              "criteria": [a["criterion"] for a in acs]})[:900])
    code, size = api("GET", base + "/size", None, 200)
    say(f"size: frd {size.get('frd_tokens')} tokens, budget {size.get('budget_tokens')}, over={size.get('over')}")
    api("POST", base + "/frd/approve", None, 200)

    # --- plan -> automatic compile -> queue
    def status():
        return api("GET", base, None, 200)[1]["status"]
    api("POST", base + "/plan/generate", None, 202)
    wait_for("plan_ready", lambda: status() == "plan_ready", 1500)
    code, rounds = api("GET", base + "/plan/rounds", None, 200)
    say(f"plan ready after {len(rounds.get('rounds') or [])} round(s)")
    api("POST", base + "/plan/approve", None, 200)
    wait_for("queue_draft (automatic compile)", lambda: status() == "queue_draft", 1500)
    code, queue = api("GET", base + "/queue", None, 200)
    tasks = queue["tasks"]
    say("queue: " + "; ".join(f"{t['key']} {t['title']}" for t in tasks))
    check(len(tasks) >= 2, f"{len(tasks)} tasks compiled")
    check(not any(t["title"].lower().startswith(("verify", "run ", "check ")) for t in tasks), "no verification-only task")
    api("POST", base + "/queue/approve", {"hash": queue["hash"]}, 200)
    code, proj = api("GET", "/api/projects/e2e", None, 200)
    check(proj.get("run_state") == "stopped", "project stays stopped until started")

    # --- run to completion
    api("POST", "/api/projects/e2e/run/start", None, 200)

    def done():
        s = status()
        if s in ("complete", "abandoned"):
            return s
        code, ts = api("GET", base + "/tasks")
        halted = [t for t in (ts if isinstance(ts, list) else []) if t.get("halted")]
        if halted:
            return "halted:" + ",".join(t["key"] for t in halted)
        return None
    outcome = wait_for("effort to finish", done, 3600, 10)
    check(outcome == "complete", f"effort finished: {outcome}")
    code, tl = api("GET", base + "/tasks")
    if isinstance(tl, list):
        say("tasks: " + "; ".join(f"{t['key']}={t.get('status')}" for t in tl))
    code, proj = api("GET", "/api/projects/e2e", None, 200)
    check(proj.get("run_state") == "stopped", f"run state after completion: {proj.get('run_state')}")
    code, gc = api("GET", base + "/git-commands", None, 200)
    branch = gc.get("branch", "")
    check(branch.startswith("agentflow/effort-"), f"integration branch {branch}")
    check(len(gc.get("commits") or []) >= 2 and gc.get("diffstat"), f"{len(gc.get('commits') or [])} commits, diffstat present")

    # --- the delivered code, checked independently of the engine
    check(git("rev-parse", "main") == git("rev-parse", "main"), "main untouched by the engine")
    check("agentflow" not in git("log", "-1", "--format=%an", "main"), "main has no engine commit")
    wt = os.path.join(SCRATCH, "check")
    git("worktree", "add", "-q", "--detach", wt, branch)
    try:
        for step in (["go", "build", "./..."], ["go", "vet", "./..."], ["go", "test", "./..."]):
            r = subprocess.run(step, cwd=wt, capture_output=True, text=True)
            check(r.returncode == 0, f"{' '.join(step)} on the branch" + ("" if r.returncode == 0 else f": {r.stdout[-300:]}{r.stderr[-300:]}"))
        src = open(os.path.join(wt, "calc.go")).read()
        check("func Subtract(" in src and "func Multiply(" in src, "Subtract and Multiply exist")
        r = subprocess.run(["go", "run", "./cmd/calc", "7", "3"], cwd=wt, capture_output=True, text=True)
        out = r.stdout
        check(r.returncode == 0 and "10" in out and "4" in out and "21" in out, f"go run ./cmd/calc 7 3 prints sum, difference and product: {out.strip()!r}")
        check(not os.path.exists(os.path.join(wt, ".omc")), "no tool state committed")
    finally:
        git("worktree", "remove", "--force", wt)

    # --- unregister, and check nothing is left behind
    code, un = api("DELETE", "/api/projects/e2e", None, 200)
    check(un.get("deleted") == "e2e", f"unregistered; leftover branches listed: {un.get('branches')}")
    check(api("GET", "/api/projects/e2e")[0] == 404, "project gone")
    left = [d for d in ("worktrees", "stages", "baselines") if os.path.isdir(os.path.join(DATA, d)) and os.listdir(os.path.join(DATA, d))]
    check(not left, f"working files removed ({left or 'none left'})")
    check(git("rev-parse", "main") != "", "repository still intact")
    return 0


if __name__ == "__main__":
    try:
        main()
    except SystemExit:
        raise
    except Exception as e:  # report, then clean up
        say(f"FAIL unexpected error: {e!r}")
        FAILS.append(repr(e))
    sys.exit(finish())
