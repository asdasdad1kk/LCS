import { useEffect, useMemo, useState } from 'react'
import {
  Crosshair,
  FileText,
  Layers,
  Loader2,
  MapPin,
  Minus,
  PencilRuler,
  Plus,
  RefreshCw,
  ScanSearch,
  Type
} from 'lucide-react'
import { Button } from '@/components/ui/button'
import { Badge } from '@/components/ui/badge'
import { api, type AnalysisRow } from '@/lib/api'
import type { AnalysisDoc, ChangeItem, DrawingProps, Region } from '@/lib/dwg-analyze'
import { formatDate, timeAgo } from '@/lib/format'
import { cn } from '@/lib/utils'

type Bounds = { minX: number; minY: number; maxX: number; maxY: number }

const KIND = {
  added: { label: 'Agregada', tone: 'bg-success/15 text-success', icon: Plus },
  removed: { label: 'Eliminada', tone: 'bg-destructive/15 text-destructive', icon: Minus },
  modified: { label: 'Modificada', tone: 'bg-warning/20 text-warning', icon: PencilRuler }
} as const

const UNITS: Record<number, string> = {
  0: 'Sin unidades',
  1: 'Pulgadas',
  2: 'Pies',
  3: 'Millas',
  4: 'Milímetros',
  5: 'Centímetros',
  6: 'Metros',
  7: 'Kilómetros',
  14: 'Decímetros'
}

// Colores ACI más comunes; el resto se reparte por tono.
const ACI = ['#888', '#ff3b30', '#ffd60a', '#34c759', '#32ade6', '#0a84ff', '#bf5af2', '#e5e5ea', '#8e8e93', '#c7c7cc']
const aci = (n: number) => ACI[n] ?? `hsl(${(n * 37) % 360} 70% 55%)`

function Stat({ label, value, tone }: { label: string; value: number; tone: string }) {
  return (
    <div className="bg-card min-w-28 flex-1 rounded-lg border px-3.5 py-2.5">
      <div className={cn('text-[22px] leading-none font-semibold tabular-nums', tone)}>{value}</div>
      <div className="text-muted-foreground mt-1 text-[11.5px]">{label}</div>
    </div>
  )
}

const bar = (n: number, max: number) => ({ width: `${Math.max(3, (n / Math.max(max, 1)) * 100)}%` })

type SubTab = 'zones' | 'texts' | 'items' | 'layers' | 'file'

