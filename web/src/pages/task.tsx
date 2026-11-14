import { useCallback, useEffect, useMemo, useState } from 'react'
import { Link, useParams } from 'react-router-dom'
import {
  ArrowLeft,
  ArrowRight,
  Check,
  CheckCheck,
  Download,
  Eye,
  FileBox,
  GitBranch,
  GitMerge,
  Loader2,
  Trash2,
  Upload
} from 'lucide-react'
import { toast } from 'sonner'
import { api, ApiError, type Branch } from '@/lib/api'
import { Button } from '@/components/ui/button'
import { Badge } from '@/components/ui/badge'
import { Skeleton } from '@/components/ui/separator'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { StatusBadge } from '@/components/status-badge'
import { CadViewer } from '@/components/cad-viewer'
import { MergeConflictDialog, UploadDialog } from '@/components/dialogs'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle
} from '@/components/ui/dialog'
import { fileName, formatBytes, formatDate, initials, shortSha, timeAgo, PREVIEW_LIMIT } from '@/lib/format'
import { cn } from '@/lib/utils'

export function TaskPage() {
  const { projectId = '', taskId = '' } = useParams()
  const [branch, setBranch] = useState<Branch | null>(null)
  const [loading, setLoading] = useState(true)
  const [uploading, setUploading] = useState(false)
  const [merging, setMerging] = useState(false)
  const [conflict, setConflict] = useState<string[] | null>(null)
  const [closing, setClosing] = useState(false)
  const [closingBusy, setClosingBusy] = useState(false)
  const [drawingIds, setDrawingIds] = useState<Record<string, string>>({})

  const load = useCallback(async () => {
    try {
      setBranch(await api.branch(taskId))
    } catch (err) {
      toast.error(err instanceof Error ? err.message : 'no pude cargar la tarea')
    } finally {
      setLoading(false)
    }
  }, [taskId])

  useEffect(() => {
    load()
  }, [load])

  useEffect(() => {
    if (!projectId) return
    api
      .project(projectId)
      .then((d) =>
        setDrawingIds(Object.fromEntries(d.drawings.map((x) => [x.path, x.id] as const)))
      )
      .catch(() => {})
  }, [projectId])

  const files = branch?.files ?? []
  const hasChanges = files.some((f) => f.changed)
  const isDone = branch?.status === 'merged' || branch?.status === 'closed'

  const step = useMemo(() => {
    if (branch?.status === 'merged') return 4
    if (branch?.status === 'review') return 4
    if (hasChanges) return 3
    return 2
  }, [branch, hasChanges])

  const doMerge = async (strategy?: string) => {
    setMerging(true)
    try {
      const res = await api.mergeBranch(taskId, strategy)
      toast.success('Tarea integrada en main', {
        description: `${res.paths.length} archivo(s) · ${shortSha(res.commit)}`
      })
      setConflict(null)
      await load()
    } catch (err) {
      if (err instanceof ApiError && err.status === 409) {
        setConflict(err.payload?.paths ?? [])
      } else {
        toast.error(err instanceof Error ? err.message : 'no pude integrar la tarea')
      }
    } finally {
      setMerging(false)
    }
  }

  if (loading) {
    return (
      <div className="space-y-3 p-6">
        <Skeleton className="h-8 w-80" />
        <Skeleton className="h-32 w-full" />
        <Skeleton className="h-64 w-full" />
      </div>
    )
  }
  if (!branch) return <div className="p-6 text-sm">Tarea no encontrada.</div>

  const firstFile = files[0]
  const previewId = firstFile ? drawingIds[firstFile.path] : undefined

  return (
    <div className="scrollbar-thin flex-1 overflow-y-auto">
      <div className="flex flex-wrap items-start justify-between gap-4 border-b px-6 py-4">
        <div className="min-w-0">
          <div className="text-muted-foreground mb-1.5 flex items-center gap-1.5 text-[11px]">
            <Link to="/tareas" className="hover:text-foreground inline-flex items-center gap-1">
              <ArrowLeft className="size-3" /> tareas
            </Link>
            <span className="opacity-40">/</span>
            <Link to={`/p/${projectId}`} className="hover:text-foreground truncate">
              {projectId}
            </Link>
          </div>
          <div className="flex flex-wrap items-center gap-2.5">
            <h1 className="text-[18px] font-semibold tracking-tight">{branch.title}</h1>
            <StatusBadge status={branch.status} />
          </div>
          <div className="text-muted-foreground mt-1 flex flex-wrap items-center gap-2 font-mono text-[11px]">
            <span className="inline-flex items-center gap-1">
              <GitBranch className="size-3" />
              {branch.name}
            </span>
            <span>·</span>
            <span>base {shortSha(branch.baseCommit)}</span>
            <span>·</span>
            <span>head {shortSha(branch.headCommit)}</span>
          </div>
        </div>

        <div className="flex flex-wrap items-center gap-2">
          <Button variant="outline" size="sm" asChild>
            <a href={api.downloadUrl(branch.id)}>
              <Download className="size-4" /> Descargar ZIP
            </a>
          </Button>
          {!isDone && (
            <>
              <Button size="sm" onClick={() => setUploading(true)}>
                <Upload className="size-4" /> Subir cambios
              </Button>
              <Button
                size="sm"
                variant={hasChanges ? 'default' : 'secondary'}
                disabled={merging || !hasChanges}
                onClick={() => doMerge()}
              >
                {merging ? <Loader2 className="size-4 animate-spin" /> : <GitMerge className="size-4" />}
                Integrar
              </Button>
              <Button
                variant="ghost"
                size="icon-sm"
                className="text-muted-foreground hover:text-destructive"
                title="Descartar tarea"
                onClick={() => setClosing(true)}
              >
                <Trash2 className="size-4" />
              </Button>
            </>
          )}
        </div>
      </div>

      <div className="px-6 py-4">
        <Workflow branch={branch} step={step} />
      </div>

      <div className="grid gap-5 px-6 pb-10 lg:grid-cols-[1.5fr_1fr]">
        <section>
          <Tabs defaultValue="files">
            <TabsList>
              <TabsTrigger value="files">Archivos · {files.length}</TabsTrigger>
              <TabsTrigger value="preview">Vista previa</TabsTrigger>
              <TabsTrigger value="history">Historial</TabsTrigger>
            </TabsList>

            <TabsContent value="files" className="mt-3">
              <div className="divide-y overflow-hidden rounded-lg border bg-card">
                {files.map((f) => (
                  <div key={f.path} className="flex items-center gap-3 px-3.5 py-3">
                    <div className="bg-muted/50 grid size-8 shrink-0 place-items-center rounded-md border">
                      <FileBox className="text-muted-foreground size-3.5" />
                    </div>
                    <div className="min-w-0 flex-1">
                      <div className="truncate text-[13px] font-medium">{fileName(f.path)}</div>
                      <div className="text-muted-foreground truncate font-mono text-[10px]">{f.path}</div>
                    </div>
                    <div className="text-right text-[11px]">
                      <Badge variant={f.changed ? 'success' : 'muted'} className="font-mono text-[10px]">
                        {f.changed ? 'subido' : 'sin cambios'}
                      </Badge>
                      <div className="text-muted-foreground mt-1 font-mono text-[10px]">
                        {formatBytes(f.sizeBytes || 0)}
                      </div>
                    </div>
                  </div>
                ))}
                {files.length === 0 && (
                  <div className="text-muted-foreground px-4 py-6 text-center text-[13px]">
                    La rama no reporta archivos todavía. Sincronizá el proyecto.
                  </div>
                )}
              </div>

              <div className="text-muted-foreground mt-3 rounded-lg border border-dashed px-3.5 py-3 text-[11.5px] leading-relaxed">
                <strong className="text-foreground">Cómo trabaja esta rama.</strong> El ZIP trae los DWG
                en <span className="font-mono">drawings/</span> más un <span className="font-mono">LEEME.txt</span>.
                Al subir, LCS verifica la cabecera DWG, commitea en la rama y empuja al NAS. Nada de esto
                toca <span className="font-mono">main</span> hasta que integres.
              </div>
            </TabsContent>

            <TabsContent value="preview" className="mt-3">
              {firstFile && previewId ? (
                firstFile.sizeBytes > PREVIEW_LIMIT ? (
                  <div className="grid h-[240px] place-items-center rounded-lg border border-dashed p-6 text-center">
                    <div className="max-w-sm">
                      <p className="text-[13.5px] font-medium">
                        Plano de {formatBytes(firstFile.sizeBytes)}
                      </p>
                      <p className="text-muted-foreground mt-1 text-[12px] leading-relaxed">
                        La vista previa está desactivada para archivos de más de{' '}
                        {formatBytes(PREVIEW_LIMIT)}. Descargá el ZIP y abrilo en AutoCAD.
                      </p>
                    </div>
                  </div>
                ) : (
                  <div className="h-[560px] overflow-hidden rounded-lg border">
                    <CadViewer
                      drawingId={previewId}
                      fileName={fileName(firstFile.path)}
                      refName={branch.name}
                      className="h-full w-full"
                    />
                  </div>
                )
              ) : (
                <div className="text-muted-foreground rounded-lg border border-dashed py-12 text-center text-[13px]">
                  Sin archivos para previsualizar.
                </div>
              )}
              <p className="text-muted-foreground mt-2 text-[11px]">
                Esta vista lee el DWG desde la rama <span className="font-mono">{branch.name}</span>, no
                desde main.
              </p>
            </TabsContent>

            <TabsContent value="history" className="mt-3">
              <div className="overflow-hidden rounded-lg border bg-card">
                {(branch.revisions ?? []).map((r) => (
                  <div key={r.id} className="flex items-start gap-3 border-b px-3.5 py-2.5 last:border-b-0">
                    <span className="bg-muted text-muted-foreground mt-0.5 grid size-5 shrink-0 place-items-center rounded-full font-mono text-[8px] font-bold">
                      {initials(r.author)}
                    </span>
                    <div className="min-w-0 flex-1">
                      <p className="text-[12.5px] leading-snug">{r.message.replace(/^LCS:\s*/, '')}</p>
                      <p className="text-muted-foreground mt-0.5 font-mono text-[10px]">
                        {shortSha(r.commitSha)} · {r.author} · {timeAgo(r.createdAt)}
                      </p>
                    </div>
                    <span className="text-muted-foreground shrink-0 font-mono text-[10px]">
                      {formatBytes(r.size)}
                    </span>
                  </div>
                ))}
                {(branch.revisions ?? []).length === 0 && (
                  <div className="text-muted-foreground px-4 py-6 text-center text-[13px]">
                    Sin commits en esta rama.
                  </div>
                )}
              </div>
            </TabsContent>
          </Tabs>
        </section>

        <aside className="space-y-3">
          <div className="rounded-lg border bg-card">
            <div className="border-b px-3.5 py-2.5 text-[11px] font-semibold tracking-[0.14em] uppercase text-muted-foreground">
              Ficha de la tarea
            </div>
            <dl className="divide-y text-[12px]">
              <Row label="Autor" value={branch.author} avatar />
              <Row label="Creada" value={formatDate(branch.createdAt)} />
              <Row label="Actualizada" value={formatDate(branch.updatedAt)} />
              <Row label="Base" value={shortSha(branch.baseCommit)} mono />
              <Row label="Head" value={shortSha(branch.headCommit)} mono />
              {branch.mergedAt > 0 && <Row label="Integrada" value={formatDate(branch.mergedAt)} />}
              <Row label="Ref" value={branch.name} mono />
            </dl>
          </div>

          <div className="rounded-lg border bg-card p-3.5">
            <div className="mb-2 text-[11px] font-semibold tracking-[0.14em] uppercase text-muted-foreground">
              Siguiente paso
            </div>
            <p className="text-[12.5px] leading-relaxed">
              {branch.status === 'merged'
                ? 'Tarea integrada. El plano vigente en main ya incluye estos cambios.'
                : branch.status === 'closed'
                  ? 'Tarea descartada. El plano sigue como estaba en main.'
                  : !hasChanges
                    ? 'Descargá el ZIP, editá el DWG en AutoCAD y subilo con el mismo nombre.'
                    : 'Revisá la vista previa y, si está conforme, integrá la tarea a main.'}
            </p>
            {!isDone && (
              <div className="mt-3 flex flex-col gap-1.5">
                <Button size="sm" variant="outline" className="justify-start" asChild>
                  <a href={api.downloadUrl(branch.id)}>
                    <Download className="size-4" /> Descargar DWG de la rama
                  </a>
                </Button>
                <Button size="sm" variant="outline" className="justify-start" onClick={() => setUploading(true)}>
                  <Upload className="size-4" /> Subir versión editada
                </Button>
                <Button size="sm" variant="outline" className="justify-start" asChild>
                  <Link to={previewId ? `/p/${projectId}/d/${previewId}` : '#'}>
                    <Eye className="size-4" /> Ver el plano en main
                    <ArrowRight className="ml-auto size-3" />
                  </Link>
                </Button>
              </div>
            )}
          </div>
        </aside>
      </div>

      <UploadDialog open={uploading} onOpenChange={setUploading} branch={branch} onDone={load} />

      <MergeConflictDialog
        open={!!conflict}
        onOpenChange={(v) => !v && setConflict(null)}
        paths={conflict ?? []}
        busy={merging}
        onForce={() => doMerge('take-branch')}
      />

      <Dialog open={closing} onOpenChange={setClosing}>
        <DialogContent className="max-w-md">
          <DialogHeader>
            <DialogTitle>Descartar tarea</DialogTitle>
            <DialogDescription>
              Se elimina la rama <span className="font-mono">{branch.name}</span> del NAS. Los commits
              quedan en el historial del repositorio, pero el trabajo deja de estar disponible.
            </DialogDescription>
          </DialogHeader>
          <DialogFooter>
            <Button variant="ghost" onClick={() => setClosing(false)} disabled={closingBusy}>
              Cancelar
            </Button>
            <Button
              variant="destructive"
              disabled={closingBusy}
              onClick={async () => {
                setClosingBusy(true)
                try {
                  await api.closeBranch(branch.id)
                  toast.success('Tarea descartada')
                  setClosing(false)
                  await load()
                } catch (err) {
                  toast.error(err instanceof Error ? err.message : 'no pude descartar la tarea')
                } finally {
                  setClosingBusy(false)
                }
              }}
            >
              {closingBusy && <Loader2 className="size-4 animate-spin" />}
              Descartar
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  )
}

