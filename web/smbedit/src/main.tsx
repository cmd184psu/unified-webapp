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

export function initHamburger(): void {
  if (hamburger) return
  const trigger = document.getElementById('hamburger-trigger')
  if (!trigger) return
  hamburger = new HamburgerMenu({
    title: 'SMBEdit',
    items: [],
    themePicker: true,
    themes,
    mountTrigger: trigger,
  })
}

createRoot(document.getElementById('root')!).render(
  <StrictMode>
    <App />
  </StrictMode>,
)
