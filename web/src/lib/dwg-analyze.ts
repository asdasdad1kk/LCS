/**
 * Lectura de las propiedades de un DWG y comparación entre dos versiones.
 *
 * Un DWG no es solo dibujo: guarda capas, layouts, bloques con atributos (el
 * cajetín), fechas, unidades y, sobre todo, un identificador (handle) estable
 * por entidad. Con eso se puede decir QUÉ cambió entre dos versiones (entidades
 * agregadas, eliminadas o modificadas, textos y capas) y DÓNDE (en coordenadas
 * del plano), sin depender de que alguien lo marque a mano.
 *
 * Este módulo es puro (sin DOM ni WASM): recibe la base de datos que entrega
 * libredwg-web y devuelve estructuras JSON. Se ejecuta en un worker.
 */

export type Bbox = [number, number, number, number] // minX, minY, maxX, maxY

export type EntRec = {
  /** handle: identificador estable de la entidad entre guardados */
  h: string
  /** tipo (LINE, TEXT, INSERT…) */
  t: string
  /** capa */
  l: string
  /** firma de contenido (geometría y propiedades, sin el handle) */
  s: string
  /** caja en coordenadas del plano */
  b?: Bbox
  /** texto visible, si tiene */
  tx?: string
  /** espacio: Modelo o el nombre del layout */
  sp: string
}

export type LayerRec = { name: string; color: number; off: boolean; frozen: boolean; locked: boolean }

export type AttribRec = { block: string; tag: string; value: string; h: string }

export type Snapshot = {
  version: string
  header: {
    created?: string
    updated?: string
    units?: number
    extMin?: [number, number]
    extMax?: [number, number]
    guid?: string
  }
  layers: LayerRec[]
  layouts: string[]
  blocks: { name: string; n: number; s: string }[]
  counts: Record<string, number>
  ents: EntRec[]
  /** atributos de bloques insertados: el cajetín (revisión, fecha, dibujó…) */
  attribs: AttribRec[]
  /** cuántas entidades no se pudieron leer (tipos que libredwg no convierte) */
  unknown?: number
}

// ---------------------------------------------------------------- utilidades

const SKIP_SIG = new Set([
  'handle',
  'ownerBlockRecordSoftId',
  'ownerHandle',
  'proxyEntity',
  'proxyByte',
  'xdata',
  'extendedData',
  'isInPaperSpace'
])

const round = (n: number) => Math.round(n * 1e4) / 1e4

/** JSON estable: claves ordenadas, números redondeados, sin campos que cambian sin que cambie el dibujo. */
function stable(v: unknown, depth = 0): string {
  if (v === null || v === undefined) return 'n'
  switch (typeof v) {
    case 'number':
      return Number.isFinite(v) ? String(round(v)) : 'x'
    case 'string':
      return JSON.stringify(v)
    case 'boolean':
      return v ? 't' : 'f'
    case 'object': {
      if (depth > 6) return '…'
      if (v instanceof Uint8Array || ArrayBuffer.isView(v)) return `b${(v as Uint8Array).length}`
      if (v instanceof Date) return String(v.getTime())
      if (Array.isArray(v)) return `[${v.map((x) => stable(x, depth + 1)).join(',')}]`
      const o = v as Record<string, unknown>
      const keys = Object.keys(o)
        .filter((k) => !SKIP_SIG.has(k))
        .sort()
      return `{${keys.map((k) => `${k}:${stable(o[k], depth + 1)}`).join(',')}}`
    }
    default:
      return ''
  }
}

/** FNV-1a de 32 bits dos veces con semillas distintas: 16 caracteres hex, suficiente para detectar cambios. */
export function hash(str: string): string {
  let a = 0x811c9dc5
  let b = 0x01000193 ^ str.length
  for (let i = 0; i < str.length; i++) {
    const c = str.charCodeAt(i)
    a ^= c
    a = Math.imul(a, 0x01000193)
    b ^= c + i
    b = Math.imul(b, 0x85ebca6b)
  }
  return (a >>> 0).toString(16).padStart(8, '0') + (b >>> 0).toString(16).padStart(8, '0')
}

const SKIP_POINTS = /direction|normal|extrusion|axis|vector|scale|rotation|offset/i

