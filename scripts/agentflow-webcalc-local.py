#!/usr/bin/env python3
"""Re-run the webcalc trial with local models only, then score the result.

A thin wrapper: picks a fresh project name and repository, runs agentflow-webcalc-kickoff.py
with --scope local --lineup local --wait (candidate lists built from the backends' tiers),
and when the effort completes runs agentflow-webcalc-probe.py against its branch.

  scripts/agentflow-webcalc-local.py [--name webcalc-local3] [--include-light] [--no-probe]
        [--exclude-backend NAME ...] [--url ...] [--host ...]

Without --name it uses the first unused `webcalc-localN` (N from 2). Needs a free port 7070
for the probe. Anything else after `--` is passed to the kickoff script.
"""
import argparse
import os
import re
import subprocess
import sys

here = os.path.dirname(os.path.abspath(__file__))
ap = argparse.ArgumentParser()
ap.add_argument("--name")
ap.add_argument("--include-light", action="store_true")
ap.add_argument("--exclude-backend", action="append", default=[])
ap.add_argument("--no-probe", action="store_true")
ap.add_argument("--url")
ap.add_argument("--host")
ap.add_argument("rest", nargs="*")
a = ap.parse_args()

name = a.name
if not name:
    n = 2
    while os.path.exists(os.path.expanduser(f"~/af-webcalc-local{n}")):
        n += 1
    name = f"webcalc-local{n}"
repo = os.path.expanduser(f"~/af-{name}")
cmd = [sys.executable, os.path.join(here, "agentflow-webcalc-kickoff.py"), "--scope", "local", "--lineup", "local",
       "--slug", name, "--repo", repo, "--wait"]
if a.include_light:
    cmd.append("--include-light")
for b in a.exclude_backend:
    cmd += ["--exclude-backend", b]
for flag, v in (("--url", a.url), ("--host", a.host)):
    if v:
        cmd += [flag, v]
cmd += a.rest

eid = None
p = subprocess.Popen(cmd, stdout=subprocess.PIPE, text=True)
for line in p.stdout:
    print(line, end="", flush=True)
    m = re.search(r"effort (\d+):", line)
    if m:
        eid = m.group(1)
if p.wait() != 0:
    sys.exit(p.returncode)
if a.no_probe or not eid:
    sys.exit(0)
print(f"\nprobing {repo} agentflow/effort-{eid}", flush=True)
sys.exit(subprocess.call([sys.executable, os.path.join(here, "agentflow-webcalc-probe.py"), "--repo", repo,
                          "--branch", f"agentflow/effort-{eid}"]))
