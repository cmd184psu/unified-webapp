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

const hamburger = new HamburgerMenu({
  title: 'SMBEdit',
  items: [],
  themePicker: true,
  themes,
})

document.body.prepend(hamburger.trigger)

createRoot(document.getElementById('root')!).render(
  <StrictMode>
    <App />
  </StrictMode>,
)
