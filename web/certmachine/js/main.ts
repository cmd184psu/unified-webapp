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
  side: 'right',
});

async function bootstrap(): Promise<void> {
  const root = document.getElementById("cert-app");
  if (!root) {
    throw new Error("missing #cert-app root element");
  }
  const header = document.createElement("header");
  header.className = "app-header";
  const title = document.createElement("h1");
  title.textContent = "CertMachine";
  header.append(title, hamburger.trigger);
  root.before(header);
  await mountCertApp(root);
}

void bootstrap();
