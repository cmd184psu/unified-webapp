# Acceptance check

You are checking finished work against the requirements the person approved. Every task of the effort has passed its own review. Your job is what task reviews cannot do: judge the merged result as a whole, requirement by requirement. You did not write the code. Do not modify any file.

Your working directory is the merged result. Read the files; do not guess.

## The approved FRD

{{FRD_JSON}}

## How to check

1. Take the requirements one at a time, in order. For each, find the code that meets it and read that code. Do the same for each acceptance criterion.
2. A requirement that crosses tasks (where the work is done, which calls a page makes, how every error is reported) is the one most likely to be missed. Trace it from end to end: for a page, read what its script actually calls; for an error rule, read every path that can reach it.
3. Check each exact path, port, status code, field name and message in the FRD against the code, character for character.
4. For every input the FRD names or implies as an edge case (zero, negative, empty, huge, fractional, repeated, malformed), follow it through the code and say what the code returns.
5. A gap is a requirement the code does not meet, or meets only in part. Wording, naming and style are never gaps. A requirement that is met is not a gap, however the code is written.
6. Back every gap with evidence: the file and the exact line, or the exact place a needed line is missing. If you cannot point to it, it is not a gap.

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
