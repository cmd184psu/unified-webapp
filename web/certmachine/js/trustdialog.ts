// trustdialog.ts — the "Trust this CA…" dialog. One entry point for both
// ways of trusting the root CA:
//   - This server: the server's own trust store (POST /api/ca/trust), when
//     the operator enabled it and the server's OS is supported.
//   - Another machine over SSH (POST /api/ca/trust/remote): the server
//     connects, detects the OS (macOS, Windows, Rocky/RHEL, Ubuntu/Debian;
//     anything else installs nothing) and installs the CA there. Privileged
//     steps use passwordless sudo, or a root login; no sudo password is asked.
import { openModal, showToast } from "@shared";
import { fetchSSHKeys, trustDevice, trustRemote, TrustFailedError } from "./api";
import type { TrustResult } from "./api";
import type { AppConfig } from "./types";

function el<K extends keyof HTMLElementTagNameMap>(tag: K, className?: string): HTMLElementTagNameMap[K] {
  const node = document.createElement(tag);
  if (className) node.className = className;
  return node;
}

const PLATFORM_NAMES: Record<string, string> = {
  darwin: "macOS",
  rhel: "Rocky/RHEL",
  debian: "Ubuntu/Debian",
  windows: "Windows",
};

/** Files in ~/.ssh that are never private keys. */
function isLikelyPrivateKey(name: string): boolean {
  if (name.endsWith(".pub")) return false;
  return !["known_hosts", "known_hosts.old", "config", "authorized_keys", "authorized_keys2"].includes(name);
}

function field(label: string, input: HTMLElement): HTMLLabelElement {
  const wrap = el("label", "cert-field");
  const span = el("span", "cert-field-label");
  span.textContent = label;
  wrap.append(span, input);
  return wrap;
}

function radio(name: string, value: string, text: string, checked: boolean): { label: HTMLLabelElement; input: HTMLInputElement } {
  const label = el("label", "cert-trust-radio");
  const input = el("input");
  input.type = "radio";
  input.name = name;
  input.value = value;
  input.checked = checked;
  label.append(input, document.createTextNode(text));
  return { label, input };
}