/** Caja de una entidad: todos los puntos {x, y} que contiene, más el radio o la altura del texto. */
function bboxOf(e: Record<string, any>): Bbox | undefined {
  let minX = Infinity
  let minY = Infinity
  let maxX = -Infinity
  let maxY = -Infinity
  const add = (x: number, y: number) => {
    if (!Number.isFinite(x) || !Number.isFinite(y)) return
    if (x < minX) minX = x
    if (x > maxX) maxX = x
    if (y < minY) minY = y
    if (y > maxY) maxY = y
  }
  let budget = 20000
  const walk = (v: any, depth: number) => {
    if (!v || typeof v !== 'object' || depth > 5 || budget-- < 0) return
    if (Array.isArray(v)) {
      for (const x of v) walk(x, depth + 1)
      return
    }
    if (typeof v.x === 'number' && typeof v.y === 'number') add(v.x, v.y)
    for (const k of Object.keys(v)) {
      if (SKIP_POINTS.test(k)) continue
      const c = v[k]
      if (c && typeof c === 'object') walk(c, depth + 1)
    }
  }
  walk(e, 0)
  if (!Number.isFinite(minX)) return undefined

  const r = typeof e.radius === 'number' ? Math.abs(e.radius) : 0
  const major = e.majorAxisEndPoint && typeof e.majorAxisEndPoint.x === 'number'
    ? Math.hypot(e.majorAxisEndPoint.x, e.majorAxisEndPoint.y) : 0
  const pad = Math.max(r, major)
  if (pad > 0) {
    minX -= pad
    minY -= pad
    maxX += pad
    maxY += pad
  }
  // Texto: la caja parte del punto de inserción y se extiende según la altura.
  const th = Number(e.textHeight ?? e.height ?? 0)
  const txt = textOf(e)
  if (th > 0 && txt) {
    const w = th * Math.max(1, Math.min(txt.length, 60)) * 0.6
    maxX = Math.max(maxX, minX + w)
    maxY = Math.max(maxY, minY + th)
  }
  return [round(minX), round(minY), round(maxX), round(maxY)]
}

function textOf(e: Record<string, any>): string | undefined {
  const t = e.text
  if (typeof t === 'string') return t
  if (t && typeof t === 'object' && typeof t.text === 'string') return t.text
  if (typeof e.textString === 'string') return e.textString
  return undefined
}

/** Quita los códigos de formato de MTEXT para comparar y mostrar el texto limpio. */
export function plainText(s: string): string {
  return s
    .replace(/\\P/g, ' ')
    .replace(/\\[A-Za-z][^;]*;/g, '')
    .replace(/[{}]/g, '')
    .replace(/\s+/g, ' ')
    .trim()
}

// ---------------------------------------------------------------- lectura

type Db = {
  header?: Record<string, any>
  tables?: Record<string, { entries?: any[] } | undefined>
  objects?: Record<string, any[] | undefined>
  entities?: any[]
}

