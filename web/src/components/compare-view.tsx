import { useCallback, useEffect, useRef, useState } from 'react'
import { ArrowLeftRight, Link2, Link2Off, Loader2, Target } from 'lucide-react'
import { Button } from '@/components/ui/button'
import { cn } from '@/lib/utils'
import { unionBounds, type Bounds } from '@/components/cad-viewer'

export type RefOption = { ref: string; label: string }

export type FocusArea = { id: string; label: string; bounds: Bounds }

function Side({
  title,
  value,
  options,
  onChange,
  src,
  frameRef,
  onLoad
}: {
  title: string
  value: string
  options: RefOption[]
  onChange: (ref: string) => void
  src: string
  frameRef: React.RefObject<HTMLIFrameElement | null>
  onLoad: () => void
}) {
  const [loaded, setLoaded] = useState(false)
  useEffect(() => setLoaded(false), [src])
  return (
    <div className="flex min-w-0 flex-1 flex-col overflow-hidden rounded-lg border">
      <div className="bg-muted/50 flex items-center gap-2 border-b px-3 py-1.5">
        <span className="text-muted-foreground text-[11px] font-semibold tracking-wider uppercase">{title}</span>
        <select
          value={value}
          onChange={(e) => onChange(e.target.value)}
          className="bg-background h-7 min-w-0 flex-1 rounded-md border px-2 text-[12.5px]"
        >
          {options.map((o) => (
            <option key={o.ref} value={o.ref}>
              {o.label}
            </option>
          ))}
        </select>
      </div>
      <div className="relative h-[560px] bg-white dark:bg-zinc-900">
        <iframe
          ref={frameRef}
          key={src}
          src={src}
          title={title}
          className="absolute inset-0 h-full w-full border-0"
          onLoad={() => {
            setLoaded(true)
            onLoad()
          }}
        />
        {!loaded && (
          <div className="text-muted-foreground pointer-events-none absolute inset-0 grid place-items-center">
            <Loader2 className="size-5 animate-spin" />
          </div>
        )}
      </div>
    </div>
  )
}

/**
 * Dos versiones del mismo plano lado a lado, con zoom y desplazamiento
 * sincronizados. "Enfocar cambios" lleva ambos visores a las coordenadas de los
 * recortes marcados.
 */
export function CompareView({
  drawingId,
  fileName,
  options,
  initialLeft,
  initialRight,
  areas,
  selectedArea
}: {
  drawingId: string
  fileName: string
  options: RefOption[]
  initialLeft: string
  initialRight: string
  areas: FocusArea[]
  /** Zona a enfocar al abrir (p. ej. al llegar desde un recorte). */
  selectedArea?: string | null
}) {
  const [left, setLeft] = useState(initialLeft)
  const [right, setRight] = useState(initialRight)
  const [sync, setSync] = useState(true)
  const leftFrame = useRef<HTMLIFrameElement>(null)
  const rightFrame = useRef<HTMLIFrameElement>(null)
  const readyCount = useRef(0)
  const wanted = useRef<Bounds | null>(null)

  const post = (frame: HTMLIFrameElement | null, msg: object) =>
    frame?.contentWindow?.postMessage(msg, window.location.origin)

  const focusOn = useCallback((b: Bounds) => {
    wanted.current = b
    post(leftFrame.current, { lcs: 'focus', b })
    post(rightFrame.current, { lcs: 'focus', b })
  }, [])

  // El visor de cada lado avisa cuando su plano terminó de cargar: ahí se
  // encuadra la zona pedida (si hay una).
  useEffect(() => {
    const onMessage = (e: MessageEvent) => {
      if (e.origin !== window.location.origin || !e.data || typeof e.data !== 'object') return
      const from = e.source
      if (e.data.lcs === 'ready') {
        if (wanted.current) post(from === leftFrame.current?.contentWindow ? leftFrame.current : rightFrame.current, { lcs: 'focus', b: wanted.current })
        return
      }
      if (e.data.lcs === 'camera' && sync && e.data.b) {
        if (from === leftFrame.current?.contentWindow) post(rightFrame.current, { lcs: 'view', b: e.data.b })
        else if (from === rightFrame.current?.contentWindow) post(leftFrame.current, { lcs: 'view', b: e.data.b })
      }
    }
    window.addEventListener('message', onMessage)
    return () => window.removeEventListener('message', onMessage)
  }, [sync])

  // Zona pedida desde fuera (recorte elegido en la galería).
  useEffect(() => {
    const a = areas.find((x) => x.id === selectedArea)
    if (a) wanted.current = a.bounds
  }, [selectedArea, areas])

  const src = (ref: string) =>
    `/embed/viewer?d=${encodeURIComponent(drawingId)}&ref=${encodeURIComponent(ref)}&n=${encodeURIComponent(fileName)}`

  const all = unionBounds(areas.map((a) => a.bounds))

  return (
    <div className="space-y-2.5">
      <div className="flex flex-wrap items-center gap-2">
        <Button
          size="sm"
          variant="outline"
          disabled={!all}
          title={all ? 'Ir a las zonas marcadas con recortes' : 'Marca una zona con el botón de recortar en la vista previa'}
          onClick={() => all && focusOn(all)}
        >
          <Target className="size-4" /> Enfocar cambios
        </Button>
        {areas.map((a) => (
          <button
            key={a.id}
            type="button"
            onClick={() => focusOn(a.bounds)}
            className="bg-card hover:border-primary/60 max-w-48 truncate rounded-full border px-2.5 py-1 text-[11.5px] transition-colors"
            title={a.label}
          >
            {a.label}
          </button>
        ))}
        <div className="ml-auto flex items-center gap-1.5">
          <Button
            size="sm"
            variant="ghost"
            title="Intercambiar lados"
            onClick={() => {
              setLeft(right)
              setRight(left)
            }}
          >
            <ArrowLeftRight className="size-4" />
          </Button>
          <Button
            size="sm"
            variant={sync ? 'secondary' : 'outline'}
            onClick={() => setSync((s) => !s)}
            title="Mover y hacer zoom en los dos a la vez"
          >
            {sync ? <Link2 className="size-4" /> : <Link2Off className="size-4" />}
            {sync ? 'Sincronizados' : 'Independientes'}
          </Button>
        </div>
      </div>

      <div className={cn('flex flex-col gap-3 lg:flex-row')}>
        <Side
          title="Antes"
          value={left}
          options={options}
          onChange={setLeft}
          src={src(left)}
          frameRef={leftFrame}
          onLoad={() => void readyCount.current++}
        />
        <Side
          title="Después"
          value={right}
          options={options}
          onChange={setRight}
          src={src(right)}
          frameRef={rightFrame}
          onLoad={() => void readyCount.current++}
        />
      </div>
    </div>
  )
}