export function ChangesPanel({
  branchId,
  analyses,
  filePaths,
  selectedPath,
  onSelectPath,
  running,
  canRun,
  onRun,
  onFocus
}: {
  branchId: string
  analyses: AnalysisRow[]
  filePaths: string[]
  selectedPath: string
  onSelectPath: (p: string) => void
  /** progreso del análisis en curso (null si no hay) */
  running: { path: string; msg: string } | null
  canRun: boolean
  onRun: (path: string) => void
  /** enfocar una zona en la comparación */
  onFocus: (b: Bounds, label?: string) => void
}) {
  const mine = analyses.filter((a) => a.path === selectedPath)
  const latest = mine[mine.length - 1]
  const [doc, setDoc] = useState<AnalysisDoc | null>(null)
  const [loadErr, setLoadErr] = useState('')
  const [tab, setTab] = useState<SubTab>('zones')

  useEffect(() => {
    setDoc(null)
    setLoadErr('')
    if (!latest) return
    let live = true
    api
      .analysisDoc(branchId, latest.id)
      .then((d) => live && setDoc(d))
      .catch((e) => live && setLoadErr(e instanceof Error ? e.message : 'no pude leer el análisis'))
    return () => {
      live = false
    }
  }, [branchId, latest?.id])

  const isRunning = running?.path === selectedPath
  const s = latest?.summary

  return (
    <div className="space-y-3">
      <div className="flex flex-wrap items-center gap-2">
        {filePaths.length > 1 && (
          <select
            value={selectedPath}
            onChange={(e) => onSelectPath(e.target.value)}
            className="bg-background h-8 rounded-md border px-2 text-[12.5px]"
          >
            {filePaths.map((p) => (
              <option key={p} value={p}>
                {p.split('/').pop()}
              </option>
            ))}
          </select>
        )}
        <Button size="sm" variant={latest ? 'outline' : 'default'} disabled={!canRun || isRunning} onClick={() => onRun(selectedPath)}>
          {isRunning ? <Loader2 className="size-4 animate-spin" /> : latest ? <RefreshCw className="size-4" /> : <ScanSearch className="size-4" />}
          {isRunning ? running?.msg : latest ? 'Volver a analizar' : 'Detectar cambios'}
        </Button>
        {latest && (
          <span className="text-muted-foreground text-[11.5px]" title={formatDate(latest.at)}>
            analizado {timeAgo(latest.at)} por {latest.author || '—'}
            {s?.match === 'content' && ' · emparejado por contenido'}
          </span>
        )}
      </div>

      {!latest && !isRunning && (
        <div className="text-muted-foreground rounded-lg border border-dashed px-6 py-12 text-center text-[13px]">
          <ScanSearch className="mx-auto mb-2 size-5 opacity-60" />
          LCS lee las propiedades de las dos versiones del DWG (entidades, capas, textos, cajetín) y registra
          qué cambió y dónde. Se hace solo al subir una versión; también puedes lanzarlo aquí.
        </div>
      )}

      {!latest && isRunning && (
        <div className="text-muted-foreground flex items-center justify-center gap-2 rounded-lg border border-dashed px-6 py-12 text-[13px]">
          <Loader2 className="size-4 animate-spin" /> {running?.msg}
        </div>
      )}

      {latest && s && (
        <>
          <div className="flex flex-wrap gap-2">
            <Stat label="Agregadas" value={s.added} tone="text-success" />
            <Stat label="Eliminadas" value={s.removed} tone="text-destructive" />
            <Stat label="Modificadas" value={s.modified} tone="text-warning" />
            <Stat label="Textos" value={s.texts} tone="text-foreground" />
            <Stat label="Zonas" value={s.regions} tone="text-foreground" />
          </div>
          {s.total === 0 && (
            <p className="text-muted-foreground bg-muted/50 rounded-md px-3 py-2 text-[12.5px]">
              No se detectaron diferencias de geometría, textos ni capas entre las dos versiones.
            </p>
          )}

          <div className="bg-muted inline-flex flex-wrap rounded-md p-0.5">
            {(
              [
                ['zones', 'Zonas', MapPin],
                ['texts', 'Textos', Type],
                ['items', 'Entidades', PencilRuler],
                ['layers', 'Capas y bloques', Layers],
                ['file', 'Archivo', FileText]
              ] as const
            ).map(([k, label, Icon]) => (
              <button
                key={k}
                type="button"
                onClick={() => setTab(k)}
                className={cn(
                  'inline-flex h-7 items-center gap-1.5 rounded-sm px-2.5 text-[12.5px] font-medium transition-all',
                  tab === k ? 'bg-card text-foreground shadow-xs' : 'text-muted-foreground'
                )}
              >
                <Icon className="size-3.5" /> {label}
              </button>
            ))}
          </div>

          {loadErr && <p className="text-destructive text-[12.5px]">{loadErr}</p>}
          {!doc && !loadErr && (
            <div className="text-muted-foreground flex items-center gap-2 py-6 text-[12.5px]">
              <Loader2 className="size-4 animate-spin" /> cargando el detalle…
            </div>
          )}
          {doc && (
            <>
              {tab === 'zones' && <Zones regions={doc.changes.regions} onFocus={onFocus} />}
              {tab === 'texts' && <Texts doc={doc} onFocus={onFocus} />}
              {tab === 'items' && <Items doc={doc} onFocus={onFocus} />}
              {tab === 'layers' && <LayersAndBlocks doc={doc} />}
              {tab === 'file' && <FileProps doc={doc} />}
            </>
          )}
        </>
      )}
    </div>
  )
}

// ---------------------------------------------------------------- zonas

