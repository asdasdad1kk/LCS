import { useRef, useState } from 'react'
import { FolderPlus, Loader2 } from 'lucide-react'
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
  onCreated
}: {
  open: boolean
  onOpenChange: (v: boolean) => void
  onCreated: (p: Project) => void
}) {
  const [name, setName] = useState('')
  const [description, setDescription] = useState('')
  const [busy, setBusy] = useState(false)
  const submitting = useRef(false)

  const submit = async () => {
    if (!name.trim() || submitting.current) return
    submitting.current = true
    setBusy(true)
    try {
      const p = await api.createProject({ name: name.trim(), description: description.trim() })
      toast.success('Proyecto creado', { description: p.name })
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
          <div className="text-muted-foreground mb-1 flex items-center gap-2 text-[11px] font-semibold tracking-[0.14em] uppercase">
            <FolderPlus className="size-3.5" /> nuevo repositorio
          </div>
          <DialogTitle>Crear proyecto</DialogTitle>
          <DialogDescription>
            Cada proyecto es un repositorio git en el NAS. Adentro viven los DWG y sus ramas de
            trabajo.
          </DialogDescription>
        </DialogHeader>

        <div className="grid gap-3 py-1">
          <div className="grid gap-1.5">
            <Label htmlFor="pname">Nombre</Label>
            <Input
              id="pname"
              autoFocus
              placeholder="Planta Norte — Obra 2026"
              value={name}
              onChange={(e) => setName(e.target.value)}
              onKeyDown={(e) => e.key === 'Enter' && submit()}
            />
          </div>
          <div className="grid gap-1.5">
            <Label htmlFor="pdesc">Descripción</Label>
            <Textarea
              id="pdesc"
              rows={2}
              placeholder="Set de planos de arquitectura, 3 niveles…"
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
            Crear proyecto
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
