// Interaction-scripted screenshot scenes, keyed by module.
//
// Each scene: {
//   name:     output filename (<module>/<name>.png)
//   ls:       optional localStorage entries seeded before page load
//   fullPage: optional, default false — interaction states (drawers, modals)
//             are viewport-anchored, and fullPage scrolling smears fixed
//             overlays, so scenes capture the viewport unless told otherwise
//   run:      async (page) => {} — drive the page into the state to capture;
//             throw to mark the scene failed (run continues, exit code 1)
// }
//
// Scenes run after the module's plain variant shots, in the same
// authenticated context. Keep steps selector-minimal and resilient: prefer
// role/text selectors over deep CSS paths so shots survive markup churn.

// Selectors below are verified against the current trees and documented in
// docs/INVENTORY-hamburger-menus.md (per-module trigger/panel sections).

module.exports = {
  certmachine: [
    {
      // Native <details> Tools dropdown (ui.ts buildToolsMenu).
      name: "tools-menu-open",
      run: async (page) => {
        await page.click("summary.cert-tools-summary");
        await page.waitForSelector("details.cert-tools[open]");
      },
    },
  ],

  obsidianoid: [
    {
      // Theme-panel popover — the module's only hamburger content.
      name: "theme-panel-open",
      ls: { "obsidianoid-theme-0": "dark" },
      run: async (page) => {
        await page.click("#btn-hamburger");
        await page.waitForSelector("#theme-panel:not([hidden])");
      },
    },
  ],

  slideshow: [
    {
      // Settings panel + scrim (hidden-attribute toggle).
      name: "settings-open",
      run: async (page) => {
        await page.click("#btn-hamburger");
        await page.waitForSelector("#settings-panel:not([hidden])");
      },
    },
  ],

  smbedit: [
    {
      // React right-side drawer; always mounted, `.open` class slides it in.
      name: "settings-drawer-open",
      run: async (page) => {
        await page.click(".hamburger-btn");
        await page.waitForSelector(".settings-drawer.open");
      },
    },
  ],

  taskmaster: [
    {
      // The existing full hamburger dropdown (#nav-menu-panel).
      name: "menu-open",
      run: async (page) => {
        await page.click("#nav-menu-btn");
        await page.waitForSelector("#nav-menu-panel:not([hidden])");
      },
    },
    {
      // Hand-brake confirmDialog — the shared promise-based modal that FR-5
      // generalizes. Opens the dialog only; nothing is confirmed.
      name: "modal-confirm",
      run: async (page) => {
        await page.click("#brake-btn");
        await page.waitForSelector(".ui-modal-panel");
      },
    },
  ],

  todo: [
    {
      // FR-4's designated base pattern: slide-in sidebar + backdrop.
      name: "drawer-open",
      ls: { "todo-theme": "light" },
      run: async (page) => {
        await page.click("#menu-toggle");
        await page.waitForSelector("#sidebar.sidebar-open");
      },
    },
  ],

  utuber: [
    {
      // Inline settings dropdown (aria-expanded maintained by the trigger).
      name: "settings-open",
      run: async (page) => {
        await page.click("#settings-btn");
        await page.waitForSelector("#settings-panel:not([hidden])");
      },
    },
  ],

  menuserver: [
    {
      // Subject nav dropdown is CSS :hover-open (responsivenav.css); the
      // pointer stays put after run(), so the hover state survives into the
      // screenshot.
      name: "nav-dropdown-open",
      run: async (page) => {
        await page.hover(".topnav .dropdown .dropbtn");
        await page.waitForSelector(".dropdown-content a", { state: "visible" });
      },
    },
    {
      // In-page panel (seeded local-test "Home Links" page): site rows,
      // username/masked-password rows with Hide/Show + Copy buttons, notes.
      name: "panel-open",
      run: async (page) => {
        await page.hover(".topnav .dropdown .dropbtn");
        await page
          .locator('.dropdown-content a[href^="javascript:showPage"]')
          .first()
          .click();
        await page.waitForSelector(".pageClass", { state: "visible" });
      },
    },
  ],
};
