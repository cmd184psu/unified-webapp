#!/usr/bin/env node
// hamburger-alignment.mjs — verifies that hamburger trigger position matches
// the HamburgerMenu `side` option across all modules.
// Fails if a module uses side: 'right' but puts the trigger in nav-left (or vice versa).

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
  ["todo", "right"],
  ["grocery", "right"],
  // Explicitly set to 'left' (or default)
  ["sampler", "left"],
  // No hamburger menu
  // ["slideshow"] - uses HamburgerMenu but no trigger in index.html (React)
]);

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

function findHamburgerTriggerInTS(tsPath) {
  if (!fs.existsSync(tsPath)) return null;
  const ts = fs.readFileSync(tsPath, "utf8");
  // Check if the file contains side: 'right' or side: 'left'
  const sideMatch = ts.match(/side:\s*['"](left|right)['"]/);
  if (sideMatch) return sideMatch[1];
  return null; // Default (left) if not specified
}

function findHamburgerTriggerInTSX(tsxPath) {
  if (!fs.existsSync(tsxPath)) return null;
  const tsx = fs.readFileSync(tsxPath, "utf8");
  // Check if the file contains side: 'right' or side: 'left'
  const sideMatch = tsx.match(/side:\s*['"](left|right)['"]/);
  if (sideMatch) return sideMatch[1];
  return null;
}

function checkModule(moduleName) {
  const expectedSide = MODULE_SIDES.get(moduleName);
  if (!expectedSide) return { ok: true, reason: "No hamburger menu or not tracked" };

  // Check static HTML first
  const htmlPath = path.join("web", moduleName, "index.html");
  let actualSide = null;
  let source = "html";

  if (fs.existsSync(htmlPath)) {
    actualSide = findHamburgerTriggerInHTML(htmlPath);
    if (actualSide) {
      return actualSide === expectedSide
        ? { ok: true, actual: actualSide, expected: expectedSide, source }
        : { ok: false, actual: actualSide, expected: expectedSide, source };
    }
  }

  // For React modules, check the TSX files
  const tsxPath = path.join("web", moduleName, "src", "App.tsx");
  if (fs.existsSync(tsxPath)) {
    actualSide = findHamburgerTriggerInTSX(tsxPath);
    if (actualSide) {
      return actualSide === expectedSide
        ? { ok: true, actual: actualSide, expected: expectedSide, source: "tsx" }
        : { ok: false, actual: actualSide, expected: expectedSide, source: "tsx" };
    }
  }

  // Check main.ts for TypeScript modules
  const tsPath = path.join("web", moduleName, "js", "main.ts");
  if (fs.existsSync(tsPath)) {
    actualSide = findHamburgerTriggerInTS(tsPath);
    if (actualSide) {
      return actualSide === expectedSide
        ? { ok: true, actual: actualSide, expected: expectedSide, source: "ts" }
        : { ok: false, actual: actualSide, expected: expectedSide, source: "ts" };
    }
  }

  return { ok: true, reason: "No trigger found in static HTML or source (may be dynamic)" };
}

function main() {
  const failures = [];
  const checked = [];

  for (const [moduleName, expectedSide] of MODULE_SIDES) {
    const result = checkModule(moduleName);
    checked.push(`${moduleName}: expected ${expectedSide}, actual ${result.actual || "unknown"} (${result.source})`);
    if (!result.ok && result.actual !== null) {
      failures.push(`${moduleName}: expected hamburger trigger on ${expectedSide} (per side='${expectedSide}'), but found in ${result.actual} (checked ${result.source})`);
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