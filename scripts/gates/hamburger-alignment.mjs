#!/usr/bin/env node
// hamburger-alignment.mjs — verifies that hamburger trigger position matches
// the HamburgerMenu `side` option across all modules.
// Fails if a module uses side: 'right' but puts the trigger in nav-left (or vice versa).
// A module whose side cannot be determined is a failure too (GT2), unless it is
// listed in NOT_APPLICABLE with a reason.

import fs from "node:fs";
import path from "node:path";
import { descriptors } from "../descriptors.mjs";

process.chdir(path.resolve(import.meta.dirname, "..", ".."));

// Modules that use HamburgerMenu and their expected side
// Determined by scanning source files for `side: 'right'` or `side: 'left'`
const MODULE_SIDES = new Map([
  // Explicitly set to 'right'
  ["agentflow", "right"],
  ["haproxy", "right"],
  ["smbedit", "right"],
  ["taskmaster", "right"],
  ["timetracker", "right"],
  ["obsidianoid", "right"],
  ["utuber", "right"],
  ["issuetracker", "right"],
  ["multissh", "right"],
  ["certmachine", "right"],
  ["menuserver", "right"],
  ["admin", "right"],
  ["slideshow", "right"],
  ["todo", "left"],
  ["grocery", "right"],
  // Left: the HamburgerMenu is built without `side`, so the drawer takes CSS's
  // left-edge default, and its trigger sits at the left of the page (todo:
  // #menu-toggle in .topbar-left; sampler: first child of the header).
  ["sampler", "left"],
]);

// Modules with no checkable hamburger side, each with the reason. Empty today:
// every tracked module is classified by reading its entry source.
const NOT_APPLICABLE = new Map([]);

function findHamburgerTriggerInHTML(htmlPath) {
  const html = fs.readFileSync(htmlPath, "utf8");
  
  // Find the nav element first, then look for nav-left/nav-right within it
  const navMatch = html.match(/<nav[^>]*id="nav"[^>]*>[\s\S]*?<\/nav>/);
  if (!navMatch) return null;
  
  const navHtml = navMatch[0];
  
  // Find the nav-left and nav-right divs separately, then check if each contains the trigger
  const navLeftMatch = navHtml.match(/<div class="nav-left"[^>]*>([\s\S]*?)<\/div>/);
  const navRightMatch = navHtml.match(/<div class="nav-right"[^>]*>([\s\S]*?)<\/div>/);
  
  const leftHasTrigger = navLeftMatch && navLeftMatch[1].includes('id="hamburger-trigger"');
  const rightHasTrigger = navRightMatch && navRightMatch[1].includes('id="hamburger-trigger"');
  
  if (leftHasTrigger) return "left";
  if (rightHasTrigger) return "right";
  return null; // Not found in static HTML
}

// The side a module's entry sources give its HamburgerMenu: an explicit
// `side: 'left'|'right'`, else "left" when a HamburgerMenu is constructed
// without one (the CSS default), else null. Entries come from the descriptor,
// so main.ts, app.ts, main.tsx and shell.ts are all covered.
function findHamburgerSideInEntries(moduleName) {
  const d = descriptors.find((x) => x.name === moduleName);
  if (!d) return null;
  let constructed = false;
  for (const entry of d.entry) {
    if (!fs.existsSync(entry)) continue;
    const src = fs.readFileSync(entry, "utf8");
    const sideMatch = src.match(/side:\s*['"](left|right)['"]/);
    if (sideMatch) return sideMatch[1];
    if (/new\s+HamburgerMenu\s*\(/.test(src)) constructed = true;
  }
  return constructed ? "left" : null;
}

function checkModule(moduleName) {
  const expectedSide = MODULE_SIDES.get(moduleName);

  // Check static HTML first
  const htmlPath = path.join("web", moduleName, "index.html");
  if (fs.existsSync(htmlPath)) {
    const htmlSide = findHamburgerTriggerInHTML(htmlPath);
    if (htmlSide) {
      return { ok: htmlSide === expectedSide, actual: htmlSide, expected: expectedSide, source: "html" };
    }
  }

  const entrySide = findHamburgerSideInEntries(moduleName);
  if (entrySide) {
    return { ok: entrySide === expectedSide, actual: entrySide, expected: expectedSide, source: "entry" };
  }

  return { ok: false, actual: null, expected: expectedSide, source: "none" };
}

function main() {
  const failures = [];
  const checked = [];

  for (const [moduleName, expectedSide] of MODULE_SIDES) {
    if (NOT_APPLICABLE.has(moduleName)) {
      checked.push(`${moduleName}: not applicable (${NOT_APPLICABLE.get(moduleName)})`);
      continue;
    }
    const result = checkModule(moduleName);
    checked.push(`${moduleName}: expected ${expectedSide}, actual ${result.actual || "unknown"} (${result.source})`);
    if (result.actual === null) {
      failures.push(`${moduleName}: hamburger side could not be determined from its HTML or descriptor entry sources; fix detection or add it to NOT_APPLICABLE with a reason`);
    } else if (!result.ok) {
      failures.push(`${moduleName}: expected hamburger on ${expectedSide}, but found ${result.actual} (checked ${result.source})`);
    }
  }

  if (failures.length > 0) {
    process.stderr.write("hamburger-alignment: FAIL\n");
    for (const f of failures) process.stderr.write(`  ${f}\n`);
    process.stderr.write("\nChecked:\n");
    for (const c of checked) process.stderr.write(`  ${c}\n`);
    process.exit(1);
  }

  process.stdout.write(`hamburger-alignment: PASS — ${MODULE_SIDES.size} module(s) checked\n`);
  for (const c of checked) process.stdout.write(`  ${c}\n`);
}

main();