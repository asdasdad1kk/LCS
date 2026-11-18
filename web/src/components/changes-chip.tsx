import { ScanSearch } from 'lucide-react'
import type { Changes } from '@/lib/api'
import { cn } from '@/lib/utils'

/** Cuántos cambios detectó el visor en una tarea, de un vistazo. */
export function ChangesChip({ changes, className }: { changes?: Changes; className?: string }) {
  if (!changes) return null
  const none = changes.total === 0
  return (
    <span
      className={cn(
        'inline-flex items-center gap-1.5 rounded-md px-2 py-0.5 text-[11.5px] font-medium',
        none ? 'bg-muted text-muted-foreground' : 'bg-violet-500/10 text-violet-600 dark:text-violet-300',
        className
      )}
      title={
        none
          ? 'No se detectaron diferencias entre la versión subida y la base'
          : `Capas: ${changes.layers.join(', ') || '—'}\n${changes.texts} texto(s) cambiados`
      }
    >
      <ScanSearch className="size-3" />
      {none ? (
        'sin diferencias'
      ) : (
        <>
          {changes.total} cambio{changes.total === 1 ? '' : 's'}
          <span className="text-success">+{changes.added}</span>
          <span className="text-destructive">−{changes.removed}</span>
          <span className="text-warning">~{changes.modified}</span>
        </>
      )}
    </span>
  )
}