export function openTrustDialog(config: AppConfig): void {
  const localOK = config.trustDeviceAvailable;
  const remoteOK = config.trustRemoteAvailable;

  const content = el("div", "cert-trust-dialog");

  // ── Target ──
  const targetRow = el("div", "cert-trust-targets");
  const localRadio = radio("trust-target", "local", `This server${config.trustPlatform ? ` (${PLATFORM_NAMES[config.trustPlatform] ?? config.trustPlatform})` : ""}`, localOK && !remoteOK);
  const remoteRadio = radio("trust-target", "remote", "Another machine over SSH", remoteOK);
  localRadio.input.disabled = !localOK;
  remoteRadio.input.disabled = !remoteOK;
  if (!localOK) localRadio.label.title = "Not enabled on this server (certmachine.trust_device_enabled), or its OS isn't supported.";
  if (!remoteOK) remoteRadio.label.title = `Unavailable: ${config.trustRemoteReason ?? "SSH isn't configured"}`;
  targetRow.append(localRadio.label, remoteRadio.label);
  content.append(targetRow);

  // ── SSH details ──
  const sshBox = el("div", "cert-trust-ssh");
  const host = el("input", "cert-field-input");
  host.placeholder = "hostname or IP";
  host.autocomplete = "off";
  const port = el("input", "cert-field-input cert-trust-port");
  port.type = "number";
  port.min = "1";
  port.max = "65535";
  port.value = "22";
  const user = el("input", "cert-field-input");
  user.placeholder = "username";
  user.autocomplete = "off";

  const hostRow = el("div", "cert-trust-row");
  hostRow.append(field("Hostname", host), field("Port", port));
  const authRow = el("div", "cert-trust-auth");
  const keyRadio = radio("trust-auth", "key", "SSH key", true);
  const pwRadio = radio("trust-auth", "password", "Password", false);
  authRow.append(keyRadio.label, pwRadio.label);

  const keySelect = el("select", "cert-field-input");
  const keyField = field("Key (from the server's ~/.ssh)", keySelect);
  const password = el("input", "cert-field-input");
  password.type = "text";
  password.classList.add("ui-secret");
  password.autocomplete = "off";
  password.setAttribute("data-lpignore", "true");
  password.setAttribute("data-1p-ignore", "");
  password.setAttribute("data-form-type", "other");
  password.spellcheck = false;
  const pwField = field("Password", password);
  pwField.hidden = true;

  const sudoNote = el("p", "cert-ca-note");
  sudoNote.textContent =
    "Installing needs admin rights: log in as root (Administrator on Windows), or as a user with passwordless sudo.";

  sshBox.append(hostRow, field("Username", user), authRow, keyField, pwField, sudoNote);
  content.append(sshBox);

  void fetchSSHKeys().then((keys) => {
    const usable = keys.filter((k) => !k.isDir && isLikelyPrivateKey(k.name));
    keySelect.textContent = "";
    if (usable.length === 0) {
      const opt = el("option");
      opt.value = "";
      opt.textContent = "No keys found";
      keySelect.append(opt);
      return;
    }
    for (const k of usable) {
      const opt = el("option");
      opt.value = k.name;
      opt.textContent = k.name;
      keySelect.append(opt);
    }
  });

  // ── Output + actions ──
  const status = el("p", "cert-trust-status");
  const output = el("pre", "cert-trust-output");
  output.hidden = true;
  const actions = el("div", "cert-trust-actions");
  const cancelBtn = el("button", "cert-btn");
  cancelBtn.type = "button";
  cancelBtn.textContent = "Close";
  const trustBtn = el("button", "cert-btn cert-btn-primary");
  trustBtn.type = "button";
  trustBtn.textContent = "Trust";
  actions.append(cancelBtn, trustBtn);
  content.append(status, output, actions);

  const sync = (): void => {
    sshBox.hidden = !remoteRadio.input.checked;
    const usePw = pwRadio.input.checked;
    keyField.hidden = usePw;
    pwField.hidden = !usePw;
  };
  for (const r of [localRadio, remoteRadio, keyRadio, pwRadio]) r.input.addEventListener("change", sync);
  sync();

  const modal = openModal(content, { title: "Trust this CA" });
  cancelBtn.addEventListener("click", () => modal.close());

  const show = (result: TrustResult | null, err: unknown): void => {
    const text = err instanceof TrustFailedError ? err.output : result?.output;
    output.textContent = text ?? "";
    output.hidden = !text;
  };

  trustBtn.addEventListener("click", async () => {
    let request: Promise<TrustResult>;
    let where: string;
    if (remoteRadio.input.checked) {
      if (!host.value.trim() || !user.value.trim()) {
        status.textContent = "Enter a hostname and a username.";
        return;
      }
      const usePw = pwRadio.input.checked;
      if (usePw ? !password.value : !keySelect.value) {
        status.textContent = usePw ? "Enter the password." : "Choose an SSH key.";
        return;
      }
      where = host.value.trim();
      request = trustRemote({
        host: where,
        port: parseInt(port.value, 10) || 22,
        user: user.value.trim(),
        ...(usePw ? { password: password.value } : { key: keySelect.value }),
      });
      status.textContent = `Connecting to ${where} and detecting its OS…`;
    } else {
      where = "this server";
      request = trustDevice();
      status.textContent = "Installing on this server…";
    }

    trustBtn.disabled = true;
    output.hidden = true;
    try {
      const result = await request;
      const os = result.platform ? ` (${PLATFORM_NAMES[result.platform] ?? result.platform})` : "";
      status.textContent = `Trusted on ${where}${os}.`;
      show(result, null);
      showToast(`CA trusted on ${where}${os}`, "success");
    } catch (err) {
      status.textContent = err instanceof Error ? err.message : String(err);
      show(null, err);
    } finally {
      trustBtn.disabled = false;
    }
  });
}
