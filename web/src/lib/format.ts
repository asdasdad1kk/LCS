export const PREVIEW_LIMIT = 32 * 1024 * 1024

export function formatBytes(bytes: number): string {  if (!bytes) return '0 B'
  const units = ['B', 'KB', 'MB', 'GB']
  const i = Math.min(Math.floor(Math.log(bytes) / Math.log(1024)), units.length - 1)
  const value = bytes / Math.pow(1024, i)
  return `${value.toFixed(value >= 10 || i === 0 ? 0 : 1)} ${units[i]}`
}

export function timeAgo(ms: number): string {
  if (!ms) return '—'
  const diff = Date.now() - ms
  const mins = Math.round(diff / 60000)
  if (mins < 1) return 'ahora'
  if (mins < 60) return `hace ${mins} min`
  const hours = Math.round(mins / 60)
  if (hours < 24) return `hace ${hours} h`
  const days = Math.round(hours / 24)
  if (days < 30) return `hace ${days} d`
  return new Date(ms).toLocaleDateString('es-AR')
}

export function formatDate(ms: number): string {
  if (!ms) return '—'
  return new Date(ms).toLocaleString('es-AR', {
    day: '2-digit',
    month: '2-digit',
    year: 'numeric',
    hour: '2-digit',
    minute: '2-digit'
  })
}

export function shortSha(sha: string | undefined): string {
  return sha ? sha.slice(0, 7) : '—'
}

export function fileName(path: string): string {
  return path.split('/').pop() ?? path
}

export function initials(name: string): string {
  return name
    .split(/[\s._-]+/)
    .filter(Boolean)
    .slice(0, 2)
    .map((p) => p[0]?.toUpperCase())
    .join('')
}

export function planos(n: number): string {
  return `${n} ${n === 1 ? 'plano' : 'planos'}`
}