function Zones({ regions, onFocus }: { regions: Region[]; onFocus: (b: Bounds, label?: string) => void }) {
  if (!regions.length)
    return <p className="text-muted-foreground py-6 text-center text-[12.5px]">Sin zonas con coordenadas.</p>
  return (
    <div className="divide-y overflow-hidden rounded-lg border">
      {regions.map((r, i) => (
        <div key={i} className="bg-card flex flex-wrap items-center gap-x-3 gap-y-1.5 px-3.5 py-2.5">
          <span className="bg-primary/10 text-primary grid size-6 shrink-0 place-items-center rounded-full text-[11px] font-semibold">
            {i + 1}
          </span>
          <div className="min-w-0 flex-1 basis-56">
            <div className="flex flex-wrap items-center gap-1.5 text-[12.5px]">
              <span className="font-medium">{r.space}</span>
              {r.added > 0 && <span className="bg-success/15 text-success rounded px-1.5 py-0.5 text-[11px]">+{r.added}</span>}
              {r.removed > 0 && <span className="bg-destructive/15 text-destructive rounded px-1.5 py-0.5 text-[11px]">−{r.removed}</span>}
              {r.modified > 0 && <span className="bg-warning/20 text-warning rounded px-1.5 py-0.5 text-[11px]">~{r.modified}</span>}
            </div>
            <div className="text-muted-foreground truncate text-[11.5px]">
              {r.layers.join(' · ')} · X {Math.round(r.bounds.minX)}–{Math.round(r.bounds.maxX)} · Y {Math.round(r.bounds.minY)}–{Math.round(r.bounds.maxY)}
            </div>
          </div>
          <Button size="xs" variant="outline" onClick={() => onFocus(r.bounds, `Zona ${i + 1}`)}>
            <Crosshair className="size-3" /> Ver en el plano
          </Button>
        </div>
      ))}
    </div>
  )
}

// ---------------------------------------------------------------- textos

function Texts({ doc, onFocus }: { doc: AnalysisDoc; onFocus: (b: Bounds, label?: string) => void }) {
  const list = doc.changes.texts
  const attribs = doc.changes.attribs
  if (!list.length && !attribs.length)
    return <p className="text-muted-foreground py-6 text-center text-[12.5px]">Los textos no cambiaron.</p>
  return (
    <div className="space-y-3">
      {attribs.length > 0 && (
        <div>
          <h4 className="text-muted-foreground mb-1.5 text-[11px] font-semibold tracking-wider uppercase">Cajetín y atributos</h4>
          <div className="divide-y overflow-hidden rounded-lg border">
            {attribs.map((a, i) => (
              <div key={i} className="bg-card grid grid-cols-[8rem_1fr] items-center gap-3 px-3.5 py-2 text-[12.5px]">
                <span className="text-muted-foreground truncate" title={`${a.block} · ${a.tag}`}>
                  {a.tag}
                </span>
                <span>
                  {a.from !== undefined && <span className="text-destructive line-through">{a.from || '∅'}</span>}
                  {a.from !== undefined && a.to !== undefined && ' → '}
                  {a.to !== undefined && <span className="text-success font-medium">{a.to || '∅'}</span>}
                </span>
              </div>
            ))}
          </div>
        </div>
      )}
      {list.length > 0 && (
        <div className="divide-y overflow-hidden rounded-lg border">
          {list.map((t, i) => (
            <div key={i} className="bg-card flex flex-wrap items-center gap-x-3 gap-y-1 px-3.5 py-2 text-[12.5px]">
              <div className="min-w-0 flex-1 basis-60">
                {t.from !== undefined && <span className="text-destructive line-through">{t.from || '∅'}</span>}
                {t.from !== undefined && t.to !== undefined && <span className="text-muted-foreground"> → </span>}
                {t.to !== undefined && <span className="text-success font-medium">{t.to || '∅'}</span>}
                <div className="text-muted-foreground text-[11px]">
                  {t.l} · {t.sp}
                </div>
              </div>
              {t.b && (
                <Button
                  size="xs"
                  variant="ghost"
                  onClick={() => onFocus({ minX: t.b![0], minY: t.b![1], maxX: t.b![2], maxY: t.b![3] }, 'Texto')}
                >
                  <Crosshair className="size-3" /> Ver
                </Button>
              )}
            </div>
          ))}
        </div>
      )}
    </div>
  )
}

