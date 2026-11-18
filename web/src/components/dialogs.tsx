import { useEffect, useMemo, useRef, useState } from 'react'
import { toast } from 'sonner'
import {
  AlertTriangle,
  Check,
  FileUp,
  GitBranch,
  Loader2,
  PencilLine,
  Upload,
  X
} from 'lucide-react'
import {
  api,
  importDrawings,
  uploadToBranch,
  type Branch,
  type Drawing,
  type Editor, type TaskState } from '@/lib/api'
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
import { Badge } from '@/components/ui/badge'
import { cn } from '@/lib/utils'
import { fileName, formatBytes } from '@/lib/format'

// ---------------------------------------------------------------- new task

export function NewTaskDialog({
  open,
  onOpenChange,
  projectId,
  projects,
  drawings,
  preselect,
  onCreated
}: {
  open: boolean
  onOpenChange: (v: boolean) => void
  projectId: string
  projects?: { id: string; name: string }[]
  drawings?: Drawing[]
  preselect?: string[]
  onCreated: (b: Branch) => void
}) {
  const [pid, setPid] = useState(projectId || projects?.[0]?.id || '')
  const [list, setList] = useState<Drawing[]>(drawings ?? [])
  const [picked, setPicked] = useState<string[]>(preselect ?? [])
  const [title, setTitle] = useState('')
  const [notes, setNotes] = useState('')
  const [busy, setBusy] = useState(false)

  useEffect(() => {
    if (projectId) setPid(projectId)
  }, [projectId])

  useEffect(() => {
    if (drawings) setList(drawings)
    else if (open && pid) {
      setPicked([])
      api
        .project(pid)
        .then((d) => setList(d.drawings))
        .catch(() => setList([]))
    }
  }, [drawings, open, pid])

  useEffect(() => {
    if (open && preselect) setPicked(preselect)
  }, [open, preselect])

  // Quién ya tiene tareas abiertas sobre estos planos (consulta la carpeta
  // compartida: puede ser alguien en otro equipo).
  const [editing, setEditing] = useState<Record<string, Editor[]>>({})
  useEffect(() => {
    if (!open || !pid) return
    let alive = true
    api
      .editing(pid)
      .then((e) => alive && setEditing(e ?? {}))
      .catch(() => alive && setEditing({}))
    return () => {
      alive = false
    }
  }, [open, pid])
  const conflicts = picked.filter((p) => (editing[p]?.length ?? 0) > 0)

  const toggle = (path: string) =>
    setPicked((p) => (p.includes(path) ? p.filter((x) => x !== path) : [...p, path]))

  const submit = async () => {
    if (!title.trim() || picked.length === 0 || !pid) return
    setBusy(true)
    try {
      const b = await api.createBranch(pid, {
        drawings: picked,
        title: title.trim(),
        notes: notes.trim()
      })
      toast.success('Tarea creada', { description: b.title })
      setTitle('')
      setNotes('')
      onOpenChange(false)
      onCreated(b)
    } catch (err) {
      toast.error(err instanceof Error ? err.message : 'no pude crear la tarea')
    } finally {
      setBusy(false)
    }
  }

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-w-xl">
        <DialogHeader>
          <div className="text-muted-foreground mb-1 flex items-center gap-2 text-[11px] font-semibold tracking-[0.14em] uppercase">
            <GitBranch className="size-3.5" /> nueva rama de trabajo
          </div>
          <DialogTitle>Abrir tarea</DialogTitle>
          <DialogDescription>
            Se crea una rama <span className="font-mono">work/…</span> aislada. El plano se descarga
            desde esa rama, se edita en AutoCAD y se vuelve a subir sin tocar main.
          </DialogDescription>
        </DialogHeader>

        <div className="grid gap-4">
          {projects && projects.length > 0 && (
            <div className="grid gap-1.5">
              <Label>Proyecto</Label>
              <div className="flex flex-wrap gap-1">
                {projects.map((p) => (
                  <button
                    key={p.id}
                    type="button"
                    onClick={() => setPid(p.id)}
                    className={cn(
                      'rounded-md border px-2.5 py-1 text-[12px] transition-colors',
                      pid === p.id
                        ? 'border-primary/40 bg-accent text-accent-foreground font-medium'
                        : 'text-muted-foreground hover:bg-muted/60'
                    )}
                  >
                    {p.name}
                  </button>
                ))}
              </div>
            </div>
          )}

          <div className="grid gap-1.5">
            <Label htmlFor="t-title">Qué hay que hacer</Label>
            <Input
              id="t-title"
              autoFocus
              placeholder="Actualizar layout planta baja — revisión municipal"
              value={title}
              onChange={(e) => setTitle(e.target.value)}
            />
          </div>

          <div className="grid gap-1.5">
            <Label>Planos incluidos · {picked.length}</Label>
            <div className="scrollbar-thin max-h-52 overflow-y-auto rounded-md border">
              {list.map((d) => {
                const active = picked.includes(d.path)
                return (
                  <button
                    key={d.path}
                    onClick={() => toggle(d.path)}
                    className={cn(
                      'flex w-full items-center gap-2.5 border-b px-3 py-2 text-left text-[13px] last:border-b-0',
                      active ? 'bg-accent/60' : 'hover:bg-muted/50'
                    )}
                  >
                    <span
                      className={cn(
                        'grid size-4 shrink-0 place-items-center rounded border',
                        active ? 'bg-primary border-primary text-primary-foreground' : ''
                      )}
                    >
                      {active && <Check className="size-3" />}
                    </span>
                    <span className="min-w-0 flex-1 truncate">{d.name}</span>
                    {(editing[d.path]?.length ?? 0) > 0 && (
                      <Badge variant="warning" className="shrink-0 gap-1">
                        <PencilLine /> {editing[d.path].map((e) => e.author).join(', ')}
                      </Badge>
                    )}
                    <span className="text-muted-foreground tnum shrink-0 text-[11.5px]">
                      {formatBytes(d.size)}
                    </span>
                  </button>
                )
              })}
              {list.length === 0 && (
                <div className="text-muted-foreground px-3 py-4 text-center text-xs">
                  Este proyecto todavía no tiene planos.
                </div>
              )}
            </div>
          </div>

          {conflicts.length > 0 && (
            <div className="border-warning/40 bg-warning/10 rounded-md border px-3 py-2.5 text-[12.5px] leading-relaxed">
              <div className="mb-1 flex items-center gap-1.5 font-semibold">
                <AlertTriangle className="text-warning size-4" /> Alguien ya está editando{' '}
                {conflicts.length === 1 ? 'este plano' : 'estos planos'}
              </div>
              <ul className="text-muted-foreground space-y-0.5">
                {conflicts.flatMap((p) =>
                  editing[p].map((e) => (
                    <li key={`${p}-${e.branchId}`}>
                      <span className="text-foreground font-medium">{fileName(p)}</span>: «{e.title}» de{' '}
                      {e.author}
                    </li>
                  ))
                )}
              </ul>
              <p className="text-muted-foreground mt-1.5">
                Dos ediciones del mismo DWG no se pueden fusionar: la que se integre después
                reemplaza a la otra. Conviene coordinarlo antes.
              </p>
            </div>
          )}

          <div className="grid gap-1.5">
            <Label htmlFor="t-notes">Notas para quien edita</Label>
            <Textarea
              id="t-notes"
              rows={2}
              placeholder="Solo tocar los layouts de planta baja. No mover cotas."
              value={notes}
              onChange={(e) => setNotes(e.target.value)}
            />
          </div>
        </div>

        <DialogFooter>
          <Button variant="ghost" onClick={() => onOpenChange(false)} disabled={busy}>
            Cancelar
          </Button>
          <Button onClick={submit} disabled={busy || !title.trim() || picked.length === 0}>
            {busy ? <Loader2 className="size-4 animate-spin" /> : <GitBranch className="size-4" />}
            Crear rama
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}

