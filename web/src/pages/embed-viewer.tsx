import { useCallback, useEffect, useRef, useState } from 'react'
import { useSearchParams } from 'react-router-dom'
import { CadViewer, type Bounds, type CadViewerHandle } from '@/components/cad-viewer'

/**
 * Visor sin marco para incrustar en un iframe. El motor CAD es un singleton por
 * página, así que para ver dos versiones a la vez cada una vive en su propio
 * iframe; se hablan con el padre por postMessage:
 *
 *   hijo → padre  { lcs: 'camera', b }   la cámara se movió
 *   padre → hijo  { lcs: 'view',   b }   copiar esta vista (sincronía)
 *                 { lcs: 'focus',  b }   encuadrar esta zona
 */
export function EmbedViewerPage() {
  const [params] = useSearchParams()
  const drawingId = params.get('d') ?? ''
  const refName = params.get('ref') ?? undefined
  const name = params.get('n') ?? 'plano.dwg'
  const viewer = useRef<CadViewerHandle>(null)
  const quietUntil = useRef(0)
  const [ready, setReady] = useState(false)
  const pending = useRef<{ lcs: string; b: Bounds } | null>(null)

  const apply = useCallback((msg: { lcs: string; b: Bounds }) => {
    const v = viewer.current
    if (!v) {
      pending.current = msg
      return
    }
    // Mientras se aplica una vista ajena, no se devuelve el eco al padre.
    quietUntil.current = Date.now() + 700
    void v.focusBounds(msg.b, msg.lcs === 'view' ? 1.0 : 1.15)
  }, [])

  useEffect(() => {
    const onMessage = (e: MessageEvent) => {
      if (e.origin !== window.location.origin) return
      const d = e.data
      if (!d || typeof d !== 'object' || (d.lcs !== 'view' && d.lcs !== 'focus') || !d.b) return
      apply(d)
    }
    window.addEventListener('message', onMessage)
    return () => window.removeEventListener('message', onMessage)
  }, [apply])

  useEffect(() => {
    if (ready && pending.current) {
      apply(pending.current)
      pending.current = null
    }
  }, [ready, apply])

  const onCamera = useCallback((b: Bounds) => {
    if (Date.now() < quietUntil.current) return
    window.parent.postMessage({ lcs: 'camera', b }, window.location.origin)
  }, [])

  if (!drawingId) return null
  return (
    <div className="bg-background fixed inset-0">
      <CadViewer
        ref={viewer}
        drawingId={drawingId}
        fileName={name}
        refName={refName}
        className="h-full w-full"
        noCrop
        onCamera={onCamera}
        onLoaded={() => {
          setReady(true)
          window.parent.postMessage({ lcs: 'ready' }, window.location.origin)
        }}
      />
    </div>
  )
}
