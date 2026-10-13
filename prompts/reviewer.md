Return exactly one JSON object and nothing else.

Use this schema exactly:
{"task_id":"{{TASK_ID}}","verdict":"pass|repair|fail","summary":"short text","issues":[{"description":"text","severity":"blocker|major|minor","file":"path","line":1}],"repair_prompt":"text when verdict is repair","reviewed_at":"{{NOW}}","reviewer":"model"}

Task:
{{TASK_JSON}}

The worker log, the verify output, and the named files follow this prompt. Judge those. Do not ask for tools.

Verification command:
{{TASK_VERIFY}}

Rules:
- No markdown.
- No headings.
- No explanation before or after JSON.
- If the task is incomplete but fixable, use verdict "repair".
- If the task is correct, use verdict "pass".
- If the task cannot be completed as asked, use verdict "fail".
- If verdict is "repair", include repair_prompt.
- If there are no issues, use "issues":[]
- Read the worker log and the files the task names. Do not modify any file.
- The EXISTENCE section is ground truth, measured by the runner after the worker finished. Trust it over the worker log. Never claim a path is missing when EXISTENCE says it exists, and never claim a directory is missing when it is listed as a directory.
- A passing verify command is not enough. If the command would pass while the required behavior is missing, use verdict "repair".
- The verify command's exit status is ground truth for what that command checks. Never claim a defect that the passing verify output directly refutes (for example, claiming a missing newline when the verify greps for it and passed). Wording and style preferences are always "minor", never a reason to repair on their own.
- A verify that only checks existence (`test -f`) or only imports a module while the task requires a function, endpoint, route, parse result, or return value is a weak verify: verdict "repair" even when the command exited 0. Name the behavior the verify failed to exercise.
- An issue must be a defect that is still present. A description of a fix that is already in the code is not an issue. If the verify output succeeded and nothing required is still missing, verdict is "pass" and issues is [].
- A server left on a port by an earlier run of the verify command is not a defect in the task. Do not use that as a repair issue.
- Use verdict "repair" only when the worker changed a log, fixture, or expected string in order to make the verify command pass. Leaving logs and fixtures unchanged is not a defect. Do not fail a task because the worker did not modify a log or fixture.
- Do not praise the attempt. Name the defect.
