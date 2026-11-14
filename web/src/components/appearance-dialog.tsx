import { Check, Moon, Sun } from 'lucide-react'
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from '@/components/ui/dialog'
import { PALETTES, useTheme, type Palette } from '@/components/theme-provider'
import { cn } from '@/lib/utils'

// Miniatura de cada paleta: marco, panel, acento y texto (claro / oscuro).
const preview: Record<Palette, { light: string[]; dark: string[]; bg?: { light: string; dark: string } }> = {
  default: { light: ['#f1efe8', '#faf9f5', '#c96442', '#3d3929'], dark: ['#1e1d1b', '#262624', '#d97757', '#c3c0b6'] },
  windows: { light: ['#ededed', '#ffffff', '#0067c0', '#1b1b1b'], dark: ['#1a1a1a', '#2b2b2b', '#60cdff', '#f5f5f5'] },
  glass: {
    light: ['#ffffffa6', '#ffffffb8', '#007aff', '#2a2f45'],
    dark: ['#ffffff1a', '#ffffff14', '#0a84ff', '#f2f3f8'],
    bg: {
      light: 'linear-gradient(135deg, #b9d2ff, #e9c8ff 55%, #bdeedd)',
      dark: 'linear-gradient(135deg, #2b2f8f, #6a2a8c 55%, #1d5e6e)'
    }
  },
  plano: { light: ['#eceff4', '#fbfcfd', '#0f6d8a', '#232a3b'], dark: ['#14181f', '#1d2330', '#4cc3d9', '#dfe5ee'] }
}

export function AppearanceDialog({ open, onOpenChange }: { open: boolean; onOpenChange: (open: boolean) => void }) {
  const { theme, setTheme, palette, setPalette } = useTheme()
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-w-xl">
        <DialogHeader>
          <DialogTitle>Apariencia</DialogTitle>
          <DialogDescription>Elige el tema de color y el modo. Se guarda en este equipo.</DialogDescription>
        </DialogHeader>

        <div className="bg-muted inline-flex w-fit rounded-lg p-0.5">
          {(['light', 'dark'] as const).map((mode) => (
            <button
              key={mode}
              type="button"
              onClick={() => setTheme(mode)}
              className={cn(
                'flex h-7 items-center gap-1.5 rounded-md px-3 text-[12.5px] transition-colors',
                theme === mode ? 'bg-card text-foreground shadow-xs' : 'text-muted-foreground hover:text-foreground'
              )}
            >
              {mode === 'light' ? <Sun className="size-3.5" /> : <Moon className="size-3.5" />}
              {mode === 'light' ? 'Claro' : 'Oscuro'}
            </button>
          ))}
        </div>

        <div className="grid grid-cols-2 gap-3">
          {PALETTES.map((p) => {
            const [frame, panel, accent, text] = preview[p.id][theme]
            const selected = palette === p.id
            return (
              <button
                key={p.id}
                type="button"
                onClick={() => setPalette(p.id)}
                className={cn(
                  'group rounded-xl border p-2.5 text-left transition-all',
                  selected ? 'border-primary ring-primary/30 ring-2' : 'hover:border-foreground/25'
                )}
              >
                <div
                  className="flex h-[78px] gap-1.5 overflow-hidden rounded-lg border p-1.5"
                  style={{ background: preview[p.id].bg?.[theme] ?? frame }}
                >
                  <div className="w-8 shrink-0 rounded-md" style={{ background: preview[p.id].bg ? frame : 'transparent' }}>
                    {[0, 1, 2].map((i) => (
                      <div key={i} className="mx-1 mt-1.5 h-1.5 rounded-full" style={{ background: i === 0 ? accent : text, opacity: i === 0 ? 1 : 0.28 }} />
                    ))}
                  </div>
                  <div className="flex-1 rounded-md p-1.5" style={{ background: panel }}>
                    <div className="h-1.5 w-10 rounded-full" style={{ background: text, opacity: 0.7 }} />
                    <div className="mt-1.5 h-1 w-14 rounded-full" style={{ background: text, opacity: 0.25 }} />
                    <div className="mt-3 h-3.5 w-9 rounded" style={{ background: accent }} />
                  </div>
                </div>
                <div className="mt-2 flex items-center gap-1.5">
                  <span className="text-[13px] font-medium">{p.name}</span>
                  {selected && <Check className="text-primary size-3.5" />}
                </div>
                <p className="text-muted-foreground mt-0.5 text-[11.5px] leading-snug">{p.description}</p>
              </button>
            )
          })}
        </div>
      </DialogContent>
    </Dialog>
  )
}
