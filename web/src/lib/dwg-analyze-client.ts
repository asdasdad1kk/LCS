import type { AnalysisDoc } from './dwg-analyze'

let seq = 0

async function fetchBuffer(url: string): Promise<ArrayBuffer> {
  const res = await fetch(url)
  if (!res.ok) throw new Error(`no pude descargar el plano (HTTP ${res.status})`)
  return res.arrayBuffer()
}

/**
 * Compara dos versiones de un plano: descarga ambas, las lee en un worker y
 * devuelve qué cambió y dónde. El worker se descarta al terminar para liberar
 * la memoria del WASM (un DWG grande ocupa varias veces su tamaño al leerse).
 */
export async function analyzeVersions(opts: {
  headUrl: string
  baseUrl?: string
  path: string
  commit: string
  baseRef: string
  onProgress?: (msg: string) => void
}): Promise<AnalysisDoc> {
  opts.onProgress?.('descargando…')
  const [head, base] = await Promise.all([fetchBuffer(opts.headUrl), opts.baseUrl ? fetchBuffer(opts.baseUrl) : Promise.resolve(undefined)])
  const worker = new Worker(new URL('./dwg-analyze.worker.ts', import.meta.url), { type: 'module' })
  const id = ++seq
  try {
    return await new Promise<AnalysisDoc>((resolve, reject) => {
      worker.onmessage = (ev: MessageEvent) => {
        const d = ev.data
        if (d.id !== id) return
        if (d.progress) opts.onProgress?.(d.progress)
        else if (d.error) reject(new Error(d.error))
        else resolve(d.doc as AnalysisDoc)
      }
      worker.onerror = (e) => reject(new Error(e.message || 'falló el análisis'))
      const transfer = base ? [head, base] : [head]
      worker.postMessage({ id, head, base, path: opts.path, commit: opts.commit, baseRef: opts.baseRef }, transfer)
    })
  } finally {
    worker.terminate()
  }
}