export function snapshotFromDb(db: Db, unknown = 0): Snapshot {
  const hdr = db.header ?? {}
  const iso = (d: unknown) => (d instanceof Date && !Number.isNaN(d.getTime()) ? d.toISOString() : undefined)
  const pt = (p: any): [number, number] | undefined =>
    p && typeof p.x === 'number' && typeof p.y === 'number' ? [round(p.x), round(p.y)] : undefined

  const layers: LayerRec[] = (db.tables?.LAYER?.entries ?? []).map((l: any) => ({
    name: String(l.name ?? ''),
    color: Number(l.colorIndex ?? 7),
    off: !!l.off,
    frozen: !!l.frozen,
    locked: !!l.locked
  }))

  // Nombre del layout por el bloque de espacio papel que le corresponde.
  const layoutByBlock = new Map<string, string>()
  const layouts: string[] = []
  for (const lo of db.objects?.LAYOUT ?? []) {
    if (lo?.layoutName) {
      layouts.push(String(lo.layoutName))
      if (lo.paperSpaceTableId) layoutByBlock.set(String(lo.paperSpaceTableId), String(lo.layoutName))
    }
  }

  const ents: EntRec[] = []
  const counts: Record<string, number> = {}
  const attribs: AttribRec[] = []
  const blocks: Snapshot['blocks'] = []

  const push = (e: any, space: string) => {
    if (!e || typeof e !== 'object') return
    const t = String(e.type ?? '?')
    counts[t] = (counts[t] ?? 0) + 1
    const raw = textOf(e)
    const rec: EntRec = {
      h: String(e.handle ?? ''),
      t,
      l: String(e.layer ?? '0'),
      s: hash(stable(e)),
      b: bboxOf(e),
      sp: space
    }
    if (raw) rec.tx = plainText(raw)
    ents.push(rec)
    if (t === 'INSERT' && Array.isArray(e.attribs)) {
      for (const a of e.attribs) {
        const v = textOf(a)
        if (a?.tag && v !== undefined) attribs.push({ block: String(e.name ?? ''), tag: String(a.tag), value: plainText(v), h: String(e.handle ?? '') })
      }
    }
  }

  const model = db.entities ?? []
  for (const e of model) push(e, 'Modelo')

  for (const br of db.tables?.BLOCK_RECORD?.entries ?? []) {
    const name = String(br?.name ?? '')
    const list: any[] = Array.isArray(br?.entities) ? br.entities : []
    if (/^\*model_space$/i.test(name)) {
      if (!model.length) for (const e of list) push(e, 'Modelo') // algunos DWG no traen las entidades sueltas
      continue
    }
    const layout = layoutByBlock.get(String(br?.handle ?? ''))
    if (layout || /^\*paper_space/i.test(name)) {
      for (const e of list) push(e, layout ?? 'Papel')
      continue
    }
    if (!name.startsWith('*') && list.length) {
      // definición de bloque: se resume en una firma para saber si se redefinió
      blocks.push({ name, n: list.length, s: hash(list.map((e) => hash(stable(e))).sort().join('|')) })
    }
  }

  return {
    version: String(hdr.ACADVER ?? hdr.version ?? ''),
    header: {
      created: iso(hdr.TDCREATE),
      updated: iso(hdr.TDUPDATE),
      units: typeof hdr.INSUNITS === 'number' ? hdr.INSUNITS : undefined,
      extMin: pt(hdr.EXTMIN),
      extMax: pt(hdr.EXTMAX),
      guid: hdr.VERSIONGUID ? String(hdr.VERSIONGUID) : undefined
    },
    layers,
    layouts,
    blocks,
    counts,
    ents,
    attribs,
    unknown
  }
}

// ---------------------------------------------------------------- comparación

export type ChangeKind = 'added' | 'removed' | 'modified'

export type ChangeItem = {
  k: ChangeKind
  h: string
  t: string
  l: string
  sp: string
  b?: Bbox
  /** texto antes / después (si es un texto que cambió) */
  from?: string
  to?: string
  /** qué cambió en una entidad modificada */
  what?: string[]
}

export type Region = {
  bounds: { minX: number; minY: number; maxX: number; maxY: number }
  added: number
  removed: number
  modified: number
  layers: string[]
  /** el layout (o Modelo) donde está */
  space: string
}

export type ChangeSet = {
  /** cómo se emparejaron las entidades: por handle (lo normal) o por contenido */
  match: 'handle' | 'content'
  total: number
  added: number
  removed: number
  modified: number
  byType: Record<string, { added: number; removed: number; modified: number }>
  byLayer: Record<string, { added: number; removed: number; modified: number }>
  texts: { h: string; l: string; from?: string; to?: string; b?: Bbox; sp: string }[]
  layers: { added: string[]; removed: string[]; changed: { name: string; what: string[] }[] }
  layouts: { added: string[]; removed: string[] }
  blocks: { added: string[]; removed: string[]; redefined: string[] }
  attribs: { block: string; tag: string; from?: string; to?: string }[]
  meta: { field: string; from?: string; to?: string }[]
  items: ChangeItem[]
  itemsTruncated: boolean
  regions: Region[]
}

const MAX_ITEMS = 400

function bump(m: ChangeSet['byType'], key: string, k: ChangeKind) {
  const r = (m[key] ??= { added: 0, removed: 0, modified: 0 })
  r[k]++
}

/** Qué propiedades cambiaron entre dos versiones de una entidad (sin la firma completa: solo lo evidente). */
function what(a: EntRec, b: EntRec): string[] {
  const out: string[] = []
  if (a.l !== b.l) out.push(`capa ${a.l} → ${b.l}`)
  if (a.tx !== b.tx) out.push('texto')
  if (a.b && b.b) {
    const d = Math.max(...a.b.map((v, i) => Math.abs(v - b.b![i])))
    if (d > 1e-3) {
      const moved =
        Math.abs(a.b[2] - a.b[0] - (b.b[2] - b.b[0])) < 1e-3 && Math.abs(a.b[3] - a.b[1] - (b.b[3] - b.b[1])) < 1e-3
      out.push(moved ? 'movida' : 'geometría')
    }
  }
  if (!out.length) out.push('propiedades')
  return out
}

