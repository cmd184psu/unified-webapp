import "./cert.css";
import { ThemeManager, HamburgerMenu } from "@shared";
import { mountCertApp } from "./ui";

const themes = new ThemeManager({ module: "certmachine", default: "dark" });
themes.apply();

const hamburger = new HamburgerMenu({
  title: "CertMachine",
  items: [],
  themePicker: true,
  themes,
});

async function bootstrap(): Promise<void> {
  const root = document.getElementById("cert-app");
  if (!root) {
    throw new Error("missing #cert-app root element");
  }
  const nav = document.createElement("div");
  nav.id = "nav";
  nav.style.display = "flex";
  nav.style.justifyContent = "flex-end";
  nav.style.padding = "8px 16px";
  nav.appendChild(hamburger.trigger);
  root.before(nav);
  await mountCertApp(root);
}

void bootstrap();
