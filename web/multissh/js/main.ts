// Entry point for the multi-system SSH terminal app.

import "./ssh.css";
import { fetchConfig } from "./api";
import { mountTabs } from "./tabs";

async function bootstrap(): Promise<void> {
  const root = document.getElementById("ssh-app");
  if (!root) {
    throw new Error("missing #ssh-app root element");
  }
  const cfg = await fetchConfig();
  mountTabs(root, cfg.maxSessions);
}

void bootstrap();
