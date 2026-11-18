/// <reference lib="webworker" />
import { LibreDwg, Dwg_File_Type } from '@mlightcad/libredwg-web'
import { diffSnapshots, propsOf, snapshotFromDb, summarize, type AnalysisDoc, type Snapshot } from './dwg-analyze'

/**
 * Analiza uno o dos DWG en un worker propio (el visor tiene el suyo, pero no
 * expone la base de datos completa). Mensajes:
 *   → { id, head: ArrayBuffer, base?: ArrayBuffer, path, commit, baseRef }
 *   ← { id, doc } | { id, error } | { id, progress }
 */

let lib: Promise<Awaited<ReturnType<typeof LibreDwg.create>>> | null = null
// Igual que el convertidor del visor (create() sin ruta); si el WASM no se
// resuelve por ahí, se pide a la carpeta pública.
const getLib = () =>
  (lib ??= LibreDwg.create().catch(() => LibreDwg.create('/workers')))

async function snapshotOf(buf: ArrayBuffer): Promise<Snapshot> {
  const dwg = await getLib()
  const data = dwg.dwg_read_data(buf, Dwg_File_Type.DWG)
  if (data === undefined || data === null) throw new Error('LibreDWG no pudo leer el archivo')
  try {
    let converted
    try {
      converted = dwg.convertEx(data)
    } catch {
      throw new Error('El archivo no es un DWG válido o LibreDWG no pudo convertirlo')
    }
    return snapshotFromDb(converted.database as never, converted.stats?.unknownEntityCount ?? 0)
  } finally {
    try {
      dwg.dwg_free(data)
    } catch {
      /* ya liberado */
    }
  }
}

self.onmessage = async (ev: MessageEvent) => {
  const { id, head, base, path, commit, baseRef } = ev.data as {
    id: number
    head: ArrayBuffer
    base?: ArrayBuffer
    path: string
    commit: string
    baseRef: string
  }
  const progress = (p: string) => (self as unknown as Worker).postMessage({ id, progress: p })
  try {
    progress('leyendo la versión nueva…')
    const headSnap = await snapshotOf(head)
    let baseSnap: Snapshot | undefined
    if (base) {
      progress('leyendo la versión base…')
      baseSnap = await snapshotOf(base)
    }
    progress('comparando…')
    const changes = diffSnapshots(baseSnap ?? { ...headSnap, ents: [], layers: [], layouts: [], blocks: [], attribs: [], counts: {} }, headSnap)
    const doc: AnalysisDoc = {
      v: 1,
      path,
      commit,
      base: baseRef,
      at: Date.now(),
      summary: summarize(changes),
      changes,
      props: propsOf(headSnap),
      baseProps: baseSnap ? propsOf(baseSnap) : undefined
    }
    ;(self as unknown as Worker).postMessage({ id, doc })
  } catch (err) {
    ;(self as unknown as Worker).postMessage({ id, error: err instanceof Error ? err.message : String(err) })
  }
}
