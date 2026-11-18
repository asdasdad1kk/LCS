import { forwardRef, useCallback, useEffect, useImperativeHandle, useRef, useState } from 'react'
import type { LayoutInfo } from '@/lib/api'
import { cn } from '@/lib/utils'
import { AlertTriangle, Check, Crop, Download, Loader2, Maximize2, Moon, Sun, Target, X } from 'lucide-react'
import { Button } from '@/components/ui/button'

/** Una zona del plano en coordenadas del dibujo (no de la pantalla). */
export type Bounds = { minX: number; minY: number; maxX: number; maxY: number }

export function unionBounds(list: Bounds[]): Bounds | null {
  if (!list.length) return null
  return list.reduce((a, b) => ({
    minX: Math.min(a.minX, b.minX),
    minY: Math.min(a.minY, b.minY),
    maxX: Math.max(a.maxX, b.maxX),
    maxY: Math.max(a.maxY, b.maxY)
  }))
}

export type CadViewerHandle = {
  showLayout: (btrId: string) => void
  fit: () => void
  /** Encuadra una zona del plano (coordenadas del dibujo). */
  focusBounds: (b: Bounds, margin?: number) => Promise<void>
  /** Lo que se ve ahora, en coordenadas del dibujo. */
  visibleBounds: () => Bounds | null
}

type Props = {
  drawingId: string
  fileName: string
  refName?: string
  className?: string
  onLayouts?: (layouts: LayoutInfo[]) => void
  onLoaded?: () => void
  /**
   * Si se pasa, el recorte se puede guardar con la tarea además de descargarlo.
   * El visor entrega la imagen PNG y la nota que escribió la persona.
   */
  onSnapshot?: (png: Blob, caption: string, bounds: Bounds | null) => Promise<void>
  /** Zona a encuadrar apenas el plano esté listo (y cada vez que cambie). */
  focus?: Bounds | null
  /** Zonas marcadas con recortes: habilitan el botón "Enfocar cambios". */
  changes?: Bounds[]
  /** Se llama cuando la cámara se mueve (para sincronizar dos visores). */
  onCamera?: (b: Bounds) => void
  /** Oculta el botón de recortar (vistas incrustadas de comparación). */
  noCrop?: boolean
}

async function zoomToBounds(view: any, b: Bounds, margin = 1.15) {
  const { AcGeBox2d } = await import('@mlightcad/data-model')
  view.zoomTo(new AcGeBox2d({ x: b.minX, y: b.minY }, { x: b.maxX, y: b.maxY }), margin)
}

function readVisible(view: any, host: HTMLElement | null): Bounds | null {
  try {
    const canvas = host?.querySelector('canvas') as HTMLCanvasElement | null
    if (!view || !canvas) return null
    const w = canvas.clientWidth
    const h = canvas.clientHeight
    const a = view.screenToWorld({ x: 0, y: 0 })
    const b = view.screenToWorld({ x: w, y: h })
    return {
      minX: Math.min(a.x, b.x),
      minY: Math.min(a.y, b.y),
      maxX: Math.max(a.x, b.x),
      maxY: Math.max(a.y, b.y)
    }
  } catch {
    return null
  }
}

type Bg = 'light' | 'dark'
const BG_KEY = 'lcs-viewer-bg'
const BG_COLOR: Record<Bg, number> = { light: 0xffffff, dark: 0x1c1c1e }

function initialBg(): Bg {
  try {
    const stored = localStorage.getItem(BG_KEY)
    if (stored === 'light' || stored === 'dark') return stored
  } catch {
    /* sin almacenamiento */
  }
  return document.documentElement.classList.contains('dark') ? 'dark' : 'light'
}

type Rect = { x: number; y: number; w: number; h: number }

/**
 * Copia lo que el visor dibuja en WebGL. El lienzo no conserva el búfer entre
 * cuadros, así que se pide un redibujado y se lee justo al terminar de dibujar.
 */