export function diffSnapshots(base: Snapshot, head: Snapshot): ChangeSet {
  const out: ChangeSet = {
    match: 'handle',
    total: 0,
    added: 0,
    removed: 0,
    modified: 0,
    byType: {},
    byLayer: {},
    texts: [],
    layers: { added: [], removed: [], changed: [] },
    layouts: { added: [], removed: [] },
    blocks: { added: [], removed: [], redefined: [] },
    attribs: [],
    meta: [],
    items: [],
    itemsTruncated: false,
    regions: []
  }

  // ---- entidades
  const items: ChangeItem[] = []
  const record = (it: ChangeItem) => {
    items.push(it)
    out[it.k]++
    bump(out.byType, it.t, it.k)
    bump(out.byLayer, it.l, it.k)
  }

  const baseByH = new Map(base.ents.map((e) => [e.h, e]))
  const matched = head.ents.reduce((n, e) => n + (baseByH.has(e.h) ? 1 : 0), 0)
  const ratio = head.ents.length ? matched / Math.max(head.ents.length, base.ents.length) : 1

  if (ratio >= 0.5 || (!base.ents.length && !head.ents.length)) {
    // El dibujo conserva los handles: comparación exacta.
    const seen = new Set<string>()
    for (const e of head.ents) {
      const a = baseByH.get(e.h)
      seen.add(e.h)
      if (!a) record({ k: 'added', h: e.h, t: e.t, l: e.l, sp: e.sp, b: e.b, to: e.tx })
      else if (a.s !== e.s || a.l !== e.l)
        record({ k: 'modified', h: e.h, t: e.t, l: e.l, sp: e.sp, b: e.b ?? a.b, from: a.tx, to: e.tx, what: what(a, e) })
    }
    for (const a of base.ents) {
      if (!seen.has(a.h)) record({ k: 'removed', h: a.h, t: a.t, l: a.l, sp: a.sp, b: a.b, from: a.tx })
    }
  } else {
    // Los handles no coinciden (archivo regenerado o reexportado): se empareja
    // por contenido; lo que no tiene pareja es agregado o eliminado.
    out.match = 'content'
    const pool = new Map<string, EntRec[]>()
    for (const a of base.ents) {
      const key = `${a.t}|${a.l}|${a.s}`
      const arr = pool.get(key)
      if (arr) arr.push(a)
      else pool.set(key, [a])
    }
    const leftHead: EntRec[] = []
    for (const e of head.ents) {
      const arr = pool.get(`${e.t}|${e.l}|${e.s}`)
      if (arr?.length) arr.pop()
      else leftHead.push(e)
    }
    const leftBase: EntRec[] = []
    for (const arr of pool.values()) leftBase.push(...arr)
    for (const e of leftHead) record({ k: 'added', h: e.h, t: e.t, l: e.l, sp: e.sp, b: e.b, to: e.tx })
    for (const a of leftBase) record({ k: 'removed', h: a.h, t: a.t, l: a.l, sp: a.sp, b: a.b, from: a.tx })
  }
  out.total = out.added + out.removed + out.modified

  // ---- textos que cambiaron
  for (const it of items) {
    const isText = it.from !== undefined || it.to !== undefined
    if (!isText) continue
    if (it.k === 'modified' && it.from === it.to) continue
    out.texts.push({ h: it.h, l: it.l, from: it.from, to: it.to, b: it.b, sp: it.sp })
  }

  // ---- capas
  const bl = new Map(base.layers.map((l) => [l.name, l]))
  const hl = new Map(head.layers.map((l) => [l.name, l]))
  for (const [n, l] of hl) {
    const a = bl.get(n)
    if (!a) out.layers.added.push(n)
    else {
      const w: string[] = []
      if (a.color !== l.color) w.push(`color ${a.color} → ${l.color}`)
      if (a.off !== l.off) w.push(l.off ? 'apagada' : 'encendida')
      if (a.frozen !== l.frozen) w.push(l.frozen ? 'congelada' : 'descongelada')
      if (a.locked !== l.locked) w.push(l.locked ? 'bloqueada' : 'desbloqueada')
      if (w.length) out.layers.changed.push({ name: n, what: w })
    }
  }
  for (const n of bl.keys()) if (!hl.has(n)) out.layers.removed.push(n)

  // ---- layouts y bloques
  const bLay = new Set(base.layouts)
  const hLay = new Set(head.layouts)
  out.layouts.added = [...hLay].filter((x) => !bLay.has(x))
  out.layouts.removed = [...bLay].filter((x) => !hLay.has(x))
  const bBlk = new Map(base.blocks.map((b) => [b.name, b]))
  const hBlk = new Map(head.blocks.map((b) => [b.name, b]))
  for (const [n, b] of hBlk) {
    const a = bBlk.get(n)
    if (!a) out.blocks.added.push(n)
    else if (a.s !== b.s) out.blocks.redefined.push(n)
  }
  for (const n of bBlk.keys()) if (!hBlk.has(n)) out.blocks.removed.push(n)

  // ---- atributos del cajetín
  const key = (a: AttribRec) => `${a.block}|${a.tag}`
  const ba = new Map(base.attribs.map((a) => [key(a), a.value]))
  const ha = new Map(head.attribs.map((a) => [key(a), a.value]))
  for (const [k, v] of ha) {
    if (ba.get(k) !== v) {
      const [block, tag] = k.split('|')
      out.attribs.push({ block, tag, from: ba.get(k), to: v })
    }
  }
  for (const [k, v] of ba) {
    if (!ha.has(k)) {
      const [block, tag] = k.split('|')
      out.attribs.push({ block, tag, from: v })
    }
  }

  // ---- propiedades del archivo
  const metaCmp = (field: string, a?: unknown, b?: unknown) => {
    const sa = a === undefined ? undefined : String(a)
    const sb = b === undefined ? undefined : String(b)
    if (sa !== sb) out.meta.push({ field, from: sa, to: sb })
  }
  metaCmp('Unidades', base.header.units, head.header.units)
  metaCmp('Versión de DWG', base.version, head.version)
  metaCmp('Extensión mínima', base.header.extMin?.join(', '), head.header.extMin?.join(', '))
  metaCmp('Extensión máxima', base.header.extMax?.join(', '), head.header.extMax?.join(', '))

  // ---- resultado acotado + regiones
  out.itemsTruncated = items.length > MAX_ITEMS
  out.items = items.slice(0, MAX_ITEMS)
  out.regions = clusterRegions(items, head, base)
  return out
}