// ---------------------------------------------------------------- entidades

function Items({ doc, onFocus }: { doc: AnalysisDoc; onFocus: (b: Bounds, label?: string) => void }) {
  const [kind, setKind] = useState<'all' | ChangeItem['k']>('all')
  const [q, setQ] = useState('')
  const [limit, setLimit] = useState(50)
  const c = doc.changes
  const filtered = useMemo(() => {
    const needle = q.trim().toLowerCase()
    return c.items.filter(
      (i) =>
        (kind === 'all' || i.k === kind) &&
        (!needle || i.t.toLowerCase().includes(needle) || i.l.toLowerCase().includes(needle) || (i.to ?? i.from ?? '').toLowerCase().includes(needle))
    )
  }, [c.items, kind, q])

  const types = Object.entries(c.byType).sort((a, b) => b[1].added + b[1].removed + b[1].modified - (a[1].added + a[1].removed + a[1].modified))
  const maxT = Math.max(1, ...types.map(([, v]) => v.added + v.removed + v.modified))

  return (
    <div className="space-y-3">
      <div className="grid gap-x-6 gap-y-1 sm:grid-cols-2">
        {types.slice(0, 8).map(([t, v]) => {
          const n = v.added + v.removed + v.modified
          return (
            <div key={t} className="flex items-center gap-2 text-[12px]">
              <span className="w-24 shrink-0 truncate font-mono text-[11px]">{t}</span>
              <div className="bg-muted h-2 flex-1 overflow-hidden rounded-full">
                <div className="bg-primary/70 h-full rounded-full" style={bar(n, maxT)} />
              </div>
              <span className="text-muted-foreground w-8 text-right tabular-nums">{n}</span>
            </div>
          )
        })}
      </div>

      <div className="flex flex-wrap items-center gap-2">
        {(['all', 'added', 'removed', 'modified'] as const).map((k) => (
          <button
            key={k}
            type="button"
            onClick={() => {
              setKind(k)
              setLimit(50)
            }}
            className={cn(
              'rounded-full border px-2.5 py-1 text-[12px] transition-colors',
              kind === k ? 'bg-primary text-primary-foreground border-primary' : 'bg-card hover:border-primary/50'
            )}
          >
            {k === 'all' ? 'Todas' : KIND[k].label + 's'}
          </button>
        ))}
        <input
          value={q}
          onChange={(e) => setQ(e.target.value)}
          placeholder="Filtrar por tipo, capa o texto"
          className="bg-background focus-visible:ring-ring ml-auto h-8 w-56 rounded-md border px-2.5 text-[12.5px] outline-none focus-visible:ring-2"
        />
      </div>

      <div className="divide-y overflow-hidden rounded-lg border">
        {filtered.slice(0, limit).map((i, idx) => {
          const K = KIND[i.k]
          return (
            <div key={`${i.h}-${idx}`} className="bg-card flex flex-wrap items-center gap-x-3 gap-y-1 px-3.5 py-2 text-[12.5px]">
              <span className={cn('inline-flex items-center gap-1 rounded px-1.5 py-0.5 text-[11px] font-medium', K.tone)}>
                <K.icon className="size-3" /> {K.label}
              </span>
              <span className="font-mono text-[11.5px]">{i.t}</span>
              <span className="text-muted-foreground">{i.l}</span>
              <span className="text-muted-foreground text-[11.5px]">{i.sp}</span>
              {i.what && <span className="text-muted-foreground text-[11.5px]">· {i.what.join(', ')}</span>}
              {(i.to ?? i.from) && <span className="max-w-60 truncate text-[11.5px]">«{i.to ?? i.from}»</span>}
              <span className="text-muted-foreground ml-auto font-mono text-[10.5px]">#{i.h}</span>
              {i.b && (
                <Button
                  size="xs"
                  variant="ghost"
                  onClick={() => onFocus({ minX: i.b![0], minY: i.b![1], maxX: i.b![2], maxY: i.b![3] }, i.t)}
                >
                  <Crosshair className="size-3" />
                </Button>
              )}
            </div>
          )
        })}
        {filtered.length === 0 && <p className="text-muted-foreground py-6 text-center text-[12.5px]">Nada con ese filtro.</p>}
      </div>
      {filtered.length > limit && (
        <Button size="sm" variant="outline" onClick={() => setLimit((l) => l + 100)}>
          Mostrar más ({filtered.length - limit} restantes)
        </Button>
      )}
      {c.itemsTruncated && (
        <p className="text-muted-foreground text-[11px]">
          Se guardan las primeras {c.items.length} entidades; los totales y las zonas cuentan todas.
        </p>
      )}
    </div>
  )
}

