import { useCallback, useState, type ReactNode } from 'react'
import { Loader2, Merge } from 'lucide-react'
import { toast } from 'sonner'
import { ApiError, api } from '@/lib/api'
import { Button } from '@/components/ui/button'
import { MergeConflictDialog } from '@/components/dialogs'

/**
 * Integrar una zona = copiar sus planos vigentes dentro del master.
 * Si el master editó esos mismos archivos desde la última integración, el
 * servidor responde 409 y se pide decidir antes de pisarlos.
 */
export function useIntegrateZone(onDone: () => void | Promise<void>) {
  const [busyId, setBusyId] = useState<string | null>(null)
  const [conflict, setConflict] = useState<{ zoneId: string; zoneName: string; paths: string[] } | null>(null)

  const run = useCallback(
    async (zoneId: string, zoneName: string, strategy?: string) => {
      setBusyId(zoneId)
      try {
        const res = await api.integrateProject(zoneId, strategy)
        toast.success('Master actualizado', {
          description: `${zoneName}: ${res.paths.length} plano(s) copiados al master`
        })
        setConflict(null)
        await onDone()
      } catch (err) {
        if (err instanceof ApiError && err.status === 409) {
          setConflict({ zoneId, zoneName, paths: err.payload?.paths ?? [] })
        } else {
          toast.error(err instanceof Error ? err.message : 'no pude integrar la zona')
        }
      } finally {
        setBusyId(null)
      }
    },
    [onDone]
  )

  const dialog = (
    <MergeConflictDialog
      open={!!conflict}
      onOpenChange={(v) => !v && setConflict(null)}
      paths={conflict?.paths ?? []}
      busy={!!conflict && busyId === conflict.zoneId}
      title="El master editó estos planos"
      description={
        <>
          Alguien modificó dentro del master los mismos archivos de <strong>{conflict?.zoneName}</strong>{' '}
          desde la última integración. Si seguís, se reemplazan por la versión de la zona.
        </>
      }
      note={
        <>
          <strong>Usar la versión de la zona</strong> sobrescribe esos planos en el master. La versión
          anterior no se pierde: queda en el historial del master.
        </>
      }
      forceLabel="Usar la versión de la zona"
      onForce={() => conflict && run(conflict.zoneId, conflict.zoneName, 'take-branch')}
    />
  )

  return { integrate: run, busyId, dialog }
}

export function IntegrateButton({
  zoneId,
  zoneName,
  busy,
  onIntegrate,
  size = 'sm',
  variant = 'default',
  children
}: {
  zoneId: string
  zoneName: string
  busy: boolean
  onIntegrate: (zoneId: string, zoneName: string) => void
  size?: 'sm' | 'xs'
  variant?: 'default' | 'outline'
  children?: ReactNode
}) {
  return (
    <Button
      size={size}
      variant={variant}
      disabled={busy}
      onClick={(e) => {
        e.preventDefault()
        e.stopPropagation()
        onIntegrate(zoneId, zoneName)
      }}
    >
      {busy ? <Loader2 className="size-4 animate-spin" /> : <Merge className="size-4" />}
      {children ?? 'Actualizar master'}
    </Button>
  )
}
