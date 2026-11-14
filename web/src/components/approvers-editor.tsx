import { useCallback, useEffect, useState } from 'react'
import { ArrowDown, ArrowUp, Loader2, Plus, Save, ShieldCheck, Trash2 } from 'lucide-react'
import { toast } from 'sonner'
import { api, type Approver, type ApproversView } from '@/lib/api'
import { Button } from '@/components/ui/button'
import { Badge } from '@/components/ui/badge'
import { Input } from '@/components/ui/input'
import { Skeleton } from '@/components/ui/separator'
import { nameOf } from '@/components/approval-chain'

const EMAIL = /^[^@\s]+@[^@\s]+\.[^@\s]+$/

/**
 * Orden de aprobación del proyecto: la lista de arriba a abajo es el orden.
 * Una tarea solo se integra cuando todos aprobaron, cada uno después del anterior.
 */
export function ApproversEditor({ projectId, onSaved }: { projectId: string; onSaved?: () => void }) {
  const [view, setView] = useState<ApproversView | null>(null)
  const [list, setList] = useState<Approver[]>([])
  const [email, setEmail] = useState('')
  const [label, setLabel] = useState('')
  const [saving, setSaving] = useState(false)

  const load = useCallback(async () => {
    try {
      const v = await api.approvers(projectId)
      setView(v)
      setList(v.approvers.map(({ email, label }) => ({ email, label })))
    } catch (err) {
      toast.error(err instanceof Error ? err.message : 'no pude cargar los aprobadores')
    }
  }, [projectId])

  useEffect(() => {
    void load()
  }, [load])

  if (!view) return <Skeleton className="h-40 w-full" />

  const saved = view.approvers.map((a) => `${a.email}|${a.label}`).join(',')
  const dirty = list.map((a) => `${a.email}|${a.label}`).join(',') !== saved
  const canEdit = view.canEdit
  const normalized = email.trim().toLowerCase()
  const addError =
    normalized && !EMAIL.test(normalized)
      ? 'No parece un correo'
      : list.some((a) => a.email === normalized)
        ? 'Ya está en la lista'
        : ''

  const add = () => {
    if (!normalized || addError) return
    setList([...list, { email: normalized, label: label.trim() }])
    setEmail('')
    setLabel('')
  }
  const move = (i: number, d: -1 | 1) => {
    const next = [...list]
    const j = i + d
    if (j < 0 || j >= next.length) return
    ;[next[i], next[j]] = [next[j], next[i]]
    setList(next)
  }

  const save = async () => {
    setSaving(true)
    try {
      const v = await api.setApprovers(projectId, list)
      setView(v)
      setList(v.approvers.map(({ email, label }) => ({ email, label })))
      toast.success('Cadena de aprobación guardada')
      onSaved?.()
    } catch (err) {
      toast.error(err instanceof Error ? err.message : 'no pude guardar')
    } finally {
      setSaving(false)
    }
  }

  return (
    <div className="grid gap-4 lg:grid-cols-[1.4fr_1fr]">
      <div className="bg-card rounded-lg border shadow-xs">
        <div className="flex items-center justify-between border-b px-4 py-3">
          <div>
            <h3 className="text-[13.5px] font-semibold">Orden de aprobación</h3>
            <p className="text-muted-foreground mt-0.5 text-[12px]">
              De arriba hacia abajo. Todos deben aprobar cada cambio, en este orden.
            </p>
          </div>
          {canEdit && (
            <Button size="sm" onClick={save} disabled={!dirty || saving}>
              {saving ? <Loader2 className="size-4 animate-spin" /> : <Save className="size-4" />}
              Guardar
            </Button>
          )}
        </div>

        {list.length === 0 ? (
          <div className="text-muted-foreground px-4 py-8 text-center text-[13px]">
            <ShieldCheck className="mx-auto mb-2 size-6 opacity-50" />
            Sin aprobadores: las tareas se integran sin revisión.
            {canEdit && ' Agrega el primero abajo.'}
          </div>
        ) : (
          <ol className="divide-y">
            {list.map((a, i) => (
              <li key={a.email} className="flex items-center gap-3 px-4 py-2.5">
                <span className="bg-muted text-muted-foreground tnum grid size-6 shrink-0 place-items-center rounded-full text-[11px] font-semibold">
                  {i + 1}
                </span>
                <div className="min-w-0 flex-1">
                  <div className="flex items-center gap-2">
                    <span className="text-[13px] font-medium">{nameOf(a.email)}</span>
                    {a.email === view.me.email && <Badge variant="default">Tú</Badge>}
                    {a.email === view.owner && <Badge variant="muted">Dueño</Badge>}
                  </div>
                  <div className="text-muted-foreground truncate text-[11.5px]">{a.email}</div>
                </div>
                {canEdit ? (
                  <Input
                    value={a.label}
                    maxLength={40}
                    placeholder="Rol (opcional)"
                    onChange={(e) => setList(list.map((x, k) => (k === i ? { ...x, label: e.target.value } : x)))}
                    className="h-8 w-36 text-[12.5px]"
                  />
                ) : (
                  a.label && <Badge variant="outline">{a.label}</Badge>
                )}
                {canEdit && (
                  <div className="flex shrink-0 items-center">
                    <Button variant="ghost" size="icon-sm" disabled={i === 0} onClick={() => move(i, -1)} title="Subir">
                      <ArrowUp className="size-4" />
                    </Button>
                    <Button variant="ghost" size="icon-sm" disabled={i === list.length - 1} onClick={() => move(i, 1)} title="Bajar">
                      <ArrowDown className="size-4" />
                    </Button>
                    <Button
                      variant="ghost"
                      size="icon-sm"
                      className="text-muted-foreground hover:text-destructive"
                      onClick={() => setList(list.filter((_, k) => k !== i))}
                      title="Quitar"
                    >
                      <Trash2 className="size-4" />
                    </Button>
                  </div>
                )}
              </li>
            ))}
          </ol>
        )}

        {canEdit && (
          <div className="bg-muted/30 flex flex-wrap items-start gap-2 border-t px-4 py-3">
            <div className="grid min-w-52 flex-1 gap-1">
              <Input
                value={email}
                onChange={(e) => setEmail(e.target.value)}
                onKeyDown={(e) => e.key === 'Enter' && add()}
                placeholder="correo@empresa.com"
                aria-invalid={!!addError}
              />
              {addError && <span className="text-destructive text-[11.5px]">{addError}</span>}
            </div>
            <Input
              value={label}
              maxLength={40}
              onChange={(e) => setLabel(e.target.value)}
              onKeyDown={(e) => e.key === 'Enter' && add()}
              placeholder="Rol (Seguridad, Producción…)"
              className="w-52"
            />
            <Button variant="outline" onClick={add} disabled={!normalized || !!addError}>
              <Plus className="size-4" /> Agregar
            </Button>
          </div>
        )}
      </div>

      <div className="space-y-3 text-[12.5px] leading-relaxed">
        <div className="bg-card rounded-lg border p-4">
          <h3 className="mb-1.5 text-[13px] font-semibold">Cómo funciona</h3>
          <ul className="text-muted-foreground list-disc space-y-1.5 pl-4">
            <li>Cada nodo decide solo cuando aprobó el anterior.</li>
            <li>
              Una aprobación vale para el contenido exacto de los planos que se revisó. Si se sube
              otra versión, <strong className="text-foreground">todas las aprobaciones se reinician</strong>.
            </li>
            <li>Un rechazo frena la cadena hasta que se suba una versión nueva.</li>
            <li>No se puede integrar una tarea hasta que apruebe el último nodo.</li>
          </ul>
        </div>
        <div className="bg-card rounded-lg border p-4">
          <h3 className="mb-1.5 text-[13px] font-semibold">Quién puede cambiar la cadena</h3>
          <p className="text-muted-foreground">
            {view.owner ? (
              <>
                El dueño del proyecto (<span className="font-mono text-[11.5px]">{view.owner}</span>) y
                cualquiera de los aprobadores.
              </>
            ) : (
              'Cualquiera, hasta que se defina el primer aprobador.'
            )}{' '}
            {!canEdit && <strong className="text-foreground">Tú no puedes editarla.</strong>}
          </p>
        </div>
      </div>
    </div>
  )
}
