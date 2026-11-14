/**
 * LCS build assets.
 *
 * 1. Copies the CAD web workers + LibreDWG wasm out of node_modules.
 * 2. Downloads a curated subset of the MLightCAD font catalog so drawings
 *    render with real SHX/TrueType text and the app never needs internet.
 *
 * Re-run with: npm run assets
 */
import { copyFileSync, existsSync, mkdirSync, readFileSync, writeFileSync, statSync } from 'node:fs'
import { dirname, join } from 'node:path'
import { createRequire } from 'node:module'
import { fileURLToPath } from 'node:url'

const require = createRequire(import.meta.url)
const root = join(dirname(fileURLToPath(import.meta.url)), '..')
const publicDir = join(root, 'public')
const workersDir = join(publicDir, 'workers')
const fontsDir = join(publicDir, 'cad-data', 'fonts')

const CDN = 'https://cdn.jsdelivr.net/gh/mlightcad/cad-data@main'

/** Fonts that cover latin drawings without pulling the whole CJK catalog. */
const CURATED = [
  'txt.shx',
  'simplex.shx',
  'romans.shx',
  'romand.shx',
  'romant.shx',
  'italic.shx',
  'isocp.shx',
  'isocp2.shx',
  'isocp3.shx',
  'isoc.shx',
  'isocpeur.shx',
  'amgdt.shx',
  'ltypeshp.shx',
  'arial.woff',
  'arialbd.woff',
  'ariali.woff',
  'tahoma.woff',
  'times.woff',
  'verdana.woff',
  'simsun.woff'
]

function resolvePackageRoot(name) {
  const entry = require.resolve(name)
  let dir = dirname(entry)
  for (let i = 0; i < 8; i++) {
    const pkg = join(dir, 'package.json')
    if (existsSync(pkg)) {
      try {
        if (JSON.parse(readFileSync(pkg, 'utf8')).name === name) return dir
      } catch {
        /* ignore */
      }
    }
    const parent = dirname(dir)
    if (parent === dir) break
    dir = parent
  }
  throw new Error(`No encuentro el paquete ${name}`)
}

function copyWorkers() {
  mkdirSync(workersDir, { recursive: true })
  const viewer = resolvePackageRoot('@mlightcad/cad-simple-viewer')
  const libredwg = resolvePackageRoot('@mlightcad/libredwg-converter')

  const copies = [
    [join(viewer, 'dist', 'mtext-renderer-worker.js'), join(workersDir, 'mtext-renderer-worker.js')],
    [join(libredwg, 'dist', 'libredwg-parser-worker.js'), join(workersDir, 'libredwg-parser-worker.js')],
    [join(libredwg, 'dist', 'libredwg-web.wasm'), join(workersDir, 'libredwg-web.wasm')]
  ]
  for (const [from, to] of copies) {
    if (!existsSync(from)) {
      console.warn(`[assets] falta ${from}`)
      continue
    }
    copyFileSync(from, to)
    console.log(`[assets] worker ${to} (${(statSync(to).size / 1024 / 1024).toFixed(1)} MB)`)
  }
}

async function copyFonts() {
  mkdirSync(fontsDir, { recursive: true })
  let catalog = []
  try {
    const res = await fetch(`${CDN}/fonts/fonts.json`)
    catalog = await res.json()
  } catch (err) {
    console.warn('[assets] sin catálogo de fuentes:', err.message)
    return
  }

  const byFile = new Map(catalog.map((f) => [String(f.file).toLowerCase(), f]))
  const shipped = []
  let bytes = 0

  for (const file of CURATED) {
    const entry = byFile.get(file.toLowerCase())
    const target = join(fontsDir, file)
    if (existsSync(target) && statSync(target).size > 0) {
      shipped.push(entry ?? { file, name: [file.replace(/\.[^.]+$/, '')] })
      bytes += statSync(target).size
      continue
    }
    try {
      const res = await fetch(`${CDN}/fonts/${encodeURIComponent(file)}`)
      if (!res.ok) continue
      const buf = Buffer.from(await res.arrayBuffer())
      writeFileSync(target, buf)
      shipped.push(entry ?? { file, name: [file.replace(/\.[^.]+$/, '')] })
      bytes += buf.length
    } catch {
      /* optional font */
    }
  }

  writeFileSync(join(fontsDir, 'fonts.json'), JSON.stringify(shipped, null, 2))
  console.log(`[assets] ${shipped.length} fuentes (${(bytes / 1024 / 1024).toFixed(1)} MB)`)
}

async function copyTemplate() {
  const tplDir = join(publicDir, 'cad-data', 'templates')
  mkdirSync(tplDir, { recursive: true })
  const target = join(tplDir, 'acadiso.dxf')
  if (existsSync(target)) return
  try {
    const res = await fetch(`${CDN}/templates/acadiso.dxf`)
    if (res.ok) writeFileSync(target, Buffer.from(await res.arrayBuffer()))
  } catch {
    /* optional */
  }
}

copyWorkers()
await copyFonts()
await copyTemplate()
