#!/usr/bin/env node
// Screenshot-baseline harness for the UI unification work (FR/C6 gate).
//
// Usage:  node shoot.js [label]
//   label defaults to "pre-phase1"; shots land in baselines/<label>/<module>/<variant>.png
//
// What it does:
//   1. Derives a harness config from ../../local-test/config.json:
//      port 18080, every already-protected module gets a local PIN file
//      (so login is scriptable without LDAP), auth data dir kept local.
//   2. Starts ../../unified-webapp with that config.
//   3. Launches Chromium with --host-resolver-rules so the Host-header
//      dispatch works against the real hostnames (*.test, *.cmdhome.net)
//      with zero DNS setup.
//   4. Logs in through POST /api/auth/login where needed, seeds theme
//      localStorage keys per variant, captures full-page screenshots.

const fs = require("fs");
const path = require("path");
const { spawn } = require("child_process");
const { chromium } = require("playwright");

const REPO = path.resolve(__dirname, "..", "..");
const PORT = 18080;
const PIN = "142536";
const LABEL = process.argv[2] || "pre-phase1";
const OUT = path.join(__dirname, "baselines", LABEL);
const VIEWPORT = { width: 1440, height: 900 };
const SETTLE_MS = 1500;

// Per-module theme variants. `ls` entries are seeded into localStorage
// before the page loads. Everything else gets a single "default" shot.
const VARIANTS = {
  todo: [
    { name: "light", ls: { "todo-theme": "light" } },
    { name: "dark", ls: { "todo-theme": "dark" } },
  ],
  // "dark" is being renamed to "obsidian" in the UI unification; after the
  // rename, change this to ["obsidian"] and compare against the "dark" shot.
  obsidianoid: ["dark"].map((t) => ({
    name: t,
    ls: { "obsidianoid-theme-0": t },
  })),
};

function deriveConfig() {
  const cfg = JSON.parse(
    fs.readFileSync(path.join(REPO, "local-test", "config.json"), "utf8")
  );
  cfg.port = PORT;

  const pinDir = path.join(__dirname, ".pins");
  fs.rmSync(pinDir, { recursive: true, force: true });
  fs.mkdirSync(pinDir, { recursive: true });

  const protectedModules = [];
  for (const mod of Object.keys(cfg.auth.modules)) {
    const pinFile = path.join(pinDir, `${mod}.pin`);
    fs.writeFileSync(pinFile, PIN + "\n", { mode: 0o400 });
    cfg.auth.modules[mod].pin_file = pinFile;
    protectedModules.push(mod);
  }
  cfg.auth.data_dir = path.join(__dirname, ".data", "auth");
  fs.mkdirSync(cfg.auth.data_dir, { recursive: true });

  const derived = path.join(__dirname, ".derived-config.json");
  fs.writeFileSync(derived, JSON.stringify(cfg, null, 2));
  return { cfg, derived, protectedModules };
}

// One representative host per module (host_routing maps several aliases to
// the same module; prefer the non-localhost spelling).
function hostsByModule(cfg) {
  const seen = new Map();
  for (const [host, mod] of Object.entries(cfg.host_routing)) {
    if (!seen.has(mod) || seen.get(mod) === "localhost") seen.set(mod, host);
  }
  return [...seen.entries()].map(([module, host]) => ({ module, host }));
}

async function waitForServer(proc) {
  for (let i = 0; i < 50; i++) {
    if (proc.exitCode !== null)
      throw new Error(`server exited early with code ${proc.exitCode}`);
    try {
      await fetch(`http://127.0.0.1:${PORT}/`, { redirect: "manual" });
      return;
    } catch {
      await new Promise((r) => setTimeout(r, 200));
    }
  }
  throw new Error("server did not become ready on port " + PORT);
}

async function main() {
  const { cfg, derived, protectedModules } = deriveConfig();
  const targets = hostsByModule(cfg);

  const bin = path.join(REPO, "unified-webapp");
  if (!fs.existsSync(bin)) throw new Error(`${bin} missing — run: make build`);

  const server = spawn(bin, ["-config", derived], {
    cwd: REPO,
    stdio: ["ignore", "pipe", "pipe"],
  });
  let serverLog = "";
  server.stdout.on("data", (d) => (serverLog += d));
  server.stderr.on("data", (d) => (serverLog += d));

  let browser;
  try {
    await waitForServer(server);

    browser = await chromium.launch({
      args: [
        '--host-resolver-rules=MAP *.test 127.0.0.1, MAP *.cmdhome.net 127.0.0.1',
      ],
    });

    let shots = 0;
    for (const { module, host } of targets) {
      const base = `http://${host}:${PORT}`;
      const outDir = path.join(OUT, module);
      fs.mkdirSync(outDir, { recursive: true });

      const context = await browser.newContext({
        viewport: VIEWPORT,
        colorScheme: "light",
      });

      if (protectedModules.includes(module)) {
        // Log in from inside the page: Playwright's context.request runs in
        // Node and bypasses Chromium's --host-resolver-rules, so a POST to a
        // *.cmdhome.net host would hit real DNS. A same-origin fetch from
        // the page goes through the browser stack and lands the uw_session
        // cookie in this context's jar.
        const login = await context.newPage();
        await login.goto(`${base}/`, { waitUntil: "load", timeout: 30000 });
        const status = await login.evaluate(async (pin) => {
          const r = await fetch("/api/auth/login", {
            method: "POST",
            headers: { "Content-Type": "application/json" },
            body: JSON.stringify({ method: "pin", pin }),
          });
          return r.status;
        }, PIN);
        await login.close();
        if (status !== 200)
          throw new Error(`login failed for ${module}: HTTP ${status}`);
      }

      const variants = VARIANTS[module] || [{ name: "default", ls: null }];
      for (const v of variants) {
        const page = await context.newPage();
        if (v.ls) {
          const seed = v.ls;
          await page.addInitScript((entries) => {
            for (const [k, val] of Object.entries(entries))
              localStorage.setItem(k, val);
          }, seed);
        }
        await page.goto(`${base}/`, { waitUntil: "load", timeout: 30000 });
        await page.waitForTimeout(SETTLE_MS);
        const file = path.join(outDir, `${v.name}.png`);
        await page.screenshot({ path: file, fullPage: true });
        console.log(`  ${module}/${v.name}.png`);
        shots++;
        await page.close();
      }
      await context.close();
    }

    // One shot of the shared login gate (any protected host, no session).
    {
      const gateTarget = targets.find((t) => protectedModules.includes(t.module));
      if (gateTarget) {
        const ctx = await browser.newContext({ viewport: VIEWPORT, colorScheme: "light" });
        const page = await ctx.newPage();
        await page.goto(`http://${gateTarget.host}:${PORT}/`, {
          waitUntil: "load",
          timeout: 30000,
        });
        await page.waitForTimeout(500);
        fs.mkdirSync(path.join(OUT, "_login-gate"), { recursive: true });
        await page.screenshot({
          path: path.join(OUT, "_login-gate", "default.png"),
          fullPage: true,
        });
        console.log(`  _login-gate/default.png (via ${gateTarget.host})`);
        shots++;
        await ctx.close();
      }
    }

    console.log(`\n${shots} screenshots -> ${OUT}`);
  } catch (err) {
    console.error("FAILED:", err.message);
    if (serverLog) console.error("--- server log tail ---\n" + serverLog.slice(-2000));
    process.exitCode = 1;
  } finally {
    if (browser) await browser.close();
    server.kill("SIGTERM");
  }
}

main();
