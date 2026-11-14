import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { Link, useNavigate, useParams } from 'react-router-dom'
import {
  ArrowLeft,
  Clock,
  Download,
  FileBox,
  GitBranch,
  Loader2,
  Maximize2,
  Plus,
  RotateCcw
} from 'lucide-react'
import { toast } from 'sonner'
import { api, type Branch, type DrawingDetail, type LayoutInfo, type Revision } from '@/lib/api'
import { Button } from '@/components/ui/button'
import { Badge } from '@/components/ui/badge'
import { Skeleton } from '@/components/ui/separator'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle
} from '@/components/ui/dialog'
import { StatusBadge } from '@/components/status-badge'
import { CadViewer, type CadViewerHandle } from '@/components/cad-viewer'
import { NewTaskDialog } from '@/components/dialogs'
import { formatBytes, formatDate, initials, shortSha, timeAgo, PREVIEW_LIMIT } from '@/lib/format'
import { cn } from '@/lib/utils'

export function DrawingPage() {
  const { projectId = '', drawingId = '' } = useParams()
  const navigate = useNavigate()
  const [data, setData] = useState<DrawingDetail | null>(null)
  const [layouts, setLayouts] = useState<LayoutInfo[]>([])
  const [activeLayout, setActiveLayout] = useState<string>('')
  const [viewRef, setViewRef] = useState<string | undefined>(undefined)
  const [newTask, setNewTask] = useState(false)
  const [loading, setLoading] = useState(true)
  const [restoreTarget, setRestoreTarget] = useState<Revision | null>(null)
  const [restoring, setRestoring] = useState(false)
  const viewerRef = useRef<CadViewerHandle>(null)
  const savedLayouts = useRef<string>('')

  const load = useCallback(async () => {
    try {
      const d = await api.drawing(drawingId)
      setData(d)
      if (Array.isArray(d.layouts) && d.layouts.length) {
        setLayouts(d.layouts)
        setActiveLayout((cur) => cur || d.layouts[0]?.btrId)
      }
    } catch (err) {
      toast.error(err instanceof Error ? err.message : 'no pude cargar el plano')
    } finally {
      setLoading(false)
    }
  }, [drawingId])

  useEffect(() => {
    load()
  }, [load])

  const onViewerLayouts = useCallback(
    (found: LayoutInfo[]) => {
      setLayouts(found)
      setActiveLayout((cur) => cur || found[0]?.btrId || '')
      const serialized = JSON.stringify(found)
      if (serialized !== savedLayouts.current) {
        savedLayouts.current = serialized
        api.saveLayouts(drawingId, { layouts: found }).catch(() => {})
      }
    },
    [drawingId]
  )

  const pickLayout = (l: LayoutInfo) => {
    setActiveLayout(l.btrId)
    viewerRef.current?.showLayout(l.btrId)
  }

  const current = useMemo(
    () => layouts.find((l) => l.btrId === activeLayout),
    [layouts, activeLayout]
  )

  // El parseo en el navegador es WASM: con planos de 100-150 MB se vuelve
  // inviable (minutos de espera y riesgo de quedarse sin memoria). Por encima
  // del límite mostramos descarga directa en vez de intentar dibujar.
  const previewSize = useMemo(() => {
    if (!data) return 0
    if (viewRef) {
      const rev = data.revisions.find((x) => x.commitSha === viewRef)
      if (rev) return rev.size
    }
    return data.drawing.size
  }, [data, viewRef])
  const previewBlocked = previewSize > PREVIEW_LIMIT

  async function doRestore() {
    if (!restoreTarget) return
    setRestoring(true)
    try {
      await api.restore(drawingId, restoreTarget.commitSha)
      toast.success(`Restaurado a ${shortSha(restoreTarget.commitSha)}`)
      setRestoreTarget(null)
      setViewRef(undefined)
      await load()
    } catch (err) {
      toast.error(err instanceof Error ? err.message : 'no pude restaurar la revisión')
    } finally {
      setRestoring(false)
    }
  }

  if (loading) {
    return (
      <div className="space-y-3 p-6">
        <Skeleton className="h-8 w-72" />
        <Skeleton className="h-[70vh] w-full" />
      </div>
    )
  }
  if (!data) return <div className="p-6 text-sm">Plano no encontrado.</div>

  const { drawing, revisions, branches } = data
  const viewingOld = !!viewRef

  return (
    <div className="flex h-full flex-col">
      <div className="flex flex-wrap items-center justify-between gap-3 border-b px-5 py-3">
        <div className="min-w-0">
          <div className="text-muted-foreground mb-1 flex items-center gap-1.5 text-[11px]">
            <Link to="/" className="hover:text-foreground inline-flex items-center gap-1">
              <ArrowLeft className="size-3" /> panel
            </Link>
            <span className="opacity-40">/</span>
            <Link
              to={`/p/${projectId}`}
              className="hover:text-foreground inline-flex items-center gap-1 truncate"
            >
              {projectId}
            </Link>
            <span className="opacity-40">/</span>
            <span className="font-mono normal-case">{drawing.path}</span>
          </div>
          <h1 className="truncate text-[17px] font-semibold tracking-tight">{drawing.name}</h1>
        </div>
        <div className="flex items-center gap-2">
          <Badge variant="outline" className="tnum">
            {formatBytes(drawing.size)}
          </Badge>
          <Button variant="outline" size="sm" asChild>
            <a href={api.fileUrl(drawingId)} download>
              <Download className="size-4" /> DWG
            </a>
          </Button>
          <Button size="sm" onClick={() => setNewTask(true)}>
            <Plus className="size-4" /> Abrir tarea
          </Button>
        </div>
      </div>

      {viewingOld && (
        <div className="bg-warning/10 border-warning/30 flex items-center gap-2 border-b px-5 py-2 text-[12px]">
          <Clock className="text-warning size-3.5" />
          Viendo la revisión <span className="font-mono font-medium">{shortSha(viewRef)}</span> — solo
          lectura.
          <Button
            variant="ghost"
            size="xs"
            className="ml-auto"
            onClick={() => {
              setViewRef(undefined)
            }}
          >
            <RotateCcw className="size-3" /> Volver a main
          </Button>
        </div>
      )}

      <div className="flex min-h-0 flex-1">
        <div className="relative min-w-0 flex-1 bg-muted/20">
          {previewBlocked ? (
            <div className="grid h-full place-items-center p-6">
              <div className="max-w-md text-center">
                <FileBox className="text-muted-foreground mx-auto mb-3 size-8" />
                <p className="text-[15px] font-medium">
                  Plano de {formatBytes(previewSize)} · vista previa desactivada
                </p>
                <p className="text-muted-foreground mt-1.5 text-[12.5px] leading-relaxed">
                  Los planos de más de {formatBytes(PREVIEW_LIMIT)} se descargan pero no se abren en
                  el navegador: el parseo tarda minutos y consume mucha memoria. Abrilo en AutoCAD.
                </p>
                <Button size="sm" className="mt-4" asChild>
                  <a href={api.fileUrl(drawingId, viewRef)} download>
                    <Download className="size-4" /> Descargar DWG
                  </a>
                </Button>
              </div>
            </div>
          ) : (
            <>
              <CadViewer
                ref={viewerRef}
                drawingId={drawingId}
                fileName={drawing.path.split('/').pop() ?? 'drawing.dwg'}
                refName={viewRef}
                onLayouts={onViewerLayouts}
                className="h-full w-full"
              />
              <div className="pointer-events-none absolute inset-x-0 bottom-0 flex items-center justify-between gap-2 bg-gradient-to-t from-black/50 to-transparent px-3 pt-6 pb-2 text-[11px] text-white/90">
                <span className="font-mono">
                  {current ? `${current.name} · ${layouts.length} layout(s)` : 'model space'}
                </span>
                <button
                  className="pointer-events-auto inline-flex items-center gap-1 rounded bg-white/10 px-1.5 py-0.5 backdrop-blur hover:bg-white/20"
                  onClick={() => viewerRef.current?.fit()}
                >
                  <Maximize2 className="size-3" /> encuadrar
                </button>
              </div>
            </>
          )}
        </div>

        <aside className="flex w-[330px] shrink-0 flex-col border-l bg-card">
          <Tabs defaultValue="layouts" className="flex min-h-0 flex-1 flex-col">
            <div className="border-b px-3 py-2">
              <TabsList className="w-full">
                <TabsTrigger value="layouts" className="flex-1">
                  Layouts
                </TabsTrigger>
                <TabsTrigger value="revisions" className="flex-1">
                  Revisiones
                </TabsTrigger>
                <TabsTrigger value="tasks" className="flex-1">
                  Tareas
                </TabsTrigger>
              </TabsList>
            </div>

            <TabsContent value="layouts" className="scrollbar-thin min-h-0 flex-1 overflow-y-auto p-2">
              {layouts.length === 0 && (
                <p className="text-muted-foreground p-3 text-[12px] leading-relaxed">
                  {previewBlocked
                    ? 'Los layouts de este plano no se pueden leer en el navegador por el tamaño. Abrilo en AutoCAD o en la vista previa de otra copia más liviana.'
                    : 'El visor todavía no reportó layouts. Se completa cuando el DWG termina de parsear.'}
                </p>
              )}
              {layouts.map((l, i) => {
                const isModel = /^(model|modelo|model space)$/i.test(l.name) || i === 0
                return (
                  <button
                    key={l.btrId}
                    onClick={() => pickLayout(l)}
                    className={cn(
                      'mb-1 flex w-full items-center gap-2.5 rounded-md border px-2.5 py-2 text-left text-[12.5px] transition-colors',
                      activeLayout === l.btrId
                        ? 'border-primary/40 bg-accent text-accent-foreground'
                        : 'border-transparent hover:bg-muted/60'
                    )}
                  >
                    <span className="text-muted-foreground tnum w-4 shrink-0 text-center text-[11px]">
                      {i + 1}
                    </span>
                    <FileBox className="text-muted-foreground size-3.5 shrink-0" />
                    <span className="min-w-0 flex-1 truncate">{l.name}</span>
                    {isModel && (
                      <Badge variant="muted" className="px-1 text-[9px] uppercase">
                        model
                      </Badge>
                    )}
                  </button>
                )
              })}
            </TabsContent>

            <TabsContent
              value="revisions"
              className="scrollbar-thin min-h-0 flex-1 overflow-y-auto p-2"
            >
              {revisions.length === 0 && (
                <p className="text-muted-foreground p-3 text-[12px]">Sin revisiones registradas.</p>
              )}
              {revisions.map((r: Revision) => {
                const isCurrent = r.blobSha === drawing.mainBlob
                return (
                  <div
                    key={r.id}
                    role="button"
                    tabIndex={0}
                    onClick={() => setViewRef(r.commitSha)}
                    onKeyDown={(e) => {
                      if (e.key === 'Enter' || e.key === ' ') setViewRef(r.commitSha)
                    }}
                    className={cn(
                      'group mb-1 w-full cursor-pointer rounded-md border px-2.5 py-2 text-left transition-colors',
                      viewRef === r.commitSha
                        ? 'border-primary/40 bg-accent'
                        : 'border-transparent hover:bg-muted/60'
                    )}
                  >
                    <div className="flex items-center gap-2">
                      <span className="font-mono text-[11px] font-medium">
                        {shortSha(r.commitSha)}
                      </span>
                      {isCurrent && (
                        <Badge variant="muted" className="px-1 text-[9px] tracking-wide uppercase">
                          en main
                        </Badge>
                      )}
                      <span className="text-muted-foreground ml-auto text-[10px]">
                        {timeAgo(r.createdAt)}
                      </span>
                      {!isCurrent && (
                        <button
                          type="button"
                          title="Restaurar esta revisión en main"
                          aria-label="Restaurar esta revisión en main"
                          onClick={(e) => {
                            e.stopPropagation()
                            setRestoreTarget(r)
                          }}
                          className="text-muted-foreground hover:text-foreground hover:bg-background -mr-1 rounded p-1 opacity-0 transition-opacity group-hover:opacity-100 focus-visible:opacity-100"
                        >
                          <RotateCcw className="size-3" />
                        </button>
                      )}
                    </div>
                    <p className="text-muted-foreground mt-0.5 line-clamp-2 text-[11.5px] leading-snug">
                      {r.message.replace(/^LCS:\s*/, '')}
                    </p>
                    <div className="text-muted-foreground mt-1 flex items-center gap-1.5 text-[10px]">
                      <span className="grid size-3.5 place-items-center rounded-full bg-muted text-[7px] font-bold">
                        {initials(r.author)}
                      </span>
                      {r.author} · {formatBytes(r.size)}
                    </div>
                  </div>
                )
              })}
            </TabsContent>

            <TabsContent value="tasks" className="scrollbar-thin min-h-0 flex-1 overflow-y-auto p-2">
              {branches.length === 0 && (
                <p className="text-muted-foreground p-3 text-[12px] leading-relaxed">
                  Ninguna rama abierta toca este plano.
                </p>
              )}
              {branches.map((b: Branch) => (
                <Link
                  key={b.id}
                  to={`/p/${projectId}/t/${b.id}`}
                  className="hover:bg-muted/60 mb-1 flex items-center gap-2 rounded-md border border-transparent px-2.5 py-2"
                >
                  <GitBranch className="text-muted-foreground size-3.5 shrink-0" />
                  <span className="min-w-0 flex-1 truncate text-[12.5px]">{b.title}</span>
                  <StatusBadge status={b.status} />
                </Link>
              ))}
              <Button variant="outline" size="sm" className="mt-2 w-full" onClick={() => setNewTask(true)}>
                <Plus className="size-4" /> Nueva tarea con este plano
              </Button>
            </TabsContent>
          </Tabs>

          <div className="text-muted-foreground space-y-1 border-t px-3.5 py-2.5 font-mono text-[10px]">
            <div className="flex justify-between">
              <span>blob</span>
              <span className="text-foreground">{shortSha(drawing.mainBlob)}</span>
            </div>
            <div className="flex justify-between">
              <span>actualizado</span>
              <span>{formatDate(drawing.updatedAt)}</span>
            </div>
          </div>
        </aside>
      </div>

      <Dialog open={!!restoreTarget} onOpenChange={(o) => !o && !restoring && setRestoreTarget(null)}>
        <DialogContent className="max-w-md">
          <DialogHeader>
            <DialogTitle>Restaurar revisión</DialogTitle>
            <DialogDescription>
              El contenido de{' '}
              <span className="text-foreground font-mono">
                {restoreTarget ? shortSha(restoreTarget.commitSha) : ''}
              </span>{' '}
              vuelve a main como un commit nuevo. No se reescribe el historial: se puede revisar y
              deshacer con otra restauración.
            </DialogDescription>
          </DialogHeader>
          <div className="text-muted-foreground rounded-md border bg-muted/40 px-3 py-2 text-[11.5px] leading-relaxed">
            {restoreTarget?.message.replace(/^LCS:\s*/, '')}
          </div>
          <DialogFooter>
            <Button variant="outline" onClick={() => setRestoreTarget(null)} disabled={restoring}>
              Cancelar
            </Button>
            <Button onClick={doRestore} disabled={restoring}>
              {restoring ? (
                <Loader2 className="size-4 animate-spin" />
              ) : (
                <RotateCcw className="size-4" />
              )}
              Restaurar en main
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      <NewTaskDialog
        open={newTask}
        onOpenChange={setNewTask}
        projectId={projectId}
        preselect={[drawing.path]}
        onCreated={(b) => navigate(`/p/${projectId}/t/${b.id}`)}
      />
    </div>
  )
}
