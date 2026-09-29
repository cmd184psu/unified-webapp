import { StrictMode } from 'react'
import { createRoot } from 'react-dom/client'
import { ThemeManager, HamburgerMenu } from '@shared'
import App from './App'

let _persistTheme: (t: string) => void = () => {}

export function setPersistTheme(fn: (t: string) => void) { _persistTheme = fn }

export const themes = new ThemeManager({
  module: 'smbedit',
  default: 'dark',
  onChange: (name: string) => { _persistTheme(name) },
})
themes.apply()

let hamburger: HamburgerMenu | null = null
let settingsHost: HTMLElement | null = null

/**
 * Mounts the shared hamburger on the topbar trigger and returns the drawer
 * slot the settings form is portalled into. Idempotent, so StrictMode's
 * doubled effect gets the same slot back.
 */
export function initHamburger(): HTMLElement | null {
  if (hamburger) return settingsHost
  const trigger = document.getElementById('hamburger-trigger')
  if (!trigger) return null
  hamburger = new HamburgerMenu({
    title: 'Settings',
    items: [{ id: 'settings', render: host => { settingsHost = host } }],
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
