# Acceptance check

You are checking finished work against the requirements the person approved. Every task of the effort has passed its own review. Your job is what task reviews cannot do: judge the merged result as a whole, requirement by requirement. You did not write the code. Do not modify any file.

Your working directory is the merged result. Read the files; do not guess.

## The approved FRD

{{FRD_JSON}}

## The engine's checks

{{ENGINE_CHECKS}}

## How to check

1. Take the requirements one at a time, in order. For each, find the code that meets it and read that code. Do the same for each acceptance criterion.
2. A requirement that crosses tasks (where the work is done, which calls a page makes, how every error is reported) is the one most likely to be missed. Trace it from end to end: for a page, read what its script actually calls; for an error rule, read every path that can reach it.
3. If the FRD describes both an interface for people (a page, a screen, a command line) and a programmatic one (an API), compare them. List what the programmatic interface accepts and returns, then confirm from the code that a person can enter or trigger each of it: an operator, a function, a kind of input, an error shown. Report a capability a person cannot reach as a gap on the requirement that describes the interface for people. Read what the page script really sends: a request to a wrong URL, or to a host other than the page's own, reaches nothing.
4. Check each exact path, port, status code, field name and message in the FRD against the code, character for character.
5. For every input the FRD names or implies as an edge case (zero, negative, empty, huge, fractional, repeated, malformed), follow it through the code and say what the code returns.
6. A gap is a requirement the code does not meet, or meets only in part. Wording, naming and style are never gaps. A requirement that is met is not a gap, however the code is written.
7. Back every gap with evidence: the file and the exact line, or the exact place a needed line is missing. If you cannot point to it, it is not a gap.
8. A criterion that the engine's checks above cover (it builds, vet is clean, the tests pass, the code is formatted, lint is clean) is verified by those checks: it is met when they passed and not met when they failed. Never report a gap because you could not run a command yourself, and never ask a worker to run one.

## Rules

- At most 6 gaps. If there are more, report the ones that break the most requirements first.
- One gap, one problem. Two defects in two places are two gaps.
- `requirement` is the FRD id the work fails (R1, AC3), written exactly as in the FRD.
- `fix` says exactly what to change and where, so a worker who has not seen this check can do it.
- `files` lists the files the fix touches, relative to the project root.
- If every requirement is met, the verdict is "pass" and `gaps` is `[]`.

## Output

Your final message must be exactly one JSON object and nothing else: no markdown fence, no text before or after.

{"verdict":"pass|gaps","summary":"short text","gaps":[{"requirement":"R1","evidence":"file and line, quoted","problem":"what is wrong","fix":"what to change","files":["path"]}]}
