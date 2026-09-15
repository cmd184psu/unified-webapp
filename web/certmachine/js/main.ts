// Entry point for the certmachine app.

import "./cert.css";
import { mountCertApp } from "./ui";

async function bootstrap(): Promise<void> {
  const root = document.getElementById("cert-app");
  if (!root) {
    throw new Error("missing #cert-app root element");
  }
  await mountCertApp(root);
}

void bootstrap();