function grabFrame(view: any): Promise<HTMLCanvasElement | null> {
  return new Promise((resolve) => {
    const gl = view?.renderer?.internalRenderer
    const src: HTMLCanvasElement | undefined = gl?.domElement ?? view?.renderer?.domElement
    if (!src) return resolve(null)
    const copy = () => {
      const c = document.createElement('canvas')
      c.width = src.width
      c.height = src.height
      c.getContext('2d')?.drawImage(src, 0, 0)
      return c
    }
    if (!gl) return resolve(copy())
    let done = false
    const finish = (c: HTMLCanvasElement | null) => {
      if (done) return
      done = true
      delete gl.render // vuelve al render original (el del prototipo)
      resolve(c)
    }
    const original = gl.render.bind(gl)
    gl.render = (...args: unknown[]) => {
      original(...args)
      finish(copy())
    }
    view.isDirty = true
    setTimeout(() => finish(copy()), 1500)
  })
}

type CadModule = typeof import('@mlightcad/cad-simple-viewer')

let cadModulePromise: Promise<CadModule> | null = null
let converterReady = false

async function loadCad(): Promise<CadModule> {
  if (!cadModulePromise) {
    cadModulePromise = (async () => {
      const mod = await import('@mlightcad/cad-simple-viewer')
      if (!converterReady) {
        const [{ AcDbDatabaseConverterManager, AcDbFileType }, { AcDbLibreDwgConverter }] =
          await Promise.all([
            import('@mlightcad/data-model'),
            import('@mlightcad/libredwg-converter')
          ])
        AcDbDatabaseConverterManager.instance.register(
          AcDbFileType.DWG,
          new AcDbLibreDwgConverter({
            convertByEntityType: false,
            useWorker: true,
            parserWorkerUrl: '/workers/libredwg-parser-worker.js'
          })
        )
        converterReady = true
      }
      return mod
    })()
  }
  return cadModulePromise
}

/**
 * The viewer keeps its layout switcher on a private field. We probe the
 * documented getters first and fall back to the runtime fields the shipped
 * bundle exposes.
 */
function setActiveLayout(view: unknown, btrId: string): boolean {
  const v = view as Record<string, any>
  const scene = v.scene ?? v._scene
  if (scene?.activeLayoutBtrId !== undefined) {
    try {
      scene.activeLayoutBtrId = btrId
      return true
    } catch {
      /* fall through */
    }
  }
  const lvm = v.layoutViewManager ?? v._layoutViewManager
  if (lvm?.activeLayoutBtrId !== undefined) {
    try {
      lvm.activeLayoutBtrId = btrId
      return true
    } catch {
      /* fall through */
    }
  }
  return false
}

