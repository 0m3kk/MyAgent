import { createContext, useCallback, useContext, useEffect, useMemo, useRef, useState, type ReactNode } from 'react'
import { api } from './api'
import type { Approval, Bootstrap } from './types'
import { usePolling } from './hooks'

const FONT_SCALE_KEY = 'myagent_font_scale'
const THEME_KEY = 'myagent_theme'
const FONT_MIN = 0.85
const FONT_MAX = 1.6
const FONT_STEP = 0.05

type ThemeMode = 'light' | 'dark' | 'system'

interface DataContextValue {
  data: Bootstrap | null
  loading: boolean
  error: string | null
  approvals: Approval[]
  refresh: () => Promise<void>
  refreshApprovals: () => Promise<void>
  theme: ThemeMode
  resolvedTheme: 'light' | 'dark'
  setTheme: (mode: ThemeMode) => void
  fontScale: number
  setFontScale: (next: number) => void
  increaseFontScale: () => void
  decreaseFontScale: () => void
  resetFontScale: () => void
}

const DataContext = createContext<DataContextValue | null>(null)

function initialTheme(): ThemeMode {
  const saved = localStorage.getItem(THEME_KEY)
  if (saved === 'light' || saved === 'dark' || saved === 'system') return saved
  return 'system'
}

function systemPrefersDark(): boolean {
  return window.matchMedia?.('(prefers-color-scheme: dark)').matches ?? false
}

function initialFontScale(): number {
  const saved = Number(localStorage.getItem(FONT_SCALE_KEY))
  if (Number.isFinite(saved) && saved >= FONT_MIN && saved <= FONT_MAX) return saved
  return 1
}

function clampFontScale(value: number): number {
  return Math.min(FONT_MAX, Math.max(FONT_MIN, Math.round(value * 100) / 100))
}

export function DataProvider({ children }: { children: ReactNode }) {
  const [data, setData] = useState<Bootstrap | null>(null)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [approvals, setApprovals] = useState<Approval[]>([])
  const [theme, setTheme] = useState<ThemeMode>(initialTheme)
  const [systemDark, setSystemDark] = useState(systemPrefersDark)
  const [fontScale, setFontScaleState] = useState(initialFontScale)
  const fontScaleRef = useRef(fontScale)
  const appearanceHydrated = useRef(false)
  const resolvedTheme: 'light' | 'dark' = theme === 'system' ? (systemDark ? 'dark' : 'light') : theme

  const refresh = useCallback(async () => {
    try {
      const next = await api.bootstrap()
      setData(next)
      setApprovals(next.approvals ?? [])
      setError(null)
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Cannot reach the local agent')
    } finally {
      setLoading(false)
    }
  }, [])

  const refreshApprovals = useCallback(async () => {
    try {
      const res = await api.approvals()
      setApprovals(res.approvals)
    } catch {
      // Keep the last known list while the server restarts.
    }
  }, [])

  useEffect(() => {
    void refresh()
  }, [refresh])

  // Appearance is stored in SQLite so it survives the random loopback port
  // used by each native-app launch. localStorage is only a first-paint cache
  // and a migration source from older builds.
  useEffect(() => {
    if (!data || appearanceHydrated.current) return
    appearanceHydrated.current = true
    const serverTheme = data.settings.theme
    const localTheme = localStorage.getItem(THEME_KEY)
    let nextTheme: ThemeMode = serverTheme === 'light' || serverTheme === 'dark' || serverTheme === 'system'
      ? serverTheme
      : 'system'
    let migrateTheme = false
    if (nextTheme === 'system' && (localTheme === 'light' || localTheme === 'dark')) {
      nextTheme = localTheme
      migrateTheme = true
    }
    const serverFont = Number(data.settings.font_scale)
    const localFont = Number(localStorage.getItem(FONT_SCALE_KEY))
    const serverFontValid = Number.isFinite(serverFont) && serverFont >= FONT_MIN && serverFont <= FONT_MAX
    const localFontValid = Number.isFinite(localFont) && localFont >= FONT_MIN && localFont <= FONT_MAX
    let nextFont = serverFontValid ? serverFont : 1
    let migrateFont = false
    if (serverFontValid && serverFont === 1 && localFontValid && localFont !== 1) {
      // Upgrade path from the localStorage-only build.
      nextFont = clampFontScale(localFont)
      migrateFont = true
    } else if (!serverFontValid && localFontValid) {
      nextFont = clampFontScale(localFont)
      migrateFont = true
    }
    setTheme(nextTheme)
    setFontScaleState(nextFont)
    fontScaleRef.current = nextFont
    if (migrateTheme) void api.updateSettings({ theme: nextTheme }).catch(() => {})
    if (migrateFont) void api.updateSettings({ font_scale: nextFont }).catch(() => {})
  }, [data])

  useEffect(() => {
    const media = window.matchMedia?.('(prefers-color-scheme: dark)')
    if (!media) return
    const onChange = (e: MediaQueryListEvent) => setSystemDark(e.matches)
    media.addEventListener('change', onChange)
    return () => media.removeEventListener('change', onChange)
  }, [])

  useEffect(() => {
    document.documentElement.dataset.theme = resolvedTheme
    document.documentElement.style.colorScheme = resolvedTheme
    localStorage.setItem(THEME_KEY, theme)
  }, [theme, resolvedTheme])

  usePolling(refreshApprovals, 12_000, !!data)

  useEffect(() => {
    document.documentElement.style.setProperty('--font-scale', String(fontScale))
    localStorage.setItem(FONT_SCALE_KEY, String(fontScale))
    fontScaleRef.current = fontScale
  }, [fontScale])

  const commitFontScale = useCallback((next: number) => {
    const value = clampFontScale(next)
    fontScaleRef.current = value
    setFontScaleState(value)
    void api.updateSettings({ font_scale: value }).catch(() => {})
  }, [])
  const setFontScale = commitFontScale
  const increaseFontScale = useCallback(() => commitFontScale(fontScaleRef.current + FONT_STEP), [commitFontScale])
  const decreaseFontScale = useCallback(() => commitFontScale(fontScaleRef.current - FONT_STEP), [commitFontScale])
  const resetFontScale = useCallback(() => commitFontScale(1), [commitFontScale])
  const setThemeMode = useCallback((mode: ThemeMode) => {
    setTheme(mode)
    void api.updateSettings({ theme: mode }).catch(() => {})
  }, [])

  const value = useMemo(
    () => ({ data, loading, error, approvals, refresh, refreshApprovals, theme, resolvedTheme, setTheme: setThemeMode,
      fontScale, setFontScale, increaseFontScale, decreaseFontScale, resetFontScale }),
    [data, loading, error, approvals, refresh, refreshApprovals, theme, resolvedTheme, setThemeMode,
      fontScale, setFontScale, increaseFontScale, decreaseFontScale, resetFontScale],
  )
  return <DataContext.Provider value={value}>{children}</DataContext.Provider>
}

export function useApp() {
  const ctx = useContext(DataContext)
  if (!ctx) throw new Error('useApp must be used inside DataProvider')
  return ctx
}