// ---------------------------------------------------------------- capas y bloques

function LayersAndBlocks({ doc }: { doc: AnalysisDoc }) {
  const c = doc.changes
  const rows: { tone: string; text: string }[] = []
  c.layers.added.forEach((n) => rows.push({ tone: 'text-success', text: `Capa nueva: ${n}` }))
  c.layers.removed.forEach((n) => rows.push({ tone: 'text-destructive', text: `Capa eliminada: ${n}` }))
  c.layers.changed.forEach((l) => rows.push({ tone: 'text-warning', text: `Capa ${l.name}: ${l.what.join(', ')}` }))
  c.layouts.added.forEach((n) => rows.push({ tone: 'text-success', text: `Layout nuevo: ${n}` }))
  c.layouts.removed.forEach((n) => rows.push({ tone: 'text-destructive', text: `Layout eliminado: ${n}` }))
  c.blocks.added.forEach((n) => rows.push({ tone: 'text-success', text: `Bloque nuevo: ${n}` }))
  c.blocks.removed.forEach((n) => rows.push({ tone: 'text-destructive', text: `Bloque eliminado: ${n}` }))
  c.blocks.redefined.forEach((n) => rows.push({ tone: 'text-warning', text: `Bloque redefinido: ${n}` }))

  const layers = Object.entries(c.byLayer).sort((a, b) => b[1].added + b[1].removed + b[1].modified - (a[1].added + a[1].removed + a[1].modified))
  const max = Math.max(1, ...layers.map(([, v]) => v.added + v.removed + v.modified))

  return (
    <div className="space-y-3">
      {rows.length > 0 && (
        <div className="divide-y overflow-hidden rounded-lg border">
          {rows.map((r, i) => (
            <div key={i} className={cn('bg-card px-3.5 py-2 text-[12.5px]', r.tone)}>
              {r.text}
            </div>
          ))}
        </div>
      )}
      <div>
        <h4 className="text-muted-foreground mb-1.5 text-[11px] font-semibold tracking-wider uppercase">Cambios por capa</h4>
        <div className="space-y-1">
          {layers.map(([l, v]) => (
            <div key={l} className="flex items-center gap-2 text-[12px]">
              <span className="w-40 shrink-0 truncate" title={l}>
                {l}
              </span>
              <div className="bg-muted flex h-2 flex-1 overflow-hidden rounded-full">
                <div className="bg-success h-full" style={bar(v.added, max)} />
                <div className="bg-destructive h-full" style={bar(v.removed, max)} />
                <div className="bg-warning h-full" style={bar(v.modified, max)} />
              </div>
              <span className="text-muted-foreground w-24 text-right tabular-nums">
                +{v.added} −{v.removed} ~{v.modified}
              </span>
            </div>
          ))}
          {layers.length === 0 && <p className="text-muted-foreground text-[12.5px]">Sin cambios por capa.</p>}
        </div>
      </div>
    </div>
  )
}

// ---------------------------------------------------------------- archivo

