import { StrictMode } from 'react'
import { createRoot } from 'react-dom/client'
import { ThemeManager, HamburgerMenu, watchSecrets } from '@shared'
import App from './App'

// ThemeManager persists to `ui-theme:haproxy`, which index.html reads before
// first paint. No server round-trip and nothing for the operator to toggle
// beyond the theme picker — B1 has no settings yet.
watchSecrets()

export const themes = new ThemeManager({
  module: 'haproxy',
  default: 'dark',
})
themes.apply()

let hamburger: HamburgerMenu | null = null
let settingsHost: HTMLElement | null = null

/**
 * Mounts the shared hamburger on the topbar trigger and returns the drawer
 * slot later settings forms are portalled into. Idempotent, so StrictMode's
 * doubled effect gets the same slot back. The ☰ sits on the right, the module
 * convention across this codebase (FRD §9).
 */
export function initHamburger(): HTMLElement | null {
  if (hamburger) return settingsHost
  const trigger = document.getElementById('hamburger-trigger')
  if (!trigger) return null
  hamburger = new HamburgerMenu({
    title: 'Settings',
    items: [{ id: 'settings', render: (host) => { settingsHost = host } }],
    themePicker: true,
    themes,
    mountTrigger: trigger,
    side: 'right',
  })
  return settingsHost
}

createRoot(document.getElementById('root')!).render(
  <StrictMode>
    <App />
  </StrictMode>,
)
