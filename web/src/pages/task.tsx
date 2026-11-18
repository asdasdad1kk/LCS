import { useCallback, useEffect, useMemo, useState } from 'react'
import { Link, useNavigate, useOutletContext, useParams, useSearchParams } from 'react-router-dom'
import {
  ArrowLeft,
  ArrowRight,
  Check,
  CheckCheck,
  Download,
  Eye,
  FileBox,
  GitBranch,
  Columns2,
  GitMerge,
  Images,
  Target,
  Loader2,
  PencilLine,
  ShieldCheck,
  Trash2,
  Upload
} from 'lucide-react'
import { toast } from 'sonner'
import { api, ApiError, type Branch, type ReviewView, type Snapshot } from '@/lib/api'
import { ApprovalActions, ApprovalChain } from '@/components/approval-chain'
import { BranchTimeline } from '@/components/branch-timeline'
import { Button } from '@/components/ui/button'
import { Badge } from '@/components/ui/badge'
import { Skeleton } from '@/components/ui/separator'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { StatusBadge } from '@/components/status-badge'
import { CadViewer, type Bounds } from '@/components/cad-viewer'
import { CompareView, type FocusArea, type RefOption } from '@/components/compare-view'
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
  const navigate = useNavigate()
  const [searchParams] = useSearchParams()
  const shell = useOutletContext<{ reloadProjects?: () => void } | null>()
  const [branch, setBranch] = useState<Branch | null>(null)
  const [review, setReview] = useState<ReviewView | null>(null)
  const [loading, setLoading] = useState(true)
  const [uploading, setUploading] = useState(false)
  const [merging, setMerging] = useState(false)
  const [conflict, setConflict] = useState<string[] | null>(null)
  const [closing, setClosing] = useState(false)
  const [closingBusy, setClosingBusy] = useState(false)
  const [drawingIds, setDrawingIds] = useState<Record<string, string>>({})
  const [tab, setTab] = useState(searchParams.get('tab') ?? 'files')
  const [compareFile, setCompareFile] = useState<string | null>(searchParams.get('file'))
  const [focusArea, setFocusArea] = useState<string | null>(null)
  const [snapshots, setSnapshots] = useState<Snapshot[]>([])
  const [openSnap, setOpenSnap] = useState<Snapshot | null>(null)

  const loadSnapshots = useCallback(async () => {
    try {
      setSnapshots(await api.snapshots(taskId))
    } catch {
      /* los recortes son un extra: la tarea se ve igual */
    }
  }, [taskId])

  // La revisión recalcula en git qué planos cambiaron y corrige la copia local;
  // la tarea se vuelve a leer después para que ambas pantallas coincidan (antes
  // se leía primero y mostraba "no hay cambios subidos" con datos viejos).
  const loadReview = useCallback(async () => {
    try {
      setReview(await api.review(taskId))
      setBranch(await api.branch(taskId))
    } catch {
      /* las aprobaciones son secundarias: la tarea se ve igual */
    }
  }, [taskId])

  const load = useCallback(async () => {
    try {
      setBranch(await api.branch(taskId))
    } catch (err) {
      toast.error(err instanceof Error ? err.message : 'no pude cargar la tarea')
    } finally {
      setLoading(false)
    }
    await loadReview()
    void loadSnapshots()
  }, [taskId, loadReview, loadSnapshots])

  useEffect(() => {
    load()
  }, [load])

  // Otras personas aprueban desde sus equipos: mantener el estado al día.
  useEffect(() => {
    const timer = window.setInterval(loadReview, 8000)
    window.addEventListener('focus', loadReview)
    return () => {
      window.clearInterval(timer)
      window.removeEventListener('focus', loadReview)
    }
  }, [loadReview])

  const decide = async (kind: 'approve' | 'reject', comment: string) => {
    try {
      const next = kind === 'approve' ? await api.approve(taskId, comment) : await api.reject(taskId, comment)
      setReview(next)
      shell?.reloadProjects?.()
      toast.success(kind === 'approve' ? 'Aprobaste esta versión' : 'Rechazaste esta versión', {
        description:
          kind === 'approve' && next.complete ? 'Ya la aprobaron todos: se puede integrar.' : undefined
      })
    } catch (err) {
      toast.error(err instanceof Error ? err.message : 'no pude registrar tu decisión')
      await loadReview()
    }
  }

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
  // Manda lo que calculó la revisión desde git; los archivos del índice local son el respaldo.
  const hasChanges = review ? review.hasChanges : files.some((f) => f.changed)
  const isDone = branch?.status === 'merged' || branch?.status === 'closed'

  // Con aprobadores, el flujo tiene un paso más: Aprobar va antes de Integrar.
  const needsApproval = !!review?.required
  const blocked = needsApproval && !review?.complete // no se puede integrar todavía

  const step = useMemo(() => {
    const last = needsApproval ? 5 : 4
    if (branch?.status === 'merged') return last + 1
    if (branch?.status === 'review') return needsApproval && blocked ? 4 : last
    if (hasChanges) return 3
    return 2
  }, [branch, hasChanges, needsApproval, blocked])

  const doMerge = async (strategy?: string) => {
    setMerging(true)
    try {
      const res = await api.mergeBranch(taskId, strategy)
      toast.success('Tarea integrada en main', {
        description: `${res.paths.length} archivo(s) · ${shortSha(res.commit)}`
      })
      setConflict(null)
      await load()
      shell?.reloadProjects?.()
      // Una zona que cambió deja al master atrasado: avisar en el momento.
      try {
        const detail = await api.project(projectId)
        if (detail.parent && detail.integration && detail.integration.changed.length + detail.integration.removed.length > 0) {
          toast.warning(`Hay que actualizar el master «${detail.parent.name}»`, {
            description: 'La zona cambió y el master todavía no tiene esta versión.',
            duration: 12000,
            action: { label: 'Ir a actualizar', onClick: () => navigate(`/p/${projectId}`) }
          })
        }
      } catch {
        /* el aviso es secundario */
      }
    } catch (err) {
      if (err instanceof ApiError && err.status === 409 && err.payload?.needsApproval) {
        // Alguien subió un plano nuevo o cambió la cadena desde que se abrió la pantalla.
        toast.error(err.message)
        await loadReview()
      } else if (err instanceof ApiError && err.status === 409) {
        setConflict(err.payload?.paths ?? [])
      } else {
        toast.error(err instanceof Error ? err.message : 'no pude integrar la tarea')
      }
    } finally {
      setMerging(false)
    }
  }

  // Comparar: el plano elegido (por defecto, el primero que cambió) y las
  // versiones disponibles de esta tarea.
  const changedFiles = files.filter((f) => f.changed)
  const cmpFile = files.find((f) => f.path === compareFile) ?? changedFiles[0] ?? files[0]
  const cmpId = cmpFile ? drawingIds[cmpFile.path] : undefined
  const cmpOptions: RefOption[] = useMemo(() => {
    if (!branch) return []
    const out: RefOption[] = []
    if (branch.baseCommit) out.push({ ref: branch.baseCommit, label: `Base · main al abrir la tarea (${shortSha(branch.baseCommit)})` })
    out.push({ ref: 'main', label: 'main actual' })
    const uploads = (review?.timeline ?? []).filter((i) => i.kind === 'upload' && i.commit)
    uploads.forEach((u, i) =>
      out.push({ ref: u.commit!, label: `Versión ${i + 1} · ${u.message || 'sin mensaje'} · ${timeAgo(u.at)}` })
    )
    out.push({ ref: branch.name, label: 'Última versión de la tarea' })
    return out
  }, [branch, review])
  const areas: FocusArea[] = useMemo(
    () =>
      snapshots
        .filter((s) => s.bounds && (!s.drawing || !cmpFile || s.drawing === fileName(cmpFile.path)))
        .map((s) => ({ id: s.id, label: s.caption || 'Zona marcada', bounds: s.bounds as Bounds })),
    [snapshots, cmpFile]
  )


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
                variant={hasChanges && !blocked ? 'default' : 'secondary'}
                disabled={merging || !hasChanges || blocked}
                title={blocked ? 'Faltan aprobaciones: mira la cadena de aprobación' : undefined}
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
        <Workflow branch={branch} step={step} withApproval={needsApproval} />
      </div>

      {review && review.overlaps.length > 0 && !isDone && (
        <div className="px-6 pb-4">
          <div className="border-warning/40 bg-warning/10 rounded-lg border px-4 py-3 text-[12.5px] leading-relaxed">
            <div className="mb-1 flex items-center gap-1.5 font-semibold">
              <PencilLine className="text-warning size-4" /> Otra tarea edita los mismos planos
            </div>
            <ul className="text-muted-foreground space-y-0.5">
              {review.overlaps.map((o) => (
                <li key={o.branchId}>
                  <Link to={`/p/${projectId}/t/${o.branchId}`} className="text-foreground font-medium hover:underline">
                    {o.title}
                  </Link>{' '}
                  · {o.author} · {o.paths.map((p) => fileName(p)).join(', ')}
                </li>
              ))}
            </ul>
            <p className="text-muted-foreground mt-1">
              Dos ediciones del mismo DWG no se pueden fusionar: la que se integre después reemplaza a la
              otra.
            </p>
          </div>
        </div>
      )}

      {review && (needsApproval || review.nodes.length > 0) ? (
        <div className="px-6 pb-4">
          <ApprovalCard
            review={review}
            isDone={isDone}
            merging={merging}
            hasChanges={hasChanges}
            onMerge={() => doMerge()}
            onApprove={(c) => decide('approve', c)}
            onReject={(c) => decide('reject', c)}
          />
        </div>
      ) : (
        review &&
        !isDone && (
          <div className="px-6 pb-4">
            <div className="text-muted-foreground flex flex-wrap items-center gap-x-3 gap-y-1 rounded-lg border border-dashed px-4 py-2.5 text-[12.5px]">
              <ShieldCheck className="size-4" />
              Este proyecto no tiene aprobadores: la tarea se integra sin revisión.
              <Link to={`/p/${projectId}?tab=approvers`} className="text-primary font-medium hover:underline">
                Definir quién aprueba →
              </Link>
            </div>
          </div>
        )
      )}

      <div className="grid gap-5 px-6 pb-10 lg:grid-cols-[1.5fr_1fr]">
        <section>
          <Tabs value={tab} onValueChange={setTab}>
            <TabsList>
              <TabsTrigger value="files">Archivos · {files.length}</TabsTrigger>
              <TabsTrigger value="preview">Vista previa</TabsTrigger>
              <TabsTrigger value="compare">
                <Columns2 /> Comparar
              </TabsTrigger>
              <TabsTrigger value="snaps">Recortes{snapshots.length ? ` · ${snapshots.length}` : ''}</TabsTrigger>
              <TabsTrigger value="history">Línea de tiempo</TabsTrigger>
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
                      <Badge variant={f.changed ? 'success' : 'muted'}>
                        {f.changed ? 'subido' : 'sin cambios'}
                      </Badge>
                      <div className="text-muted-foreground tnum mt-1 text-[11.5px]">
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
                      changes={areas.map((a) => a.bounds)}
                      onSnapshot={
                        isDone
                          ? undefined
                          : async (png, caption, bounds) => {
                              try {
                                await api.addSnapshot(branch.id, png, {
                                  caption,
                                  drawing: fileName(firstFile.path),
                                  bounds: bounds ? JSON.stringify(bounds) : undefined
                                })
                                toast.success('Recorte guardado en la tarea')
                                void loadSnapshots()
                              } catch (err) {
                                toast.error(err instanceof Error ? err.message : 'no pude guardar el recorte')
                                throw err
                              }
                            }
                      }
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

            <TabsContent value="compare" className="mt-3">
              {tab === 'compare' &&
                (cmpFile && cmpId ? (
                  <div className="space-y-2.5">
                    {changedFiles.length > 1 && (
                      <select
                        value={cmpFile.path}
                        onChange={(e) => setCompareFile(e.target.value)}
                        className="bg-background h-8 rounded-md border px-2 text-[12.5px]"
                      >
                        {changedFiles.map((f) => (
                          <option key={f.path} value={f.path}>
                            {fileName(f.path)}
                          </option>
                        ))}
                      </select>
                    )}
                    <CompareView
                      key={`${cmpId}-${focusArea ?? ''}`}
                      drawingId={cmpId}
                      fileName={fileName(cmpFile.path)}
                      options={cmpOptions}
                      initialLeft={cmpOptions[0]?.ref ?? 'main'}
                      initialRight={branch.name}
                      areas={areas}
                      selectedArea={focusArea}
                    />
                    <p className="text-muted-foreground text-[11px]">
                      Los dos planos se mueven juntos. Elige qué versión ves de cada lado; con
                      «Enfocar cambios» ambos van a las zonas que se marcaron con recortes.
                    </p>
                  </div>
                ) : (
                  <div className="text-muted-foreground rounded-lg border border-dashed py-12 text-center text-[13px]">
                    Sube una versión para poder compararla con la de main.
                  </div>
                ))}
            </TabsContent>

            <TabsContent value="snaps" className="mt-3">
              {snapshots.length === 0 ? (
                <div className="text-muted-foreground rounded-lg border border-dashed px-6 py-12 text-center text-[13px]">
                  <Images className="mx-auto mb-2 size-5 opacity-60" />
                  Todavía no hay recortes. En la vista previa usá el botón de recortar para marcar la
                  zona del layout que se va a cambiar.
                </div>
              ) : (
                <div className="grid gap-3 sm:grid-cols-2">
                  {snapshots.map((s) => (
                    <button
                      key={s.id}
                      type="button"
                      onClick={() => setOpenSnap(s)}
                      className="bg-card hover:border-primary/60 overflow-hidden rounded-lg border text-left transition-colors"
                    >
                      <div className="grid h-40 place-items-center bg-white">
                        <img
                          src={api.snapshotUrl(branch.id, s.id)}
                          alt={s.caption || 'Recorte'}
                          loading="lazy"
                          className="max-h-40 object-contain"
                        />
                      </div>
                      <div className="border-t px-3 py-2">
                        <p className="truncate text-[12.5px] font-medium">{s.caption || 'Sin nota'}</p>
                        <p className="text-muted-foreground text-[11px]">
                          {s.author} · {timeAgo(s.at)}
                        </p>
                        {s.bounds && (
                          <span
                            role="button"
                            tabIndex={0}
                            onClick={(e) => {
                              e.stopPropagation()
                              setFocusArea(s.id)
                              setTab('compare')
                            }}
                            className="text-primary mt-1 inline-flex items-center gap-1 text-[11.5px] font-medium hover:underline"
                          >
                            <Target className="size-3" /> Ver en el plano
                          </span>
                        )}
                      </div>
                    </button>
                  ))}
                </div>
              )}
            </TabsContent>

            <TabsContent value="history" className="mt-3">
              {review ? (
                <BranchTimeline items={review.timeline} />
              ) : (
                <Skeleton className="h-40 w-full" />
              )}
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

      <UploadDialog
        open={uploading}
        onOpenChange={setUploading}
        branch={branch}
        onDone={async () => {
          setTab('compare')
          await load()
        }}
      />

      <MergeConflictDialog
        open={!!conflict}
        onOpenChange={(v) => !v && setConflict(null)}
        paths={conflict ?? []}
        busy={merging}
        onForce={() => doMerge('take-branch')}
      />

      <Dialog open={!!openSnap} onOpenChange={(v) => !v && setOpenSnap(null)}>
        <DialogContent className="max-w-3xl">
          <DialogHeader>
            <DialogTitle>{openSnap?.caption || 'Recorte del layout'}</DialogTitle>
            <DialogDescription>
              {openSnap && `${openSnap.author} · ${formatDate(openSnap.at)}`}
            </DialogDescription>
          </DialogHeader>
          {openSnap && (
            <div className="max-h-[70vh] overflow-auto rounded-md border bg-white">
              <img src={api.snapshotUrl(branch.id, openSnap.id)} alt="Recorte" className="mx-auto" />
            </div>
          )}
          <DialogFooter>
            {openSnap && (
              <Button variant="outline" asChild>
                <a href={api.snapshotUrl(branch.id, openSnap.id)} download={`recorte-${openSnap.id}.png`}>
                  <Download className="size-4" /> Descargar
                </a>
              </Button>
            )}
          </DialogFooter>
        </DialogContent>
      </Dialog>

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