// ---------------------------------------------------------------- import

export function ImportDrawingsDialog({
  open,
  onOpenChange,
  projectId,
  onDone
}: {
  open: boolean
  onOpenChange: (v: boolean) => void
  projectId: string
  onDone: () => void
}) {
  const [files, setFiles] = useState<File[]>([])
  const [busy, setBusy] = useState(false)
  const [pct, setPct] = useState(0)
  const [dragging, setDragging] = useState(false)
  const inputRef = useRef<HTMLInputElement>(null)

  const addFiles = (list: FileList | null) => {
    if (!list) return
    setFiles((prev) => [...prev, ...Array.from(list).filter((f) => /\.dwg$/i.test(f.name))])
  }

  const submit = async () => {
    if (!files.length) return
    setBusy(true)
    setPct(0)
    try {
      const res = await importDrawings(projectId, files, setPct)
      toast.success('Planos importados', { description: `${res.count} archivo(s) a main` })
      setFiles([])
      onOpenChange(false)
      onDone()
    } catch (err) {
      toast.error(err instanceof Error ? err.message : 'falló la importación')
    } finally {
      setBusy(false)
    }
  }

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-w-lg">
        <DialogHeader>
          <DialogTitle>Importar planos</DialogTitle>
          <DialogDescription>
            Los DWG se commitean a <span className="font-mono">main</span>. Si el archivo ya existe
            con otro contenido, queda una revisión nueva.
          </DialogDescription>
        </DialogHeader>

        <div
          onDragOver={(e) => {
            e.preventDefault()
            setDragging(true)
          }}
          onDragLeave={() => setDragging(false)}
          onDrop={(e) => {
            e.preventDefault()
            setDragging(false)
            addFiles(e.dataTransfer.files)
          }}
          onClick={() => inputRef.current?.click()}
          className={cn(
            'grid cursor-pointer place-items-center gap-1 rounded-lg border border-dashed px-4 py-8 text-center transition-colors',
            dragging ? 'border-primary bg-accent/50' : 'hover:bg-muted/40'
          )}
        >
          <FileUp className="text-muted-foreground size-5" />
          <p className="text-[13px] font-medium">Arrastrá los DWG o hacé clic</p>
          <p className="text-muted-foreground text-[11.5px]">Solo archivos .dwg</p>
          <input
            ref={inputRef}
            type="file"
            accept=".dwg"
            multiple
            hidden
            onChange={(e) => addFiles(e.target.files)}
          />
        </div>

        {files.length > 0 && (
          <div className="scrollbar-thin max-h-40 overflow-y-auto rounded-md border">
            {files.map((f, i) => (
              <div key={i} className="flex items-center gap-2 border-b px-3 py-1.5 text-[12px] last:border-b-0">
                <span className="min-w-0 flex-1 truncate">{f.name}</span>
                <span className="text-muted-foreground tnum text-[11.5px]">{formatBytes(f.size)}</span>
                <button onClick={() => setFiles((p) => p.filter((_, j) => j !== i))}>
                  <X className="text-muted-foreground size-3.5" />
                </button>
              </div>
            ))}
          </div>
        )}

        {busy && (
          <div className="bg-muted h-1.5 overflow-hidden rounded-full">
            <div className="bg-primary h-full transition-all" style={{ width: `${pct}%` }} />
          </div>
        )}

        <DialogFooter>
          <Button variant="ghost" onClick={() => onOpenChange(false)} disabled={busy}>
            Cancelar
          </Button>
          <Button onClick={submit} disabled={busy || !files.length}>
            {busy ? <Loader2 className="size-4 animate-spin" /> : <Upload className="size-4" />}
            Importar {files.length || ''}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}

