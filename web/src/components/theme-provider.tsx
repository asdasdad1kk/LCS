import * as React from 'react'

type Mode = 'light' | 'dark'
/** Fondo de la ventana nativa: opaca o Acrylic. */
export type Backdrop = 'off' | 'acrylic'
export type Palette = 'default' | 'windows' | 'glass' | 'plano' | 'obsidian'

export const PALETTES: { id: Palette; name: string; description: string }[] = [
  { id: 'default', name: 'Default', description: 'Crema cálido y terracota. El tema por defecto.' },
  { id: 'windows', name: 'Windows moderno', description: 'Fluent: superficies planas, azul de sistema y esquinas suaves.' },
  { id: 'glass', name: 'Cristal', description: 'Glassmorphism: paneles translúcidos con desenfoque sobre un degradado.' },
  { id: 'obsidian', name: 'Obsidiana', description: 'Vidrio ahumado con acento naranja y navegación flotante.' },
  { id: 'plano', name: 'Plano técnico', description: 'Pizarra azulada con acento petróleo.' }
]

type ThemeContextValue = {
  theme: Mode
  toggle: () => void
  setTheme: (mode: Mode) => void
  palette: Palette
  setPalette: (palette: Palette) => void
}

const ThemeContext = React.createContext<ThemeContextValue>({
  theme: 'light',
  toggle: () => {},
  setTheme: () => {},
  palette: 'default',
  setPalette: () => {}
})

const read = (key: string) => {
  try {
    return localStorage.getItem(key)
  } catch {
    return null
  }
}

export function ThemeProvider({ children }: { children: React.ReactNode }) {
  const [theme, setTheme] = React.useState<Mode>(() => {
    const stored = read('lcs-theme')
    if (stored === 'light' || stored === 'dark') return stored
    return window.matchMedia('(prefers-color-scheme: dark)').matches ? 'dark' : 'light'
  })
  const [palette, setPalette] = React.useState<Palette>(() => {
    const stored = read('lcs-palette')
    return PALETTES.some((p) => p.id === stored) ? (stored as Palette) : 'default'
  })

  // Fondo de la ventana (off | acrylic): lo reporta Wails según cómo arrancó.
  React.useEffect(() => {
    const state = window.go?.main?.App?.WindowBackdrop?.()
    if (state) void state.then((r) => (document.documentElement.dataset.acrylic = r.active)).catch(() => {})
  }, [])

  React.useEffect(() => {
    const root = document.documentElement
    root.classList.toggle('dark', theme === 'dark')
    root.style.colorScheme = theme
    root.dataset.palette = palette
    try {
      localStorage.setItem('lcs-theme', theme)
      localStorage.setItem('lcs-palette', palette)
    } catch {
      /* sin almacenamiento: la preferencia dura solo esta sesión */
    }
  }, [theme, palette])

  const toggle = React.useCallback(() => setTheme((t) => (t === 'dark' ? 'light' : 'dark')), [])

  return (
    <ThemeContext.Provider value={{ theme, toggle, setTheme, palette, setPalette }}>{children}</ThemeContext.Provider>
  )
}

export function useTheme() {
  return React.useContext(ThemeContext)
}