/** El nodo de aprobación: la cadena en orden y, al final, el nodo de integrar. */
function ApprovalCard({
  review,
  isDone,
  merging,
  hasChanges,
  onMerge,
  onApprove,
  onReject
}: {
  review: ReviewView
  isDone: boolean
  merging: boolean
  hasChanges: boolean
  onMerge: () => void
  onApprove: (comment: string) => Promise<void>
  onReject: (comment: string) => Promise<void>
}) {
  const approved = review.nodes.filter((n) => n.status === 'approved').length
  const rejected = review.nodes.some((n) => n.status === 'rejected')
  const total = review.nodes.length
  const summary = review.complete
    ? { text: 'Aprobada por todos', variant: 'success' as const }
    : rejected
      ? { text: 'Rechazada', variant: 'destructive' as const }
      : { text: `${approved} de ${total} aprobaron`, variant: 'warning' as const }

  return (
    <div className="bg-card rounded-lg border shadow-xs">
      <div className="flex flex-wrap items-center gap-2 border-b px-4 py-3">
        <ShieldCheck className="text-muted-foreground size-4" />
        <h2 className="text-[13.5px] font-semibold">Aprobación</h2>
        <Badge variant={summary.variant}>{summary.text}</Badge>
        <span className="text-muted-foreground ml-auto text-[12px]">
          Cada nodo decide después del anterior. Una versión nueva reinicia la cadena.
        </span>
      </div>

      <div className="grid gap-x-8 gap-y-4 px-4 py-4 md:grid-cols-[1fr_1fr]">
        <div>
          <ApprovalChain nodes={review.nodes} />
        </div>
        <div className="flex flex-col">
          {!hasChanges && !isDone && (
            <p className="text-muted-foreground bg-muted/50 rounded-md px-3 py-2 text-[12.5px]">
              {review.uploaded
                ? 'Subiste una versión, pero el plano es idéntico al de main: no hay diferencias que aprobar. Subí un DWG con cambios.'
                : 'Todavía no hay cambios subidos: no hay nada que aprobar.'}
            </p>
          )}
          <ApprovalActions review={review} onApprove={onApprove} onReject={onReject} />

          {/* Último nodo de la cadena: integrar a main. */}
          <div
            className={cn(
              'mt-auto flex items-center gap-3 rounded-lg border px-3 py-2.5',
              review.complete && !isDone ? 'border-success/50 bg-success/5' : 'bg-muted/30'
            )}
          >
            <span
              className={cn(
                'grid size-7 shrink-0 place-items-center rounded-full',
                isDone || review.complete ? 'bg-success text-white' : 'bg-muted text-muted-foreground'
              )}
            >
              <GitMerge className="size-4" />
            </span>
            <div className="min-w-0 flex-1">
              <div className="text-[13px] font-medium">Integrar a main</div>
              <div className="text-muted-foreground text-[11.5px]">
                {isDone
                  ? 'Ya integrada.'
                  : review.complete
                    ? 'Todos aprobaron esta versión.'
                    : 'Se habilita cuando todos aprueben.'}
              </div>
            </div>
            {!isDone && (
              <Button size="sm" disabled={merging || !hasChanges || !review.complete} onClick={onMerge}>
                {merging ? <Loader2 className="size-4 animate-spin" /> : <GitMerge className="size-4" />}
                Integrar
              </Button>
            )}
          </div>
        </div>
      </div>
    </div>
  )
}

function Workflow({ branch, step, withApproval }: { branch: Branch; step: number; withApproval: boolean }) {
  const steps = [
    { n: 1, label: 'Descargar', hint: 'ZIP con los DWG de la rama' },
    { n: 2, label: 'Editar', hint: 'AutoCAD · mismos nombres' },
    { n: 3, label: 'Subir', hint: 'commit en la rama' },
    ...(withApproval
      ? [
          { n: 4, label: 'Aprobar', hint: 'todos los nodos, en orden' },
          { n: 5, label: 'Integrar', hint: 'merge a main' }
        ]
      : [{ n: 4, label: 'Integrar', hint: 'merge a main' }])
  ]
  const done = branch.status === 'merged'
  return (
    <div className={cn('grid grid-cols-2 gap-2', withApproval ? 'md:grid-cols-5' : 'md:grid-cols-4')}>
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