// ---------------------------------------------------------------- upload

export function UploadDialog({
  open,
  onOpenChange,
  branch,
  onDone
}: {
  open: boolean
  onOpenChange: (v: boolean) => void
  branch: Branch
  onDone: (state?: TaskState) => void
}) {
  const [file, setFile] = useState<File | null>(null)
  const [message, setMessage] = useState('')
  const [busy, setBusy] = useState(false)
  const [pct, setPct] = useState(0)
  const [dragging, setDragging] = useState(false)
  const inputRef = useRef<HTMLInputElement>(null)

  const expected = useMemo(() => {
    const files = branch.files ?? []
    if (files.length === 1) return fileName(files[0].path)
    return files.map((f) => fileName(f.path)).join(' · ')
  }, [branch])

  const submit = async () => {
    if (!file) return
    setBusy(true)
    setPct(0)
    try {
      const res = await uploadToBranch(branch.id, file, message, setPct)
      setFile(null)
      setMessage('')
      onOpenChange(false)
      // Primero se actualiza la pantalla con lo que respondió el servidor; el aviso va después.
      onDone(res.state)
      toast.success('Cambios subidos', { description: file.name })
    } catch (err) {
      toast.error(err instanceof Error ? err.message : 'falló la subida')
    } finally {
      setBusy(false)
    }
  }

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-w-lg">
        <DialogHeader>
          <DialogTitle>Subir cambios</DialogTitle>
          <DialogDescription>
            Subí el DWG editado. Se commitea en la rama{' '}
            <span className="font-mono">{branch.name}</span>.
          </DialogDescription>
        </DialogHeader>

        <div className="bg-muted/40 flex items-center gap-2 rounded-md border px-3 py-2">
          <span className="text-muted-foreground text-[11px] font-medium uppercase tracking-wide">
            Esperado
          </span>
          <span className="truncate font-mono text-[11px]">{expected}</span>
        </div>

        <div
          onDragOver={(e) => {
            e.preventDefault()
            setDragging(true)
          }}
          onDragLeave={() => setDragging(false)}
          onDrop={(e) => {
            e.preventDefault()
            setDragging(false)
            const f = e.dataTransfer.files?.[0]
            if (f && /\.dwg$/i.test(f.name)) setFile(f)
          }}
          onClick={() => inputRef.current?.click()}
          className={cn(
            'grid cursor-pointer place-items-center gap-1 rounded-lg border border-dashed px-4 py-8 text-center transition-colors',
            dragging ? 'border-primary bg-accent/50' : 'hover:bg-muted/40'
          )}
        >
          <Upload className="text-muted-foreground size-5" />
          {file ? (
            <>
              <p className="text-[13px] font-medium">{file.name}</p>
              <p className="text-muted-foreground tnum text-[11.5px]">{formatBytes(file.size)}</p>
            </>
          ) : (
            <>
              <p className="text-[13px] font-medium">Arrastrá el DWG editado</p>
              <p className="text-muted-foreground font-mono text-[10px]">mantené el mismo nombre</p>
            </>
          )}
          <input
            ref={inputRef}
            type="file"
            accept=".dwg"
            hidden
            onChange={(e) => {
              const f = e.target.files?.[0]
              if (f) setFile(f)
            }}
          />
        </div>

        <div className="grid gap-1.5">
          <Label htmlFor="up-msg">Comentario del cambio</Label>
          <Input
            id="up-msg"
            placeholder="Corregí cotas y cambié el rótulo"
            value={message}
            onChange={(e) => setMessage(e.target.value)}
          />
        </div>

        {busy && (
          <div className="bg-muted h-1.5 overflow-hidden rounded-full">
            <div className="bg-primary h-full transition-all" style={{ width: `${pct}%` }} />
          </div>
        )}

        <DialogFooter>
          <Button variant="ghost" onClick={() => onOpenChange(false)} disabled={busy}>
            Cancelar
          </Button>
          <Button onClick={submit} disabled={busy || !file}>
            {busy ? <Loader2 className="size-4 animate-spin" /> : <Upload className="size-4" />}
            Subir a la rama
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}