function FileProps({ doc }: { doc: AnalysisDoc }) {
  const p = doc.props
  const b = doc.baseProps
  const row = (label: string, now?: string, before?: string) => (
    <div className="grid grid-cols-[9rem_1fr] gap-3 px-3.5 py-2 text-[12.5px]">
      <span className="text-muted-foreground">{label}</span>
      <span>
        {before !== undefined && before !== now && (
          <>
            <span className="text-muted-foreground line-through">{before}</span> →{' '}
          </>
        )}
        <span className={before !== undefined && before !== now ? 'text-warning font-medium' : ''}>{now ?? '—'}</span>
      </span>
    </div>
  )
  const date = (iso?: string) => (iso ? formatDate(Date.parse(iso)) : undefined)
  const ext = (h: DrawingProps['header']) =>
    h.extMin && h.extMax ? `${Math.round(h.extMax[0] - h.extMin[0])} × ${Math.round(h.extMax[1] - h.extMin[1])}` : undefined
  const types = Object.entries(p.counts).sort((a, b) => b[1] - a[1])
  const maxT = Math.max(1, ...types.map(([, n]) => n))

  return (
    <div className="space-y-3">
      <div className="divide-y overflow-hidden rounded-lg border">
        {row('Versión de DWG', p.version || undefined, b?.version || undefined)}
        {row('Unidades', UNITS[p.header.units ?? -1] ?? (p.header.units !== undefined ? String(p.header.units) : undefined), b ? UNITS[b.header.units ?? -1] : undefined)}
        {row('Última modificación', date(p.header.updated), b ? date(b.header.updated) : undefined)}
        {row('Creado', date(p.header.created))}
        {row('Extensión del dibujo', ext(p.header), b ? ext(b.header) : undefined)}
        {row('Entidades', String(p.entities), b ? String(b.entities) : undefined)}
        {row('Layouts', p.layouts.join(', ') || undefined, b ? b.layouts.join(', ') : undefined)}
        {(p.unknown ?? 0) > 0 && row('No convertidas', `${p.unknown} (tipos que LibreDWG no lee)`)}
      </div>

      {p.attribs.length > 0 && (
        <div>
          <h4 className="text-muted-foreground mb-1.5 text-[11px] font-semibold tracking-wider uppercase">Cajetín y atributos</h4>
          <div className="grid gap-x-6 sm:grid-cols-2">
            {p.attribs.slice(0, 24).map((a, i) => (
              <div key={i} className="flex gap-2 border-b py-1 text-[12px]">
                <span className="text-muted-foreground w-28 shrink-0 truncate" title={`${a.block} · ${a.tag}`}>
                  {a.tag}
                </span>
                <span className="truncate">{a.value || '—'}</span>
              </div>
            ))}
          </div>
        </div>
      )}

      <div>
        <h4 className="text-muted-foreground mb-1.5 text-[11px] font-semibold tracking-wider uppercase">Capas ({p.layers.length})</h4>
        <div className="grid gap-x-6 sm:grid-cols-2">
          {p.layers.slice(0, 40).map((l) => (
            <div key={l.name} className="flex items-center gap-2 border-b py-1 text-[12px]">
              <span className="size-3 shrink-0 rounded-sm border" style={{ background: aci(l.color) }} />
              <span className="min-w-0 flex-1 truncate" title={l.name}>
                {l.name}
              </span>
              {(l.off || l.frozen || l.locked) && (
                <Badge variant="muted">{l.frozen ? 'congelada' : l.off ? 'apagada' : 'bloqueada'}</Badge>
              )}
              <span className="text-muted-foreground w-10 text-right tabular-nums">{l.n}</span>
            </div>
          ))}
        </div>
      </div>

      <div>
        <h4 className="text-muted-foreground mb-1.5 text-[11px] font-semibold tracking-wider uppercase">Entidades por tipo</h4>
        <div className="grid gap-x-6 gap-y-1 sm:grid-cols-2">
          {types.slice(0, 12).map(([t, n]) => (
            <div key={t} className="flex items-center gap-2 text-[12px]">
              <span className="w-28 shrink-0 truncate font-mono text-[11px]">{t}</span>
              <div className="bg-muted h-2 flex-1 overflow-hidden rounded-full">
                <div className="bg-primary/70 h-full rounded-full" style={bar(n, maxT)} />
              </div>
              <span className="text-muted-foreground w-12 text-right tabular-nums">{n}</span>
            </div>
          ))}
        </div>
      </div>
    </div>
  )
}
