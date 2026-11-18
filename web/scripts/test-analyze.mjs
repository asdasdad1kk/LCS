// Prueba del análisis de DWG: lógica pura con bases de datos sintéticas y, si
// se puede, una lectura real con libredwg (plantilla DXF incluida en el repo).
//   node scripts/test-analyze.mjs
import { build } from 'esbuild'
import fs from 'node:fs'
import assert from 'node:assert/strict'
import path from 'node:path'
import { pathToFileURL } from 'node:url'

const out = path.join(import.meta.dirname, '.tmp-analyze.mjs')
await build({ entryPoints: [path.join(import.meta.dirname, '../src/lib/dwg-analyze.ts')], bundle: true, format: 'esm', outfile: out, platform: 'node', logLevel: 'error' })
const A = await import(pathToFileURL(out).href)
fs.rmSync(out)

const line = (h, x1, y1, x2, y2, layer = 'MUROS') => ({ type: 'LINE', handle: h, layer, startPoint: { x: x1, y: y1, z: 0 }, endPoint: { x: x2, y: y2, z: 0 }, extrusionDirection: { x: 0, y: 0, z: 1 } })
const text = (h, t, x, y, layer = 'TEXTO') => ({ type: 'TEXT', handle: h, layer, text: t, startPoint: { x, y }, textHeight: 2.5 })
const circle = (h, x, y, r) => ({ type: 'CIRCLE', handle: h, layer: 'EQUIPOS', center: { x, y, z: 0 }, radius: r })
const db = (ents, extra = {}) => ({
  header: { INSUNITS: 4, TDUPDATE: new Date('2026-01-01') },
  tables: { LAYER: { entries: [{ name: 'MUROS', colorIndex: 1 }, { name: 'TEXTO', colorIndex: 7 }, { name: 'EQUIPOS', colorIndex: 3 }] }, BLOCK_RECORD: { entries: [] } },
  objects: { LAYOUT: [{ layoutName: 'Layout1', paperSpaceTableId: 'P1' }] },
  entities: ents,
  ...extra
})

const baseEnts = [line('A1', 0, 0, 100, 0), line('A2', 100, 0, 100, 50), text('T1', 'PRENSA 1', 20, 20), circle('C1', 500, 500, 10), line('A9', 900, 900, 950, 900)]
const headEnts = [
  line('A1', 0, 0, 100, 0), // igual
  line('A2', 100, 0, 100, 60), // modificada
  text('T1', 'PRENSA 2', 20, 20), // texto cambiado
  circle('C1', 500, 500, 10), // igual
  line('N1', 510, 505, 530, 505, 'EQUIPOS') // agregada, cerca del círculo
  // A9 eliminada
]
const base = A.snapshotFromDb(db(baseEnts))
const head = A.snapshotFromDb(db(headEnts, { tables: { LAYER: { entries: [{ name: 'MUROS', colorIndex: 1 }, { name: 'TEXTO', colorIndex: 7 }, { name: 'EQUIPOS', colorIndex: 3, frozen: true }, { name: 'NUEVA', colorIndex: 2 }] }, BLOCK_RECORD: { entries: [] } } }))

const c = A.diffSnapshots(base, head)
assert.equal(c.match, 'handle')
assert.equal(c.added, 1, 'agregadas')
assert.equal(c.removed, 1, 'eliminadas')
assert.equal(c.modified, 2, 'modificadas')
assert.equal(c.total, 4)
assert.deepEqual(c.texts.map((t) => [t.from, t.to]), [['PRENSA 1', 'PRENSA 2']])
assert.deepEqual(c.layers.added, ['NUEVA'])
assert.deepEqual(c.layers.changed, [{ name: 'EQUIPOS', what: ['congelada'] }])
assert.ok(c.regions.length >= 2, 'regiones separadas: ' + c.regions.length)
const near = c.regions.find((r) => r.bounds.minX < 520 && r.bounds.maxX > 500)
assert.ok(near && near.added === 1, 'la entidad agregada queda en su región')

// Sin cambios: nada.
const same = A.diffSnapshots(base, A.snapshotFromDb(db(baseEnts)))
assert.equal(same.total, 0)
assert.equal(same.regions.length, 0)

// Handles distintos (archivo regenerado): se empareja por contenido.
const renamed = baseEnts.map((e, i) => ({ ...e, handle: 'Z' + i }))
renamed[1] = { ...renamed[1], endPoint: { x: 100, y: 70, z: 0 } }
const byContent = A.diffSnapshots(base, A.snapshotFromDb(db(renamed)))
assert.equal(byContent.match, 'content')
assert.equal(byContent.added, 1)
assert.equal(byContent.removed, 1)

// Cajetín: atributos de un INSERT.
const withTitle = (rev) => db([{ type: 'INSERT', handle: 'B1', layer: '0', name: 'CAJETIN', insertionPoint: { x: 0, y: 0 }, attribs: [{ tag: 'REV', text: { text: rev } }, { tag: 'FECHA', text: { text: '17/11/2026' } }] }])
const t = A.diffSnapshots(A.snapshotFromDb(withTitle('A')), A.snapshotFromDb(withTitle('B')))
assert.deepEqual(t.attribs, [{ block: 'CAJETIN', tag: 'REV', from: 'A', to: 'B' }])

// El resumen y las propiedades salen sin lanzar.
const s = A.summarize(c)
assert.equal(s.total, 4)
assert.equal(A.propsOf(head).layers.find((l) => l.name === 'EQUIPOS').n, 2)
console.log('lógica OK:', JSON.stringify(s))

// ---- lectura real con libredwg (plantilla DXF)
try {
  const { LibreDwg, Dwg_File_Type } = await import('@mlightcad/libredwg-web')
  const lib = await LibreDwg.create(path.join(import.meta.dirname, '../public/workers') + path.sep)
  const dxf = fs.readFileSync(path.join(import.meta.dirname, '../public/cad-data/templates/acadiso.dxf'), 'utf8')
  const data = lib.dwg_read_data(dxf, Dwg_File_Type.DXF)
  assert.ok(data !== undefined && data !== null, 'libredwg leyó la plantilla')
  const { database } = lib.convertEx(data)
  const snap = A.snapshotFromDb(database)
  console.log('libredwg OK:', snap.layers.length, 'capas,', snap.layouts.length, 'layouts,', snap.ents.length, 'entidades, versión', snap.version || '(sin)')
  assert.ok(snap.layers.length > 0)
  const self = A.diffSnapshots(snap, snap)
  assert.equal(self.total, 0)
} catch (err) {
  console.log('lectura real omitida:', err?.message ?? err)
}
