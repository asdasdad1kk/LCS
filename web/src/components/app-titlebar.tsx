import { useEffect, useRef, useState } from 'react'
import { Copy, Minus, Square, X } from 'lucide-react'
import { Logo } from '@/components/logo'
import { cn } from '@/lib/utils'

export type MenuEntry =
  | { type: 'sep' }
  | {
      type?: 'item'
      label: string
      shortcut?: string
      icon?: React.ComponentType<{ className?: string }>
      checked?: boolean
      disabled?: boolean
      danger?: boolean
      onSelect: () => void
    }

export type MenuDef = { id: string; label: string; items: MenuEntry[] }

const win = () => window.go?.main?.App

/** Barra superior de la ventana (frameless): marca, menús, título y controles. */
export function AppTitleBar({ menus, title }: { menus: MenuDef[]; title: string }) {
  const [openId, setOpenId] = useState<string | null>(null)
  const [maximised, setMaximised] = useState(false)
  const barRef = useRef<HTMLDivElement>(null)

  // Cierra el menú al hacer clic fuera, con Escape o al perder el foco de la ventana.
  useEffect(() => {
    if (!openId) return
    const onDown = (e: MouseEvent) => {
      if (!barRef.current?.contains(e.target as Node)) setOpenId(null)
    }
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape') setOpenId(null)
    }
    window.addEventListener('mousedown', onDown)
    window.addEventListener('keydown', onKey)
    window.addEventListener('blur', () => setOpenId(null))
    return () => {
      window.removeEventListener('mousedown', onDown)
      window.removeEventListener('keydown', onKey)
    }
  }, [openId])

  // El icono de maximizar cambia a "restaurar" según el estado real de la ventana.
  useEffect(() => {
    const check = () => {
      const result = window.runtime?.WindowIsMaximised?.()
      if (result && typeof result.then === 'function') void result.then(setMaximised).catch(() => {})
    }
    check()
    window.addEventListener('resize', check)
    return () => window.removeEventListener('resize', check)
  }, [])

  return (
    <div
      ref={barRef}
      className="app-chrome flex h-10 shrink-0 items-center"
      onDoubleClick={() => win()?.ToggleMaximise?.()}
    >
      <div className="flex shrink-0 items-center gap-2 pr-2 pl-3.5">
        <Logo className="size-5" />
        <span className="text-[12.5px] font-semibold tracking-tight text-white/90">LCS</span>
      </div>

      <nav className="titlebar-nodrag flex items-center gap-0.5" aria-label="Menú principal" onDoubleClick={(e) => e.stopPropagation()}>
        {menus.map((menu) => (
          <div key={menu.id} className="relative">
            <button
              type="button"
              className="chrome-menu-btn"
              data-open={openId === menu.id}
              onClick={() => setOpenId(openId === menu.id ? null : menu.id)}
              onMouseEnter={() => openId && setOpenId(menu.id)}
            >
              {menu.label}
            </button>
            {openId === menu.id && (
              <div role="menu" className="chrome-menu" onMouseDown={(e) => e.preventDefault()}>
                {menu.items.map((item, i) =>
                  item.type === 'sep' ? (
                    <div key={i} className="my-1 h-px bg-white/10" />
                  ) : (
                    <button
                      key={i}
                      role="menuitem"
                      type="button"
                      disabled={item.disabled}
                      className={cn('chrome-menu-item', item.danger && 'chrome-menu-danger')}
                      onClick={() => {
                        setOpenId(null)
                        item.onSelect()
                      }}
                    >
                      <span className="grid size-4 shrink-0 place-items-center">
                        {item.icon ? <item.icon className="size-3.5" /> : item.checked ? <span className="size-1.5 rounded-full bg-[var(--brand)]" /> : null}
                      </span>
                      <span className="flex-1 truncate text-left">{item.label}</span>
                      {item.shortcut && <kbd className="chrome-kbd">{item.shortcut}</kbd>}
                    </button>
                  )
                )}
              </div>
            )}
          </div>
        ))}
      </nav>

      <div className="pointer-events-none min-w-0 flex-1 truncate px-4 text-center text-[12px] text-white/45">{title}</div>

      <div className="window-controls titlebar-nodrag flex h-full shrink-0 items-center" onDoubleClick={(e) => e.stopPropagation()}>
        <button aria-label="Minimizar" title="Minimizar" onClick={() => win()?.Minimise?.()}>
          <Minus className="size-4" strokeWidth={1.5} />
        </button>
        <button
          aria-label={maximised ? 'Restaurar' : 'Maximizar'}
          title={maximised ? 'Restaurar' : 'Maximizar'}
          onClick={() => win()?.ToggleMaximise?.()}
        >
          {maximised ? <Copy className="size-3 -scale-x-100" strokeWidth={1.75} /> : <Square className="size-3" strokeWidth={1.75} />}
        </button>
        <button className="window-close" aria-label="Cerrar" title="Cerrar" onClick={() => win()?.Quit?.()}>
          <X className="size-4" strokeWidth={1.5} />
        </button>
      </div>
    </div>
  )
}
