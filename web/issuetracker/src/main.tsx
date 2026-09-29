import React from "react";
import ReactDOM from "react-dom/client";
import { BrowserRouter } from "react-router-dom";
import { ThemeManager, HamburgerMenu } from "@shared";
import App from "./App";
import { DataProvider } from "./DataContext";
import "./styles.css";

const themes = new ThemeManager({
  module: "issuetracker",
  default: "dark",
});
themes.apply();

const hamburger = new HamburgerMenu({
  title: "IssueTracker",
  items: [],
  themePicker: true,
  themes,
  side: "right",
});

// Pinned to the window's top-right corner (see .app-menu-corner), at the end
// of each page's top bar, instead of in a strip of its own above the app. The
// corner also holds the shared sign-out icon, which lands before the trigger.
const menuCorner = document.createElement("div");
menuCorner.className = "app-menu-corner";
menuCorner.append(hamburger.trigger);
document.body.append(menuCorner);

ReactDOM.createRoot(document.getElementById("root")!).render(
  <React.StrictMode>
    <BrowserRouter>
      <DataProvider>
        <App />
      </DataProvider>
    </BrowserRouter>
  </React.StrictMode>
);