// ---------------------------------------------------------------- regiones

/**
 * Agrupa los cambios cercanos en zonas rectangulares. Cada zona sirve para
 * "Enfocar cambios" y para dibujar un recuadro sobre el plano.
 */
export function clusterRegions(items: ChangeItem[], head: Snapshot, base: Snapshot): Region[] {
  const boxed = items.filter((i) => i.b)
  if (!boxed.length) return []

  // La distancia de fusión depende del tamaño del dibujo: ~3 % de su diagonal.
  const ext = extents([...head.ents, ...base.ents])
  const diag = ext ? Math.hypot(ext[2] - ext[0], ext[3] - ext[1]) : 1000
  const gap = Math.max(diag * 0.03, 1)

  type Cl = { b: Bbox; items: ChangeItem[]; sp: string }
  let clusters: Cl[] = []
  const bySpace = new Map<string, ChangeItem[]>()
  for (const i of boxed) {
    const arr = bySpace.get(i.sp)
    if (arr) arr.push(i)
    else bySpace.set(i.sp, [i])
  }
  for (const [sp, list] of bySpace) {
    // Sin superar 3000 cajas por espacio: el resto se agrupa grueso.
    const slice = list.length > 3000 ? list.slice(0, 3000) : list
    let cs: Cl[] = slice.map((i) => ({ b: [...i.b!] as Bbox, items: [i], sp }))
    let merged = true
    let guard = 0
    while (merged && guard++ < 40) {
      merged = false
      const next: Cl[] = []
      cs.sort((a, b) => a.b[0] - b.b[0])
      for (const c of cs) {
        const hit = next.find((n) => near(n.b, c.b, gap))
        if (hit) {
          hit.b = [Math.min(hit.b[0], c.b[0]), Math.min(hit.b[1], c.b[1]), Math.max(hit.b[2], c.b[2]), Math.max(hit.b[3], c.b[3])]
          hit.items.push(...c.items)
          merged = true
        } else next.push(c)
      }
      cs = next
    }
    clusters = clusters.concat(cs)
  }

  return clusters
    .sort((a, b) => b.items.length - a.items.length)
    .slice(0, 40)
    .map((c) => {
      const layerCount = new Map<string, number>()
      let added = 0
      let removed = 0
      let modified = 0
      for (const i of c.items) {
        layerCount.set(i.l, (layerCount.get(i.l) ?? 0) + 1)
        if (i.k === 'added') added++
        else if (i.k === 'removed') removed++
        else modified++
      }
      const m = Math.max(gap * 0.15, 0.5)
      return {
        bounds: { minX: c.b[0] - m, minY: c.b[1] - m, maxX: c.b[2] + m, maxY: c.b[3] + m },
        added,
        removed,
        modified,
        layers: [...layerCount.entries()].sort((a, b) => b[1] - a[1]).slice(0, 4).map(([l]) => l),
        space: c.sp
      }
    })
}

