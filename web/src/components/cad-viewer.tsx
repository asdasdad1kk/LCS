import { forwardRef, useEffect, useImperativeHandle, useRef, useState } from 'react'
import type { LayoutInfo } from '@/lib/api'
import { cn } from '@/lib/utils'
import { AlertTriangle, Loader2 } from 'lucide-react'

export type CadViewerHandle = {
  showLayout: (btrId: string) => void
  fit: () => void
}

type Props = {
  drawingId: string
  fileName: string
  refName?: string
  className?: string
  onLayouts?: (layouts: LayoutInfo[]) => void
  onLoaded?: () => void
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
  { drawingId, fileName, refName, className, onLayouts, onLoaded },
  ref
) {
  const hostRef = useRef<HTMLDivElement>(null)
  const managerRef = useRef<any>(null)
  const [state, setState] = useState<'loading' | 'ready' | 'error'>('loading')
  const [error, setError] = useState<string>('')
  const [progress, setProgress] = useState<string>('iniciando visor…')

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

  return (
    <div className={cn('lcs-viewer relative min-h-0 overflow-hidden bg-white dark:bg-zinc-900', className)}>
      <div ref={hostRef} className="absolute inset-0" />

      {state === 'loading' && (
        <div className="pointer-events-none absolute inset-0 grid place-items-center bg-white/85 backdrop-blur-[1px] dark:bg-zinc-900/85">
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
