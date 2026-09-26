// web/menuserver/js/main.ts
import { ThemeManager, HamburgerMenu, showToast } from "/shared/dist/shared.mjs";
var themes = new ThemeManager({ module: "menuserver", default: "dark" });
themes.apply();
var config = {};
var topMenus = [];
var SHOWALLPAGES = false;
async function fetchJSON(url) {
  const res = await fetch(url);
  if (!res.ok) {
    if (res.status === 401) {
      window.location.reload();
      return new Promise(() => {
      });
    }
    throw new Error(`Request to ${url} failed: ${res.status}`);
  }
  return res.json();
}
function titleCase(str) {
  let upper = true;
  let result = "";
  for (let i = 0; i < str.length; i++) {
    if (str[i] === " ") {
      upper = true;
      result += str[i];
      continue;
    }
    result += upper ? str[i].toUpperCase() : str[i].toLowerCase();
    upper = false;
  }
  return result;
}
function toggle(id) {
  const inner = document.getElementById(id + "_inner");
  const hidden = document.getElementById(id + "_hidden");
  if (!inner || !hidden) return;
  inner.hidden = !inner.hidden;
  hidden.hidden = !hidden.hidden;
}
function makeID(length) {
  const chars = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789";
  let result = "";
  for (let i = 0; i < length; i++) {
    result += chars.charAt(Math.floor(Math.random() * chars.length));
  }
  return result;
}
function copyToClipBoard(text) {
  const c = document.getElementById("copytext");
  const x = document.getElementById("hiddentext");
  if (!c || !x) return;
  c.value = text;
  x.hidden = false;
  c.select();
  try {
    document.execCommand("copy");
    showToast("Copied!", "success");
  } catch {
    showToast("Failed to copy.", "error");
  }
  x.hidden = true;
}
function renderSiteRow(site, i) {
  let link = "";
  if (site.url != null) link = site.url;
  if (site.port != null) link = site.url + ":" + site.port;
  if (site.prefix == null) site.prefix = makeID(5) + "_";
  let content = "";
  const label = site.label ?? link;
  if (link === "" && label !== "") {
    content += `<tr><td colspan="2">${label}</td></tr>`;
  } else if (link !== "" && label !== "" && link !== label) {
    content += `<tr><td colspan="2"><a href="${link}" target="_blank">${label}</a> : ${link}</td></tr>`;
  } else if (link !== "" && label !== "") {
    content += `<tr><td colspan="2"><a href="${link}" target="_blank">${label}</a></td></tr>`;
  }
  if (site.username != null && site.username !== "") {
    content += `<tr><td>Username: </td><td>${site.username}</td></tr><tr><td>Password: </td><td><div id="${site.prefix}pwd${i}_inner" hidden><input type="text" id="${site.prefix}txt${i}" value="${site.password}" readonly></div><div id="${site.prefix}pwd${i}_hidden">xxxxxxxxxx</div></td></tr><tr><td></td><td><table><tr><td><button type="button" data-toggle="${site.prefix}pwd${i}">Hide/Show</button></td><td><button type="button" data-copy="${site.password}">Copy</button></td></tr></table></td></tr>`;
  }
  return content;
}
function renderSiteTable(siteArray) {
  if (!siteArray) return "";
  return siteArray.map((s, i) => renderSiteRow(s, i)).join("");
}
function addPage(el, json) {
  const s = SHOWALLPAGES ? "" : " hidden";
  let content = `<div id="${json.id}" class="pageClass"${s}><h2>${json.title}</h2><br>`;
  content += '<table style="border-collapse:separate;border-spacing:15px 20px">';
  if (json.sites) content += renderSiteTable(json.sites);
  if (json.notes != null) {
    content += `<tr><td>Notes: </td><td><p>${json.notes}</p></td></tr>`;
  }
  if (json.gdoc != null) {
    content += `<tr><td>G-Doc: </td><td><p>${json.gdoc}</p></td></tr>`;
  }
  content += "</table></div>";
  if (SHOWALLPAGES) {
    content += '<div><a href="#top">&uarr;</a></div><div class="pageClass"><hr><br></div>';
  }
  el.insertAdjacentHTML("beforeend", content);
}
function showPage(id) {
  if (SHOWALLPAGES) {
    window.location.hash = "#" + id;
    return;
  }
  document.querySelectorAll(".pageClass").forEach((p) => {
    p.hidden = true;
  });
  const target = document.getElementById(id);
  if (target) target.hidden = false;
}
function closeAllDropdowns() {
  document.querySelectorAll(".topnav-item.open").forEach((d) => {
    d.classList.remove("open");
  });
}
function buildDropdown(group) {
  const item = document.createElement("div");
  item.className = "topnav-item";
  const btn = document.createElement("button");
  btn.type = "button";
  btn.className = "topnav-btn";
  const caret = document.createElement("span");
  caret.className = "topnav-caret";
  caret.textContent = "\u25BC";
  btn.append(group.subject, caret);
  const dropdown = document.createElement("div");
  dropdown.className = "topnav-dropdown";
  for (const sub of group.submenus) {
    if (sub.url != null) {
      const a = document.createElement("a");
      a.href = sub.url;
      a.target = "_blank";
      a.rel = "noopener";
      a.textContent = sub.title;
      dropdown.append(a);
    } else {
      const subBtn = document.createElement("button");
      subBtn.type = "button";
      subBtn.textContent = sub.title;
      subBtn.addEventListener("click", () => {
        showPage(sub.id);
        closeAllDropdowns();
      });
      dropdown.append(subBtn);
    }
  }
  btn.addEventListener("click", (e) => {
    e.stopPropagation();
    const wasOpen = item.classList.contains("open");
    closeAllDropdowns();
    if (!wasOpen) item.classList.add("open");
  });
  item.append(btn, dropdown);
  return item;
}
async function startMenuserver() {
  config = await fetchJSON("config/");
  SHOWALLPAGES = config.showAllPages ?? false;
  topMenus = await fetchJSON("items");
  const lowerSection = document.getElementById("lowerSection");
  const nav = document.getElementById("topnav");
  if (!lowerSection || !nav) return;
  const renderedPageSet = /* @__PURE__ */ new Set();
  let haveSplash = false;
  for (const group of topMenus) {
    group.subject = titleCase(group.subject);
    group.submenus = [];
    if (!group.entries) continue;
    for (const entry of group.entries) {
      const sub = await fetchJSON("menus/" + entry);
      sub.title = titleCase(sub.title);
      group.submenus.push(sub);
      if (!renderedPageSet.has(sub.id)) {
        addPage(lowerSection, sub);
        renderedPageSet.add(sub.id);
      }
    }
    if (group.subject !== "Splash") {
      nav.append(buildDropdown(group));
    } else {
      haveSplash = true;
    }
  }
  const settingsTrigger = document.createElement("button");
  settingsTrigger.className = "topnav-btn topnav-settings";
  settingsTrigger.type = "button";
  settingsTrigger.setAttribute("aria-label", "Menu");
  settingsTrigger.textContent = "\u2630";
  nav.append(settingsTrigger);
  new HamburgerMenu({
    title: "Settings",
    mountTrigger: settingsTrigger,
    side: "right",
    items: [],
    themePicker: true,
    themes
  });
  document.addEventListener("click", (e) => {
    if (!e.target.closest(".topnav-item")) {
      closeAllDropdowns();
    }
  });
  if (haveSplash) showPage("splash");
}
var lower = document.getElementById("lowerSection");
if (lower) {
  lower.addEventListener("click", (e) => {
    const target = e.target;
    const toggleId = target.getAttribute("data-toggle");
    if (toggleId) {
      toggle(toggleId);
      return;
    }
    const copyText = target.getAttribute("data-copy");
    if (copyText) {
      copyToClipBoard(copyText);
    }
  });
}
startMenuserver();
