import { createContext, useContext, useEffect, useState, ReactNode } from 'react'

export type ThemeMode = 'dark' | 'light' | 'system'

interface ThemeCtx {
  mode: ThemeMode
  resolved: 'dark' | 'light'
  setMode: (m: ThemeMode) => void
}

const Ctx = createContext<ThemeCtx>({
  mode: 'dark',
  resolved: 'dark',
  setMode: () => {},
})

export function ThemeProvider({ initial, onChange, children }: {
  initial: ThemeMode
  onChange: (m: ThemeMode) => void
  children: ReactNode
}) {
  const [mode, setModeState] = useState<ThemeMode>(initial)

  const systemDark = window.matchMedia('(prefers-color-scheme: dark)').matches
  const resolved: 'dark' | 'light' =
    mode === 'system' ? (systemDark ? 'dark' : 'light') : mode

  useEffect(() => {
    document.documentElement.setAttribute('data-theme', resolved)
  }, [resolved])

  const setMode = (m: ThemeMode) => {
    setModeState(m)
    onChange(m)
  }

  return <Ctx.Provider value={{ mode, resolved, setMode }}>{children}</Ctx.Provider>
}

export const useTheme = () => useContext(Ctx)
