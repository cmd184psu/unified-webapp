import { fetchConfig, fetchCerts, fetchCA, initCA } from "./api";
import type { CAStatus } from "./api";
import { renderCertList } from "./render";
import { filterCerts, sortCerts } from "./listmodel";
import type { SortKey, SortDir } from "./listmodel";
import { openCertDetail } from "./detail";
import { openGenerateForm } from "./generate";
import { openImportWizard } from "./wizard";
import { showToast } from "./toast";
import type { AppConfig, Cert } from "./types";

function el<K extends keyof HTMLElementTagNameMap>(
  tag: K,
  className?: string,
): HTMLElementTagNameMap[K] {
  const node = document.createElement(tag);
  if (className) node.className = className;
  return node;
}

function countLabel(count: number): string {
  return `${count} certificate${count === 1 ? "" : "s"}`;
}

function errorText(err: unknown): string {
  return err instanceof Error ? err.message : String(err);
}

const SORT_LABELS: Record<SortKey, string> = {
  name: "Name",
  created: "Created",
  expiry: "Expiry",
};

function formatCADate(iso: string | undefined): string {
  return iso !== undefined ? iso.slice(0, 10) : "unknown";
}

function addMetaRow(dl: HTMLDListElement, label: string, value: string): void {
  const dt = el("dt", "cert-detail-key");
  dt.textContent = label;
  const dd = el("dd", "cert-detail-value");
  dd.textContent = value;
  dl.append(dt, dd);
}

/** Per-OS trust instructions, carried over from `reference/certmachine/static/index.html:24-27` (FR-3). */
const TRUST_INSTRUCTIONS: Array<[string, string]> = [
  ["macOS", 'Open the downloaded rootCA.crt in Keychain Access, then set it to "Always Trust".'],
  ["Linux", "Copy rootCA.crt to /usr/local/share/ca-certificates/ and run update-ca-certificates."],
  ["Windows", 'Import rootCA.crt into the "Trusted Root Certification Authorities" store.'],
  [
    "iOS",
    "Install the configuration profile for rootCA.crt, then enable full trust under Settings > General > About > Certificate Trust Settings.",
  ],
];

function buildTrustInstructions(): HTMLElement {
  const list = el("dl", "cert-trust-list");
  for (const [os, instr] of TRUST_INSTRUCTIONS) {
    const dt = el("dt", "cert-trust-os");
    dt.textContent = os;
    const dd = el("dd", "cert-trust-instr");
    dd.textContent = instr;
    list.append(dt, dd);
  }
  return list;
}

function handleInitCA(button: HTMLButtonElement, onCAChanged: () => void): void {
  button.disabled = true;
  initCA()
    .then(() => {
      showToast("Certificate authority initialized.", "success");
      onCAChanged();
    })
    .catch((err: unknown) => {
      button.disabled = false;
      showToast(errorText(err), "error");
    });
}

/**
 * CA status panel: root CA metadata + download + per-OS trust instructions
 * when a CA exists (FR-3); the init/import choice when it doesn't. Init CA
 * is demoted to a secondary action whenever `legacyImportAvailable &&
 * certCount === 0` -- the wizard leads in that case, per the plan's binding
 * decision (the server's own half of this is `ErrImportPending`, slice 5).
 */
