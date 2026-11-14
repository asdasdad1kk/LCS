import { useEffect, useRef, useState } from 'react'
import { FolderPlus, Loader2, Split } from 'lucide-react'
import { toast } from 'sonner'
import { api, type Project } from '@/lib/api'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle
} from '@/components/ui/dialog'
import { Button } from '@/components/ui/button'
import { Input, Textarea } from '@/components/ui/input'
import { Label } from '@/components/ui/label'

export function NewProjectDialog({
  open,
  onOpenChange,
  onCreated,
  masters = [],
  parentId = '',
  lockParent = false
}: {
  open: boolean
  onOpenChange: (v: boolean) => void
  onCreated: (p: Project) => void
  /** Proyectos raíz que pueden ser master de una zona. */
  masters?: Project[]
  /** Master preseleccionado: el proyecto nuevo nace como zona de ese master. */
  parentId?: string
  /** Impide cambiar el master (se abrió desde la página del propio master). */
  lockParent?: boolean
}) {
  const [name, setName] = useState('')
  const [description, setDescription] = useState('')
  const [parent, setParent] = useState(parentId)
  const [busy, setBusy] = useState(false)
  const submitting = useRef(false)

  useEffect(() => {
    if (open) setParent(parentId)
  }, [open, parentId])

  const isZone = parent !== ''
  const parentName = masters.find((m) => m.id === parent)?.name

  const submit = async () => {
    if (!name.trim() || submitting.current) return
    submitting.current = true
    setBusy(true)
    try {
      const p = await api.createProject({
        name: name.trim(),
        description: description.trim(),
        parentId: parent || undefined
      })
      toast.success(isZone ? 'Subproyecto creado' : 'Proyecto creado', { description: p.name })
      setName('')
      setDescription('')
      onOpenChange(false)
      onCreated(p)
    } catch (err) {
      toast.error(err instanceof Error ? err.message : 'no pude crear el proyecto')
    } finally {
      submitting.current = false
      setBusy(false)
    }
  }

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-w-md">
        <DialogHeader>
          <div className="text-muted-foreground mb-1 flex items-center gap-1.5 text-[11px] font-medium tracking-[0.06em] uppercase">
            {isZone ? <Split className="size-3.5" /> : <FolderPlus className="size-3.5" />}
            {isZone ? 'nueva zona' : 'nuevo repositorio'}
          </div>
          <DialogTitle>{isZone ? 'Crear subproyecto' : 'Crear proyecto'}</DialogTitle>
          <DialogDescription>
            {isZone ? (
              <>
                Una zona tiene su propio repositorio y su propio ciclo de tareas. Cuando cambia, LCS
                avisa que hay que actualizar <strong>{parentName ?? 'el master'}</strong>.
              </>
            ) : (
              'Cada proyecto es un repositorio git en el NAS. Adentro viven los DWG y sus ramas de trabajo.'
            )}
          </DialogDescription>
        </DialogHeader>

        <div className="grid gap-3 py-1">
          <div className="grid gap-1.5">
            <Label htmlFor="pname">Nombre</Label>
            <Input
              id="pname"
              autoFocus
              placeholder={isZone ? 'Zona Norte — Bloque A' : 'Planta Norte — Obra 2026'}
              value={name}
              onChange={(e) => setName(e.target.value)}
              onKeyDown={(e) => e.key === 'Enter' && submit()}
            />
          </div>
          {(masters.length > 0 || lockParent) && (
            <div className="grid gap-1.5">
              <Label htmlFor="pparent">Pertenece a</Label>
              <select
                id="pparent"
                value={parent}
                disabled={lockParent}
                onChange={(e) => setParent(e.target.value)}
                className="border-input bg-card focus-visible:border-ring focus-visible:ring-ring/30 h-9 w-full rounded-md border px-3 text-sm shadow-xs outline-none focus-visible:ring-2 disabled:opacity-60"
              >
                {!lockParent && <option value="">Ninguno (proyecto independiente / master)</option>}
                {masters.map((m) => (
                  <option key={m.id} value={m.id}>
                    {m.name}
                  </option>
                ))}
              </select>
            </div>
          )}
          <div className="grid gap-1.5">
            <Label htmlFor="pdesc">Descripción</Label>
            <Textarea
              id="pdesc"
              rows={2}
              placeholder={isZone ? 'Bloque A, niveles 1 a 3…' : 'Set de planos de arquitectura, 3 niveles…'}
              value={description}
              onChange={(e) => setDescription(e.target.value)}
            />
          </div>
        </div>

        <DialogFooter>
          <Button variant="ghost" onClick={() => onOpenChange(false)} disabled={busy}>
            Cancelar
          </Button>
          <Button onClick={submit} disabled={busy || !name.trim()}>
            {busy && <Loader2 className="size-4 animate-spin" />}
            {isZone ? 'Crear subproyecto' : 'Crear proyecto'}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
