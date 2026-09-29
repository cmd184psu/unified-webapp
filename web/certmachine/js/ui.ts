import { fetchConfig, fetchCerts, fetchCA, initCA } from "./api";
import { openTrustDialog } from "./trustdialog";
import { openReplaceCADialog, confirmSwitchBackCA } from "./cadialog";
import type { CAStatus } from "./api";
import { renderCertList } from "./render";
import { filterCerts, filterStale, sortCerts } from "./listmodel";
import type { SortKey, SortDir } from "./listmodel";
import { openCertDetail } from "./detail";
import { openGenerateForm } from "./generate";
import { openImportWizard } from "./wizard";
import { showToast, promptDialog } from "@shared";
import type { AppConfig, Cert } from "./types";

function el<K extends keyof HTMLElementTagNameMap>(
  tag: K,
  className?: string,
): HTMLElementTagNameMap[K] {
  const node = document.createElement(tag);
  if (className) node.className = className;
  return node;
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

// Initializing asks for the CA's name: it becomes the certificate's Common
// Name (what trust stores show) and names its files, e.g. "Home Lab CA 2026"
// downloads as Home-Lab-CA-2026.crt.
async function handleInitCA(button: HTMLButtonElement, onCAChanged: () => void): Promise<void> {
  const name = await promptDialog("Name for the new certificate authority:", {
    title: "Initialize CA",
    defaultValue: "CertMachine Root CA",
    confirmLabel: "Initialize",
  });
  if (name === null) return;
  button.disabled = true;
  initCA(name.trim())
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
 * CA status panel: root CA metadata + download + (when this server or SSH
 * trust is available) the "Trust this CA…" dialog button, when a CA
 * exists; the init/import choice when it doesn't. Manual per-OS trust
 * instructions for platforms this app cannot automate live in the README /
 * docs/certmachine.md, not here -- keeping them out of the bundle avoids
 * maintaining the same OS list in two places. Init CA is demoted to a
 * secondary action whenever `legacyImportAvailable && certCount === 0` -- the
 * wizard leads in that case, per the plan's binding decision (the server's
 * own half of this is `ErrImportPending`, slice 5).
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

  const openTrust = (): void => openTrustDialog(config);

  if (ca.exists) {
    const meta = el("dl", "cert-detail-meta");
    addMetaRow(meta, "Subject", ca.subject ?? "unknown");
    addMetaRow(meta, "Serial", ca.serial ?? "unknown");
    addMetaRow(meta, "Valid", `${formatCADate(ca.notBefore)} – ${formatCADate(ca.notAfter)}`);
    addMetaRow(meta, "Fingerprint", ca.fingerprint ?? "unknown");
    if (ca.importedFrom !== undefined) addMetaRow(meta, "Imported from", ca.importedFrom);
    panel.append(meta);

    // The previous CA (CA-replacement plan FR-R7): shown only when one
    // exists, with the active-cert count that also drives the Replace
    // dialog's forced previousStale choice (D7).
    if (ca.previous !== undefined) {
      const prevHeading = el("p", "cert-field-label");
      prevHeading.textContent = "Previous certificate authority";
      panel.append(prevHeading);
      const prevMeta = el("dl", "cert-detail-meta");
      addMetaRow(prevMeta, "Subject", ca.previous.subject);
      addMetaRow(prevMeta, "Valid", `${formatCADate(ca.previous.notBefore)} – ${formatCADate(ca.previous.notAfter)}`);
      addMetaRow(
        prevMeta,
        "Signs",
        `${ca.previous.activeCount} active certificate${ca.previous.activeCount === 1 ? "" : "s"}`,
      );
      panel.append(prevMeta);
    }

    const actions = el("div", "cert-ca-actions");
    const download = el("a", "cert-btn");
    download.href = "/api/ca/root.crt";
    download.textContent = "Download root CA";
    actions.append(download);

    // One entry point for trusting the CA, here or on another machine.
    if (config.trustDeviceAvailable || config.trustRemoteAvailable) {
      const trustBtn = el("button", "cert-btn cert-btn-secondary");
      trustBtn.type = "button";
      trustBtn.textContent = "Trust this CA…";
      trustBtn.addEventListener("click", openTrust);
      actions.append(trustBtn);
    }

    const replaceBtn = el("button", "cert-btn cert-btn-secondary");
    replaceBtn.type = "button";
    replaceBtn.textContent = "Replace CA…";
    replaceBtn.addEventListener("click", () => openReplaceCADialog(ca, onCAChanged, openTrust));
    actions.append(replaceBtn);

    if (ca.previous !== undefined) {
      const switchBackBtn = el("button", "cert-btn cert-btn-secondary");
      switchBackBtn.type = "button";
      switchBackBtn.textContent = "Switch back to previous CA";
      switchBackBtn.addEventListener("click", () => {
        void confirmSwitchBackCA(onCAChanged, openTrust);
      });
      actions.append(switchBackBtn);
    }

    panel.append(actions);

    const trustNote = el("p", "cert-ca-note");
    trustNote.textContent =
      "For other devices, or if the button above isn't available: manual per-OS trust instructions are in the README (and docs/certmachine.md).";
    panel.append(trustNote);
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
      initBtn.addEventListener("click", () => void handleInitCA(initBtn, onCAChanged));
      actions.append(importBtn, initBtn);
    } else {
      const initBtn = el("button", "cert-btn cert-btn-primary");
      initBtn.type = "button";
      initBtn.textContent = "Initialize root CA";
      initBtn.addEventListener("click", () => void handleInitCA(initBtn, onCAChanged));
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
 * The Import button: the one action the old Tools menu held that isn't
 * elsewhere (the CA panel already offers "Download root CA"). Disabled, with
 * the reason as its tooltip, when there's nothing to import.
 */
function buildImportButton(config: AppConfig, onOpenWizard: () => void): HTMLButtonElement {
  const btn = el("button", "cert-btn");
  btn.type = "button";
  btn.textContent = "Import";
  if (config.legacyImportAvailable) {
    btn.title = "Import the legacy certificates";
    btn.addEventListener("click", onOpenWizard);
  } else {
    btn.disabled = true;
    btn.title = config.legacyImportDir !== ""
      ? `Nothing to import: ${config.legacyImportReason}`
      : "Nothing to import: no legacy import directory is configured.";
  }
  return btn;
}

/**
 * Mount the certmachine shell: CA panel, toolbar (search/sort/group/new
 * certificate/import), and the cert list. Fetches config+certs+CA once per
 * `refresh()` call; search/sort/group changes never re-fetch, they just
 * re-run the pure `listmodel.ts` pipeline over the already-fetched `certs`
 * array and re-render.
 */
export async function mountCertApp(root: HTMLElement): Promise<void> {
  root.textContent = "";
  root.classList.add("cert-app");

  const main = el("main", "cert-main");
  const caPanelWrap = el("div", "cert-ca-panel-wrap");
  const toolbar = el("div", "cert-toolbar");
  const listWrap = el("div", "cert-list-wrap");
  main.append(caPanelWrap, toolbar, listWrap);

  root.append(main);

  let config: AppConfig;
  let certs: Cert[];
  let ca: CAStatus;

  let query = "";
  let sortKey: SortKey = "name";
  let sortDir: SortDir = "asc";
  let groupByDomain = false;
  let staleOnly = false;

  function renderList(): void {
    const filtered = filterStale(filterCerts(certs, query), staleOnly);
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

    const groupToggle = el("label", "ui-toggle cert-group-toggle");
    const groupCheckbox = el("input");
    groupCheckbox.type = "checkbox";
    groupCheckbox.checked = groupByDomain;
    groupCheckbox.addEventListener("change", () => {
      groupByDomain = groupCheckbox.checked;
      renderList();
    });
    groupToggle.append(groupCheckbox, el("span", "ui-toggle-track"), document.createTextNode("Group by domain"));

    // "Stale only" (CA-replacement plan FR-R4): same toggle pattern as
    // "Group by domain" above -- a pure client-side filter over the
    // already-fetched `certs` array, no re-fetch.
    const staleToggle = el("label", "ui-toggle cert-group-toggle");
    const staleCheckbox = el("input");
    staleCheckbox.type = "checkbox";
    staleCheckbox.checked = staleOnly;
    staleCheckbox.addEventListener("change", () => {
      staleOnly = staleCheckbox.checked;
      renderList();
    });
    staleToggle.append(staleCheckbox, el("span", "ui-toggle-track"), document.createTextNode("Stale only"));

    const newBtn = el("button", "cert-btn cert-btn-primary");
    newBtn.type = "button";
    newBtn.textContent = "New certificate";
    newBtn.disabled = !ca.exists;
    newBtn.title = ca.exists ? "" : "Initialize or import a certificate authority first.";
    newBtn.addEventListener("click", () => {
      openGenerateForm(config.defaultValidityDays, () => void refresh());
    });

    const importBtn = buildImportButton(config, () => openImportWizard(config.certCount, () => void refresh()));

    toolbar.append(search, sortSelect, dirBtn, groupToggle, staleToggle, newBtn, importBtn);
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
