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
});

document.body.prepend(hamburger.trigger);

ReactDOM.createRoot(document.getElementById("root")!).render(
  <React.StrictMode>
    <BrowserRouter>
      <DataProvider>
        <App />
      </DataProvider>
    </BrowserRouter>
  </React.StrictMode>
);