// ---------------------------------------------------------------- conflict

export function MergeConflictDialog({
  open,
  onOpenChange,
  paths,
  busy,
  onForce,
  title = 'Main cambió el mismo plano',
  description,
  note,
  forceLabel = 'Imponer mi versión'
}: {
  open: boolean
  onOpenChange: (v: boolean) => void
  paths: string[]
  busy: boolean
  onForce: () => void
  title?: string
  description?: React.ReactNode
  note?: React.ReactNode
  forceLabel?: string
}) {
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-w-md">
        <DialogHeader>
          <div className="text-warning mb-1 flex items-center gap-2 text-[11px] font-semibold tracking-[0.14em] uppercase">
            <AlertTriangle className="size-3.5" /> conflicto de revisión
          </div>
          <DialogTitle>{title}</DialogTitle>
          <DialogDescription>
            {description ?? (
              <>
                Estos archivos se modificaron en <span className="font-mono">main</span> desde que
                abriste la rama. Podés descartar tu versión o imponerla como definitiva.
              </>
            )}
          </DialogDescription>
        </DialogHeader>

        <div className="rounded-md border">
          {paths.map((p) => (
            <div key={p} className="border-b px-3 py-1.5 font-mono text-[11px] last:border-b-0">
              {p}
            </div>
          ))}
        </div>

        <div className="bg-muted/40 rounded-md border px-3 py-2 text-[12px] leading-relaxed">
          {note ?? (
            <>
              <strong>Imponer mi versión</strong> crea un commit de merge que deja el contenido de la
              rama en main. La versión anterior no se pierde: queda en el historial.
            </>
          )}
        </div>

        <DialogFooter>
          <Button variant="ghost" onClick={() => onOpenChange(false)} disabled={busy}>
            Cancelar
          </Button>
          <Button variant="destructive" onClick={onForce} disabled={busy}>
            {busy && <Loader2 className="size-4 animate-spin" />}
            {forceLabel}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