function renderCAPanel(
  container: HTMLElement,
  ca: CAStatus,
  config: AppConfig,
  onCAChanged: () => void,
  onOpenWizard: () => void,
): void {
  const panel = el("section", "cert-ca-panel");
  const heading = el("h2", "cert-ca-heading");
  heading.textContent = "Certificate authority";
  panel.append(heading);

  if (ca.exists) {
    const meta = el("dl", "cert-detail-meta");
    addMetaRow(meta, "Subject", ca.subject ?? "unknown");
    addMetaRow(meta, "Serial", ca.serial ?? "unknown");
    addMetaRow(meta, "Valid", `${formatCADate(ca.notBefore)} – ${formatCADate(ca.notAfter)}`);
    addMetaRow(meta, "Fingerprint", ca.fingerprint ?? "unknown");
    if (ca.importedFrom !== undefined) addMetaRow(meta, "Imported from", ca.importedFrom);
    panel.append(meta);

    const actions = el("div", "cert-ca-actions");
    const download = el("a", "cert-btn");
    download.href = "/api/ca/root.crt";
    download.textContent = "Download root CA";
    actions.append(download);
    panel.append(actions);

    const trust = el("details", "cert-trust");
    const summary = el("summary");
    summary.textContent = "Trust this CA on your device";
    trust.append(summary, buildTrustInstructions());
    panel.append(trust);
  } else {
    const preferImport = config.legacyImportAvailable && config.certCount === 0;
    const note = el("p", "cert-ca-note");
    note.textContent = preferImport
      ? "No certificate authority yet. Import the existing legacy certificates to bring the current root CA forward, or start fresh."
      : "No certificate authority yet. Initialize one to start issuing certificates.";
    panel.append(note);

    const actions = el("div", "cert-ca-actions");
    if (preferImport) {
      const importBtn = el("button", "cert-btn cert-btn-primary");
      importBtn.type = "button";
      importBtn.textContent = "Import legacy certificates";
      importBtn.addEventListener("click", onOpenWizard);
      const initBtn = el("button", "cert-btn cert-btn-secondary");
      initBtn.type = "button";
      initBtn.textContent = "Initialize a new CA instead";
      initBtn.addEventListener("click", () => handleInitCA(initBtn, onCAChanged));
      actions.append(importBtn, initBtn);
    } else {
      const initBtn = el("button", "cert-btn cert-btn-primary");
      initBtn.type = "button";
      initBtn.textContent = "Initialize root CA";
      initBtn.addEventListener("click", () => handleInitCA(initBtn, onCAChanged));
      actions.append(initBtn);
    }
    panel.append(actions);

    if (config.legacyImportDir !== "" && !config.legacyImportAvailable) {
      const reason = el("p", "cert-ca-note cert-ca-note-warn");
      reason.textContent = `Legacy import unavailable: ${config.legacyImportReason}`;
      panel.append(reason);
    }
  }

  container.append(panel);
}

/**
 * The Tools menu (a native `<details>`, no custom dropdown logic needed):
 * always reachable, so a declined first import is never a dead end. When
 * `legacyImportAvailable` is false but `legacyImportDir` is set, the entry
 * shows `legacyImportReason` as disabled text rather than vanishing.
 */
function buildToolsMenu(config: AppConfig, ca: CAStatus, onOpenWizard: () => void): HTMLDetailsElement {
  const details = el("details", "cert-tools");
  const summary = el("summary", "cert-tools-summary");
  summary.textContent = "Tools";
  details.append(summary);

  const menu = el("div", "cert-tools-menu");

  if (ca.exists) {
    const download = el("a", "cert-tools-item");
    download.href = "/api/ca/root.crt";
    download.textContent = "Download root CA";
    menu.append(download);
  }

  if (config.legacyImportAvailable) {
    const importItem = el("button", "cert-tools-item");
    importItem.type = "button";
    importItem.textContent = "Re-import legacy certificates";
    importItem.addEventListener("click", () => {
      details.open = false;
      onOpenWizard();
    });
    menu.append(importItem);
  } else if (config.legacyImportDir !== "") {
    const reason = el("p", "cert-tools-item cert-tools-disabled");
    reason.textContent = `Legacy import unavailable: ${config.legacyImportReason}`;
    menu.append(reason);
  }

  if (menu.childElementCount === 0) {
    const empty = el("p", "cert-tools-item cert-tools-disabled");
    empty.textContent = "No tools available.";
    menu.append(empty);
  }

  details.append(menu);
  return details;
}

/**
 * Mount the certmachine shell: CA panel, toolbar (search/sort/group/new
 * certificate/tools), and the cert list. Fetches config+certs+CA once per
 * `refresh()` call; search/sort/group changes never re-fetch, they just
 * re-run the pure `listmodel.ts` pipeline over the already-fetched `certs`
 * array and re-render.
 */