export const CadViewer = forwardRef<CadViewerHandle, Props>(function CadViewer(
  { drawingId, fileName, refName, className, onLayouts, onLoaded, onSnapshot, focus, changes, onCamera, noCrop },
  ref
) {
  const hostRef = useRef<HTMLDivElement>(null)
  const managerRef = useRef<any>(null)
  const [state, setState] = useState<'loading' | 'ready' | 'error'>('loading')
  const [error, setError] = useState<string>('')
  const [progress, setProgress] = useState<string>('iniciando visor…')
  const [bg, setBg] = useState<Bg>(initialBg)
  const [cropping, setCropping] = useState(false)
  const [drag, setDrag] = useState<Rect | null>(null)
  const [shot, setShot] = useState<{ url: string; blob: Blob; area: Bounds | null } | null>(null)
  const [caption, setCaption] = useState('')
  const [saving, setSaving] = useState(false)
  const overlayRef = useRef<HTMLDivElement>(null)
  const dragStart = useRef<{ x: number; y: number } | null>(null)

  const applyBg = useCallback((mode: Bg) => {
    try {
      const view = managerRef.current?.curView
      if (view) view.backgroundColor = BG_COLOR[mode]
    } catch {
      /* el visor puede no estar listo */
    }
  }, [])

  const toggleBg = () => {
    const next: Bg = bg === 'light' ? 'dark' : 'light'
    setBg(next)
    try {
      localStorage.setItem(BG_KEY, next)
    } catch {
      /* ignore */
    }
  }

  const finishCrop = async (r: Rect) => {
    setCropping(false)
    setDrag(null)
    if (r.w < 8 || r.h < 8) return
    const view = managerRef.current?.curView
    const frame = await grabFrame(view)
    const host = hostRef.current
    if (!frame || !host) return
    const canvas = host.querySelector('canvas') as HTMLCanvasElement | null
    const box = (canvas ?? host).getBoundingClientRect()
    const hostBox = host.getBoundingClientRect()
    const sx = frame.width / box.width
    const sy = frame.height / box.height
    const left = (hostBox.left - box.left + r.x) * sx
    const top = (hostBox.top - box.top + r.y) * sy
    const out = document.createElement('canvas')
    out.width = Math.max(1, Math.round(r.w * sx))
    out.height = Math.max(1, Math.round(r.h * sy))
    const ctx = out.getContext('2d')
    if (!ctx) return
    ctx.fillStyle = bg === 'light' ? '#ffffff' : '#1c1c1e'
    ctx.fillRect(0, 0, out.width, out.height)
    ctx.drawImage(frame, left, top, out.width, out.height, 0, 0, out.width, out.height)
    // La zona en coordenadas del dibujo: así se puede volver a encuadrar en
    // cualquier versión del plano, sin depender del tamaño de la ventana.
    let area: Bounds | null = null
    try {
      const ox = hostBox.left - box.left
      const oy = hostBox.top - box.top
      const a = view.screenToWorld({ x: ox + r.x, y: oy + r.y })
      const b = view.screenToWorld({ x: ox + r.x + r.w, y: oy + r.y + r.h })
      area = {
        minX: Math.min(a.x, b.x),
        minY: Math.min(a.y, b.y),
        maxX: Math.max(a.x, b.x),
        maxY: Math.max(a.y, b.y)
      }
    } catch {
      /* sin coordenadas: el recorte se guarda igual, solo sin "enfocar" */
    }
    out.toBlob((blob) => {
      if (!blob) return
      setCaption('')
      setShot({ url: URL.createObjectURL(blob), blob, area })
    }, 'image/png')
  }

  const closeShot = () => {
    if (shot) URL.revokeObjectURL(shot.url)
    setShot(null)
  }

  const downloadShot = () => {
    if (!shot) return
    const a = document.createElement('a')
    a.href = shot.url
    a.download = fileName.replace(/\.dwg$/i, '') + '-recorte.png'
    a.click()
  }

  const saveShot = async () => {
    if (!shot || !onSnapshot) return
    setSaving(true)
    try {
      await onSnapshot(shot.blob, caption.trim(), shot.area)
      closeShot()
    } catch {
      /* quien llama muestra el error; el recorte queda abierto para reintentar */
    } finally {
      setSaving(false)
    }
  }

  const pointer = (e: React.PointerEvent) => {
    const box = overlayRef.current?.getBoundingClientRect()
    if (!box) return { x: 0, y: 0 }
    return {
      x: Math.min(Math.max(e.clientX - box.left, 0), box.width),
      y: Math.min(Math.max(e.clientY - box.top, 0), box.height)
    }
  }

  useImperativeHandle(ref, () => ({
    showLayout(btrId: string) {
      const manager = managerRef.current
      if (!manager) return
      const ok = setActiveLayout(manager.curView, btrId)
      if (!ok) return
      try {
        manager.curView?.zoomToFitDrawing?.(1500, btrId)
      } catch {
        /* zoom is best effort */
      }
    },
    fit() {
      try {
        managerRef.current?.curView?.zoomToFitDrawing?.(800)
      } catch {
        /* ignore */
      }
    },
    async focusBounds(b: Bounds, margin?: number) {
      const view = managerRef.current?.curView
      if (view) await zoomToBounds(view, b, margin)
    },
    visibleBounds() {
      return readVisible(managerRef.current?.curView, hostRef.current)
    }
  }))

  useEffect(() => {
    let cancelled = false
    const host = hostRef.current
    if (!host) return

    ;(async () => {
      try {
        const mod = await loadCad()
        if (cancelled) return

        const { AcApDocManager, AcEdOpenMode, LIBREDWG_PARSER_WORKER_FILE, MTEXT_RENDERER_WORKER_FILE, acapListFindLayouts } =
          mod as any

        const existing = AcApDocManager.tryGetInstance?.()
        if (existing) await existing.destroy()
        if (cancelled) return

        const manager = AcApDocManager.createInstance({
          container: host,
          autoResize: true,
          baseUrl: '/cad-data/',
          webworkerFileUrls: {
            dwgParser: `/workers/${LIBREDWG_PARSER_WORKER_FILE}`,
            mtextRender: `/workers/${MTEXT_RENDERER_WORKER_FILE}`
          }
        })
        if (!manager) throw new Error('no se pudo crear el visor CAD')
        managerRef.current = manager

        try {
          mod.AcApI18n?.setCurrentLocale?.('en')
        } catch {
          /* optional */
        }

        setProgress('descargando plano…')
        const url = `/api/drawings/${drawingId}/file${refName ? `?ref=${encodeURIComponent(refName)}` : ''}`
        const res = await fetch(url)
        if (!res.ok) throw new Error(`no pude descargar el DWG (HTTP ${res.status})`)
        const buffer = await res.arrayBuffer()
        if (cancelled) return

        setProgress('parseando DWG…')
        const onProgress = (e: any) => {
          const pct = e?.percent ?? e?.progress
          setProgress(
            typeof pct === 'number' && pct > 0 && pct <= 100
              ? `parseando DWG… ${Math.round(pct)}%`
              : 'parseando DWG…'
          )
        }
        mod.eventBus?.on?.('open-file-progress', onProgress)

        const ok = await manager.openDocument(fileName, buffer, {
          mode: AcEdOpenMode.Read
        })
        mod.eventBus?.off?.('open-file-progress', onProgress)
        if (cancelled) return
        if (!ok) throw new Error('LibreDWG no pudo interpretar el archivo')

        const layouts: LayoutInfo[] = acapListFindLayouts(manager.curDocument.database)
        if (layouts.length) onLayouts?.(layouts)

        applyBg(initialBg())
        setState('ready')
        onLoaded?.()
      } catch (err) {
        if (cancelled) return
        setError(err instanceof Error ? err.message : String(err))
        setState('error')
      }
    })()

    return () => {
      cancelled = true
      const manager = managerRef.current
      managerRef.current = null
      manager?.destroy?.().catch?.(() => {})
    }
  }, [drawingId, fileName, refName])

  useEffect(() => {
    if (state === 'ready') applyBg(bg)
  }, [bg, state, applyBg])

  // Encuadrar la zona pedida cuando el plano termina de cargar o la zona cambia.
  const focusKey = focus ? [focus.minX, focus.minY, focus.maxX, focus.maxY].join(',') : ''
  useEffect(() => {
    if (state !== 'ready' || !focus) return
    const view = managerRef.current?.curView
    if (!view) return
    const id = window.setTimeout(() => void zoomToBounds(view, focus).catch(() => {}), 250)
    return () => window.clearTimeout(id)
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [state, focusKey])

  // Aviso de cámara: se compara lo visible cada 150 ms (el visor no emite un evento utilizable).
  const lastCam = useRef('')
  useEffect(() => {
    if (state !== 'ready' || !onCamera) return
    const id = window.setInterval(() => {
      const b = readVisible(managerRef.current?.curView, hostRef.current)
      if (!b) return
      const key = [b.minX, b.minY, b.maxX, b.maxY].map((n) => n.toFixed(3)).join(',')
      if (key === lastCam.current) return
      lastCam.current = key
      onCamera(b)
    }, 150)
    return () => window.clearInterval(id)
  }, [state, onCamera])

  const focusChanges = () => {
    const view = managerRef.current?.curView
    const all = unionBounds(changes ?? [])
    if (view && all) void zoomToBounds(view, all)
  }

  return (
    <div
      className={cn(
        'lcs-viewer relative min-h-0 overflow-hidden',
        bg === 'light' ? 'bg-white' : 'bg-zinc-900',
        className
      )}
    >
      <div ref={hostRef} className="absolute inset-0" />

      {state === 'ready' && (
        <div className="bg-card/90 absolute top-2 right-2 z-10 flex gap-1 rounded-md border p-1 shadow-sm backdrop-blur">
          <Button
            size="icon"
            variant="ghost"
            className="size-7"
            title="Ajustar al plano"
            onClick={() => managerRef.current?.curView?.zoomToFitDrawing?.(800)}
          >
            <Maximize2 className="size-4" />
          </Button>
          {!!changes?.length && (
            <Button
              size="sm"
              variant="ghost"
              className="h-7 gap-1 px-2 text-[12px]"
              title="Ir a las zonas marcadas con recortes"
              onClick={focusChanges}
            >
              <Target className="size-4" /> Enfocar cambios
            </Button>
          )}
          <Button
            size="icon"
            variant="ghost"
            className="size-7"
            title={bg === 'light' ? 'Fondo oscuro' : 'Fondo claro'}
            onClick={toggleBg}
          >
            {bg === 'light' ? <Moon className="size-4" /> : <Sun className="size-4" />}
          </Button>
          {!noCrop && (
            <Button
              size="icon"
              variant={cropping ? 'default' : 'ghost'}
              className="size-7"
              title="Recortar una zona del plano"
              onClick={() => setCropping((c) => !c)}
            >
              <Crop className="size-4" />
            </Button>
          )}
        </div>
      )}

      {cropping && (
        <div
          ref={overlayRef}
          className="absolute inset-0 z-20 cursor-crosshair touch-none bg-black/10 select-none"
          onPointerDown={(e) => {
            e.currentTarget.setPointerCapture(e.pointerId)
            const p = pointer(e)
            dragStart.current = p
            setDrag({ x: p.x, y: p.y, w: 0, h: 0 })
          }}
          onPointerMove={(e) => {
            const s = dragStart.current
            if (!s) return
            const p = pointer(e)
            setDrag({
              x: Math.min(s.x, p.x),
              y: Math.min(s.y, p.y),
              w: Math.abs(p.x - s.x),
              h: Math.abs(p.y - s.y)
            })
          }}
          onPointerUp={() => {
            const r = drag
            dragStart.current = null
            if (r) void finishCrop(r)
            else setCropping(false)
          }}
        >
          <div className="bg-card/95 pointer-events-none absolute top-2 left-1/2 -translate-x-1/2 rounded-md border px-3 py-1 text-[12px] shadow">
            Arrastrá para encerrar la zona que se va a cambiar
          </div>
          {drag && drag.w > 0 && (
            <div
              className="border-primary bg-primary/10 absolute border-2 border-dashed"
              style={{ left: drag.x, top: drag.y, width: drag.w, height: drag.h }}
            />
          )}
        </div>
      )}

      {shot && (
        <div className="absolute inset-0 z-30 grid place-items-center bg-black/50 p-4">
          <div className="bg-card flex max-h-full w-full max-w-xl flex-col gap-3 rounded-lg border p-3 shadow-xl">
            <div className="flex items-center justify-between">
              <span className="text-[13px] font-semibold">Recorte del layout</span>
              <Button size="icon" variant="ghost" className="size-7" onClick={closeShot}>
                <X className="size-4" />
              </Button>
            </div>
            <div className="min-h-0 flex-1 overflow-auto rounded-md border bg-white">
              <img src={shot.url} alt="Recorte del plano" className="mx-auto max-h-[46vh] object-contain" />
            </div>
            {onSnapshot && (
              <input
                value={caption}
                onChange={(e) => setCaption(e.target.value)}
                placeholder="Nota: qué se va a cambiar en esta zona"
                className="bg-background focus-visible:ring-ring h-9 rounded-md border px-3 text-[13px] outline-none focus-visible:ring-2"
              />
            )}
            <div className="flex flex-wrap justify-end gap-2">
              <Button size="sm" variant="outline" onClick={downloadShot}>
                <Download className="size-4" /> Descargar PNG
              </Button>
              {onSnapshot && (
                <Button size="sm" onClick={saveShot} disabled={saving}>
                  {saving ? <Loader2 className="size-4 animate-spin" /> : <Check className="size-4" />}
                  Guardar en la tarea
                </Button>
              )}
            </div>
          </div>
        </div>
      )}

      {state === 'loading' && (
        <div className="pointer-events-none absolute inset-0 grid place-items-center bg-background/85 backdrop-blur-[1px]">
          <div className="text-muted-foreground flex flex-col items-center gap-2">
            <Loader2 className="size-5 animate-spin" />
            <span className="font-mono text-[11px] tracking-wide">{progress}</span>
          </div>
        </div>
      )}

      {state === 'error' && (
        <div className="absolute inset-0 grid place-items-center p-6">
          <div className="max-w-sm text-center">
            <AlertTriangle className="text-warning mx-auto mb-2 size-6" />
            <p className="text-sm font-medium">No pude abrir este plano</p>
            <p className="text-muted-foreground mt-1 font-mono text-[11px] leading-relaxed">
              {error}
            </p>
          </div>
        </div>
      )}
    </div>
  )
})
