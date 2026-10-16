#!/usr/bin/env python3
"""Give agentflow a clean slate for a full test run, keeping its configuration.

Removes every project (and with it its efforts, tasks, attempts, stages, strikes and statistics), so the
Projects, Board and Stats screens start empty and honest. Kept: backends, models, tiers, tool flags, API keys,
candidate lists and settings. Your repositories and the agentflow/* branches in them are not touched.

Before removing anything it saves (1) a copy of the database and (2) a JSON archive of the per-model
statistics, the strike history and each project's efforts, so the failure history can still be read later.

  scripts/agentflow-reset.py                 # dry run: show what would go
  scripts/agentflow-reset.py --yes           # do it
  scripts/agentflow-reset.py --yes --keep af-test --force

--force stops runs that are active (otherwise it refuses). --keep SLUG leaves a project alone (repeatable).
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

ap = argparse.ArgumentParser()
ap.add_argument("--url", default="http://127.0.0.1:8787")
ap.add_argument("--host", default="agentflow.builder.cmdhome.net")
ap.add_argument("--pin-file", default=os.path.expanduser("~/.unified_common.pin"))
ap.add_argument("--db", default="/opt/unified-webapp/data/agentflow/agentflow.db")
ap.add_argument("--archive-dir", default=os.path.expanduser("~/agentflow-archive"))
ap.add_argument("--keep", action="append", default=[])
ap.add_argument("--force", action="store_true")
ap.add_argument("--yes", action="store_true")
args = ap.parse_args()

opener = urllib.request.build_opener(urllib.request.HTTPCookieProcessor(http.cookiejar.CookieJar()))


def api(method, path, body=None, _retry=True):
    req = urllib.request.Request(args.url + path, method=method, data=None if body is None else json.dumps(body).encode())
    req.add_header("Host", args.host)
    req.add_header("Content-Type", "application/json")
    try:
        with opener.open(req, timeout=60) as r:
            code, raw = r.status, r.read()
    except urllib.error.HTTPError as e:
        code, raw = e.code, e.read()
    if code == 401 and _retry and path != "/api/auth/login":
        api("POST", "/api/auth/login", {"method": "pin", "pin": open(args.pin_file).read().strip()}, _retry=False)
        return api(method, path, body, _retry=False)
    if code >= 400:
        sys.exit(f"{method} {path} -> {code}: {raw[:200]!r}")
    return json.loads(raw) if raw else None


projects = [p for p in api("GET", "/api/projects") if p["slug"] not in args.keep]
active = [p["slug"] for p in projects if p["run_state"] in ("running", "pausing")]
print(f"{len(projects)} project(s) would be removed:")
for p in projects:
    e = p.get("effort") or {}
    print(f"  {p['slug']:18} run={p['run_state']:8} effort={e.get('id')} {e.get('status')} progress={p.get('progress')}")
print("kept: backends, models, tiers, tool flags, API keys, candidate lists, settings; repositories and their agentflow/* branches")
if active and not args.force:
    sys.exit(f"runs are active in {', '.join(active)}: wait for them, or use --force to stop them")
if not args.yes:
    sys.exit("dry run: nothing changed (add --yes to do it)")

stamp = time.strftime("%Y%m%d-%H%M%S")
os.makedirs(args.archive_dir, exist_ok=True)
db_copy = os.path.join(args.archive_dir, f"agentflow-{stamp}.db")
subprocess.run(["sqlite3", args.db, f".backup '{db_copy}'"], check=True)
archive = {
    "taken_at": stamp,
    "projects": api("GET", "/api/projects"),
    "stats_models": api("GET", "/api/stats/models"),
    "strikes": api("GET", "/api/strikes?limit=1000"),
}
arch = os.path.join(args.archive_dir, f"agentflow-{stamp}.json")
json.dump(archive, open(arch, "w"), indent=1)
print(f"saved {db_copy} and {arch}")

for p in projects:
    if p["slug"] in active:
        api("POST", f"/api/projects/{p['slug']}/run/stop")
    r = api("DELETE", f"/api/projects/{p['slug']}")
    print(f"removed {p['slug']}; branches left in its repository: {r.get('branches')}")
print("done. The next trial starts from an empty Projects list and an empty Stats tab.")
