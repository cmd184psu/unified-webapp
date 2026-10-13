# Task Compiler Prompt Template

You are a task compiler. Convert an approved implementation plan into a queue of atomic, independently executable tasks for a one-task-at-a-time worker.

## Input

The approved plan:

```json
{{PLAN_JSON}}
```

All paths are relative to the project directory. The worker already runs
inside the project directory. Never emit an absolute path anywhere.

## Output contract

Return exactly one valid JSON object and nothing else.

The JSON must parse with a strict parser.
Do not include markdown fences.
Do not include commentary before or after the JSON.
Do not include trailing commas.

Use this shape:

```json
{
  "frd_id": "{{FRD_ID}}",
  "plan_status": "approved",
  "compiled_date": "{{TODAY}}",
  "tasks": [
    {
      "id": "T001",
      "phase": "P1",
      "title": "Short title",
      "status": "ready",
      "depends_on": [],
      "prompt": "Short plain-English worker instruction naming relative file paths.",
      "working_dir": ".",
      "verify": "One concrete verification command using relative paths.",
      "acceptance": ["R2: project root with .agentflow/ validates as ok"],
      "files_touched": ["relative/path.ext"],
      "attempts": 0,
      "max_attempts": 3
    }
  ]
}
```

## Rules

1. Tasks must be atomic and small enough for one worker run under 5 minutes.
2. Use sequential dependencies within a phase.
3. The first task in a phase depends on the last task of prerequisite phases.
4. Set `status` to `"ready"` only when `depends_on` is empty. Otherwise use `"blocked"`. (The compiler driver normalizes this mechanically, but set it correctly anyway.)
5. Each `prompt` must be self-contained, but brief. Target 2 to 5 short sentences.
6. Each `prompt` must name exact file paths and desired outcome, but must NOT embed full source code, HTML, JSON, XML, shell scripts, or multi-line file contents.
7. Do not include raw HTML tags like `<div>` or `<h1>` inside `prompt`.
8. Do not include unescaped double quotes inside `prompt` or `verify`.
9. Describe desired content in words instead of pasting literal file bodies.
10. Each `verify` must be a single shell command that exits 0 only when the behavior named in `prompt` is present, and exits non-zero when that behavior is missing. Importing a module, printing a type, or checking that a file exists is not enough when the prompt requires parsed values or a computed result. Do not end the command with `echo $?`. The shell's own exit status must be the check's status. Close every quote. Do not run grep on a directory. To check that a file exists, use `test -f path`. To check its contents, grep that file. Use `grep -r` only when the search is really meant to walk a directory. Do not search file contents for the file's own name. If the task creates `app/services/project_service.py`, existence is `test -f app/services/project_service.py`. A content search must look for behavior named in the prompt, such as `.agentflow` or `ValueError`. If the task installs packages into a virtual environment, the verify command must run that environment's Python, not a bare `python`. Do not hide verify output with `/dev/null`.
11. Keep `files_touched` minimal and accurate.
12. Prefer implementation and verification as separate tasks if combining them would make the prompt long or fragile.
13. Never invent infrastructure outside the approved plan. Do not require a verify command to find files under `.agentflow` that the task does not create. The runner owns `frd.json`, `plan.json`, and `queue.json`.
14. Use relative paths everywhere. Set every `working_dir` to `"."`. Never write absolute paths such as `/opt/...` or `/home/...` in `prompt`, `verify`, or `files_touched`. Never paste the project directory into a field. The worker already runs inside the project directory.
15. Keep the three file lists consistent. Every path in `files_touched` must be named in `prompt` with backticks. Every file the prompt tells the worker to create or modify must appear in `files_touched`. A prompt may also name files created by tasks it depends on when the worker only reads them, plus `.agentflow/` and `venv/` paths. A strict checker rejects the queue otherwise, and unmatched `files_touched` entries are dropped.
16. No forward references. A task may only name files created by itself or by tasks in its dependency chain. Never reference a file "to be created in the next task" and never import code that does not exist yet.
17. Modify-in-place only down a dependency chain. A file may appear in `files_touched` of more than one task only when each later task depends on the earlier owner. Never touch a file owned by a task outside your dependencies.
18. Never use `||` in `verify`. Chain checks with `&&` only, so the exit status always reflects the last real check.
19. Never grep a file for its own name. `grep -q 'index.html' templates/index.html` always passes or fails for the wrong reason; grep for behavior the prompt requires instead.
20. Never grep for a quoted token such as `"'/health'"`. Grep for the bare token `/health` so the check does not depend on the worker's string-quoting style.
21. Never pipe `pip show PKG` into `grep -q`. Use `venv/bin/pip show PKG >/dev/null` or `venv/bin/python -c "import PKG"` to test an installed package.
22. When a task runs a test suite, verify with `pytest` (for example `python -m pytest tests/ -q`). A pytest verify that exits 0 passes the task without model review, so make it the strongest check you can.
23. Output valid JSON only.
24. Every task must create or modify at least one file: `files_touched` is never empty. Fold verification into the implementation task; never emit a no-code check task that only starts a server and curls it.
25. Verify commands are hermetic foreground checks only: no `uvicorn`, `gunicorn`, `curl`, `wget`, `fuser`, `sleep`, `nohup`, `kill`, `wait`, background `&`, ports, `localhost`, or `127.0.0.1`. Exercise behavior in-process (`pytest`, `python -c` with asserts, `grep` on files) instead of starting a server.
26. Verify must assert behavior, not mere existence. When the prompt names a function, endpoint, route, parser, or return value, an existence-only `test -f` or an import-only `python -c "from x import y"` is rejected: call the function and assert its result, hit the route through the test client, or run `pytest`.
27. Every task carries `acceptance`: 1-3 concrete checkable criteria, each traced to an FRD requirement or AC id (e.g. "R2: ...", "AC3: ..."). Generic criteria ("implementation is complete", "code works") are rejected. The `verify` command must check every listed criterion with fresh evidence.

## Task writing style

Good prompt example:
"Create `app/state.py` with a `ProjectState` class that loads `.agentflow/frd.json` and `.agentflow/plan.json`. Return structured counts for task status. Do not add database code or network calls."

Bad prompt example:
"Create `base.html` containing `<h1>Hello</h1>` and also add the following Python code: ..."

Produce the full queue now.