function near(a: Bbox, b: Bbox, gap: number) {
  return a[0] - gap <= b[2] && b[0] - gap <= a[2] && a[1] - gap <= b[3] && b[1] - gap <= a[3]
}

function extents(list: EntRec[]): Bbox | undefined {
  let minX = Infinity
  let minY = Infinity
  let maxX = -Infinity
  let maxY = -Infinity
  for (const e of list) {
    if (!e.b) continue
    minX = Math.min(minX, e.b[0])
    minY = Math.min(minY, e.b[1])
    maxX = Math.max(maxX, e.b[2])
    maxY = Math.max(maxY, e.b[3])
  }
  return Number.isFinite(minX) ? [minX, minY, maxX, maxY] : undefined
}

// ---------------------------------------------------------------- resúmenes

/** Lo que se guarda aparte y se muestra en listas: pequeño. */
export type AnalysisSummary = {
  added: number
  removed: number
  modified: number
  total: number
  texts: number
  regions: number
  layers: string[]
  /** capas con cambios, de más a menos */
  layersChanged: number
  match: 'handle' | 'content'
}

export function summarize(c: ChangeSet): AnalysisSummary {
  const layers = Object.entries(c.byLayer)
    .sort((a, b) => b[1].added + b[1].removed + b[1].modified - (a[1].added + a[1].removed + a[1].modified))
    .map(([l]) => l)
  return {
    added: c.added,
    removed: c.removed,
    modified: c.modified,
    total: c.total,
    texts: c.texts.length,
    regions: c.regions.length,
    layers: layers.slice(0, 5),
    layersChanged: layers.length + c.layers.added.length + c.layers.removed.length + c.layers.changed.length,
    match: c.match
  }
}

/** Propiedades de una versión para mostrar en el panel (sin la lista de entidades). */
export type DrawingProps = {
  version: string
  header: Snapshot['header']
  layers: (LayerRec & { n: number })[]
  layouts: string[]
  blocks: { name: string; n: number }[]
  counts: Record<string, number>
  attribs: AttribRec[]
  entities: number
  unknown?: number
}

export function propsOf(s: Snapshot): DrawingProps {
  const perLayer: Record<string, number> = {}
  for (const e of s.ents) perLayer[e.l] = (perLayer[e.l] ?? 0) + 1
  return {
    version: s.version,
    header: s.header,
    layers: s.layers.map((l) => ({ ...l, n: perLayer[l.name] ?? 0 })).sort((a, b) => b.n - a.n),
    layouts: s.layouts,
    blocks: s.blocks.map((b) => ({ name: b.name, n: b.n })).sort((a, b) => b.n - a.n).slice(0, 60),
    counts: s.counts,
    attribs: s.attribs.slice(0, 80),
    entities: s.ents.length,
    unknown: s.unknown
  }
}

/** Lo que guarda el servidor por cada análisis. */
export type AnalysisDoc = {
  v: 1
  path: string
  commit: string
  base: string
  at: number
  summary: AnalysisSummary
  changes: ChangeSet
  props: DrawingProps
  /** propiedades de la versión base, para comparar fechas, unidades, etc. */
  baseProps?: DrawingProps
}
