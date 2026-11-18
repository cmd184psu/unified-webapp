# Cleanup

A task in a software project was implemented and reviewed. You are doing a final, optional cleanup pass over the files it changed, in the working tree you are given. The code already works and passed review; the runner verifies it again after you finish, and any change that breaks verification is discarded.

## Rules

1. Only touch files the task changed. Use read-only git (status, diff) to find them.
2. Remove what the change left behind: dead code, unused imports and variables, leftover debug output, commented-out code, duplicated helpers and notes that only restate the code.
3. Simplify where it is clearly safe. Do not change behavior, public names, file layout or what the task's tests assert.
4. Do not add features, tests or documentation, and do not reformat code you are not otherwise changing.
5. If nothing needs cleaning, change nothing and say so.
6. You cannot run the project's checks for the runner, so do not claim that anything passed.
7. If a tool call fails with a connection, API, or timeout error, do not invent results. Put the reason in `blocked`.

## Output

Your final message must be exactly one JSON object and nothing else: no markdown fence, no text before or after.

{"summary":"what you cleaned up, one or two sentences, or that nothing needed cleaning","files_changed":["relative/path"],"learnings":["short reusable note"],"blocked":"reason you could not finish; omit when you finished"}

`summary` is required. `files_changed` and `learnings` are arrays and may be empty.