export async function mountCertApp(root: HTMLElement): Promise<void> {
  root.textContent = "";
  root.classList.add("cert-app");

  const header = el("header", "cert-header");
  const title = el("h1", "cert-title");
  title.textContent = "CertMachine";
  const subtitle = el("p", "cert-subtitle");
  subtitle.textContent = "Loading certificates…";
  header.append(title, subtitle);

  const main = el("main", "cert-main");
  const caPanelWrap = el("div", "cert-ca-panel-wrap");
  const toolbar = el("div", "cert-toolbar");
  const listWrap = el("div", "cert-list-wrap");
  main.append(caPanelWrap, toolbar, listWrap);

  root.append(header, main);

  let config: AppConfig;
  let certs: Cert[];
  let ca: CAStatus;

  let query = "";
  let sortKey: SortKey = "name";
  let sortDir: SortDir = "asc";
  let groupByDomain = false;

  function renderList(): void {
    const filtered = filterCerts(certs, query);
    const sorted = sortCerts(filtered, sortKey, sortDir);
    renderCertList(listWrap, sorted, config.expiryWarnDays, new Date(), {
      groupByDomain,
      hasAnyCerts: certs.length > 0,
      onOpenDetail: (id) => {
        openCertDetail(id, config, new Date(), {
          onChanged: () => {
            void refresh();
          },
        });
      },
    });
    subtitle.textContent = countLabel(certs.length);
  }

  function renderChrome(): void {
    caPanelWrap.textContent = "";
    renderCAPanel(
      caPanelWrap,
      ca,
      config,
      () => {
        void refresh();
      },
      () => openImportWizard(config.certCount, () => void refresh()),
    );

    toolbar.textContent = "";

    const search = el("input", "cert-search");
    search.type = "search";
    search.placeholder = "Search FQDN or SAN…";
    search.value = query;
    search.setAttribute("aria-label", "Search certificates");
    search.addEventListener("input", () => {
      query = search.value;
      renderList();
    });

    const sortSelect = el("select", "cert-sort");
    sortSelect.setAttribute("aria-label", "Sort by");
    (Object.keys(SORT_LABELS) as SortKey[]).forEach((key) => {
      const opt = el("option");
      opt.value = key;
      opt.textContent = SORT_LABELS[key];
      if (key === sortKey) opt.selected = true;
      sortSelect.append(opt);
    });
    sortSelect.addEventListener("change", () => {
      sortKey = sortSelect.value as SortKey;
      renderList();
    });

    const dirLabel = (): string => (sortDir === "asc" ? "↑ Ascending" : "↓ Descending");
    const dirBtn = el("button", "cert-sort-dir");
    dirBtn.type = "button";
    dirBtn.textContent = dirLabel();
    dirBtn.addEventListener("click", () => {
      sortDir = sortDir === "asc" ? "desc" : "asc";
      dirBtn.textContent = dirLabel();
      renderList();
    });

    const groupToggle = el("label", "cert-group-toggle");
    const groupCheckbox = el("input");
    groupCheckbox.type = "checkbox";
    groupCheckbox.checked = groupByDomain;
    groupCheckbox.addEventListener("change", () => {
      groupByDomain = groupCheckbox.checked;
      renderList();
    });
    groupToggle.append(groupCheckbox, document.createTextNode(" Group by domain"));

    const newBtn = el("button", "cert-btn cert-btn-primary");
    newBtn.type = "button";
    newBtn.textContent = "New certificate";
    newBtn.disabled = !ca.exists;
    newBtn.title = ca.exists ? "" : "Initialize or import a certificate authority first.";
    newBtn.addEventListener("click", () => {
      openGenerateForm(config.defaultValidityDays, () => void refresh());
    });

    const tools = buildToolsMenu(config, ca, () => openImportWizard(config.certCount, () => void refresh()));

    toolbar.append(search, sortSelect, dirBtn, groupToggle, newBtn, tools);
  }

  // Every mutating action (generate, renew, delete, import) fires its own
  // refresh(), so two or more can easily be in flight at once -- delete a row
  // while the previous refresh is still waiting on /api/certs and the older,
  // slower response wins, repainting the deleted row as if it were still
  // there. A monotonic token makes the last call started the only one allowed
  // to paint; stale resolutions are dropped, including stale *failures*, which
  // would otherwise replace a good list with an error banner.
  let refreshGeneration = 0;

  async function refresh(): Promise<void> {
    const generation = ++refreshGeneration;
    let loaded: [AppConfig, Cert[], CAStatus];
    try {
      loaded = await Promise.all([fetchConfig(), fetchCerts(), fetchCA()]);
    } catch (err) {
      if (generation !== refreshGeneration) return;
      subtitle.textContent = "";
      toolbar.textContent = "";
      caPanelWrap.textContent = "";
      listWrap.textContent = "";
      const banner = el("p", "cert-error");
      banner.textContent = `Failed to load certificates: ${errorText(err)}`;
      listWrap.appendChild(banner);
      return;
    }
    if (generation !== refreshGeneration) return;

    [config, certs, ca] = loaded;
    renderChrome();
    renderList();
  }

  await refresh();
}
