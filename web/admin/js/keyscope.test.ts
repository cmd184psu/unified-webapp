// Run with `npm run test:web`.

import { scopeFromList, scopeSummary, scopeToList, scopeValid, setAll, setModule } from "./keyscope";

function check(name: string, cond: boolean, detail: string): void {
  if (!cond) throw new Error(`${name}: ${detail}`);
  console.log(`ok - ${name}`);
}
const j = (v: unknown): string => JSON.stringify(v);

check("a legacy key with no scope reads as all", scopeFromList(undefined).all && scopeFromList([]).all, "legacy");
check("* reads as all", scopeFromList(["*"]).all, "star");
check("a list reads as those modules, sorted", j(scopeFromList(["todo", "grocery"])) === j({ all: false, modules: ["grocery", "todo"] }), j(scopeFromList(["todo", "grocery"])));
check("all is stored as *", j(scopeToList({ all: true, modules: ["todo"] })) === j(["*"]), "toList all");
check("modules are stored sorted", j(scopeToList({ all: false, modules: ["todo", "grocery"] })) === j(["grocery", "todo"]), "toList");
check("nothing chosen is not valid", !scopeValid({ all: false, modules: [] }), "invalid");
check("all is valid", scopeValid({ all: true, modules: [] }), "valid all");
check("one module is valid", scopeValid({ all: false, modules: ["todo"] }), "valid one");
check("setModule adds and removes", j(setModule(setModule({ all: false, modules: [] }, "todo", true), "todo", false).modules) === j([]), "toggle");
check("setModule does not duplicate", setModule({ all: false, modules: ["todo"] }, "todo", true).modules.length === 1, "dup");
check("setAll keeps the module choice for when it is switched back", setAll({ all: false, modules: ["todo"] }, true).modules[0] === "todo", "keeps");
check("summary", scopeSummary(["*"]) === "All modules" && scopeSummary(["todo", "grocery"]) === "grocery, todo" && scopeSummary(undefined) === "All modules", "summary");