function Workflow({ branch, step }: { branch: Branch; step: number }) {
  const steps = [
    { n: 1, label: 'Descargar', hint: 'ZIP con los DWG de la rama' },
    { n: 2, label: 'Editar', hint: 'AutoCAD · mismos nombres' },
    { n: 3, label: 'Subir', hint: 'commit en la rama' },
    { n: 4, label: 'Integrar', hint: 'merge a main' }
  ]
  const done = branch.status === 'merged'
  return (
    <div className="grid grid-cols-2 gap-2 md:grid-cols-4">
      {steps.map((s) => {
        const isDone = done || s.n < step
        const isCurrent = !done && s.n === step
        return (
          <div
            key={s.n}
            className={cn(
              'flex items-start gap-2.5 rounded-lg border px-3 py-2.5 transition-colors',
              isCurrent ? 'border-primary/40 bg-accent/50' : 'bg-card',
              isDone && 'opacity-80'
            )}
          >
            <span
              className={cn(
                'mt-0.5 grid size-5 shrink-0 place-items-center rounded-full text-[10px] font-semibold',
                isDone
                  ? 'bg-success text-white'
                  : isCurrent
                    ? 'bg-primary text-primary-foreground'
                    : 'bg-muted text-muted-foreground'
              )}
            >
              {isDone ? <Check className="size-3" /> : s.n}
            </span>
            <div className="min-w-0">
              <div className="text-[12.5px] font-medium">{s.label}</div>
              <div className="text-muted-foreground truncate text-[10.5px]">{s.hint}</div>
            </div>
          </div>
        )
      })}
    </div>
  )
}

function Row({
  label,
  value,
  mono,
  avatar
}: {
  label: string
  value: string
  mono?: boolean
  avatar?: boolean
}) {
  return (
    <div className="flex items-center justify-between gap-3 px-3.5 py-2">
      <dt className="text-muted-foreground">{label}</dt>
      <dd className={cn('flex min-w-0 items-center gap-1.5 truncate', mono && 'font-mono text-[11px]')}>
        {avatar && (
          <span className="grid size-4 place-items-center rounded-full bg-muted text-[7px] font-bold">
            {initials(value)}
          </span>
        )}
        {value}
      </dd>
    </div>
  )
}
