import { ThemeManager, HamburgerMenu } from "/shared/dist/shared.mjs";

const themes = new ThemeManager({ module: "botchat", default: "puma" });
themes.apply();

const menu = new HamburgerMenu({
  title: "BotChat",
  items: [],
  themePicker: true,
  themes,
  side: "right",
});

const mount = () => {
  const host = document.getElementById("shell-actions");
  if (!host || host.childElementCount) return;
  host.append(menu.trigger);
};

mount();
new MutationObserver(mount).observe(document.documentElement, {
  childList: true,
  subtree: true,
});
