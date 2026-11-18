import { useCallback, useEffect, useMemo, useState } from 'react'
import { Link, useNavigate, useOutletContext, useParams, useSearchParams } from 'react-router-dom'
import {
  AlertTriangle,
  ArrowLeft,
  ArrowUpRight,
  CheckCircle2,
  FileBox,
  GitBranch,
  LayoutTemplate,
  PencilLine,
  Plus,
  RefreshCw,
  ShieldCheck,
  Loader2,
  Split,
  Trash2,
  Upload
} from 'lucide-react'
import { toast } from 'sonner'
import { api, type ActivityEvent, type Branch, type Drawing, type ProjectDetail } from '@/lib/api'
import { Button } from '@/components/ui/button'
import { Badge } from '@/components/ui/badge'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { Skeleton } from '@/components/ui/separator'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle
} from '@/components/ui/dialog'
import { PageHeader, EmptyState } from '@/components/page-header'
import { StatusBadge } from '@/components/status-badge'
import { ActivityGraph } from '@/pages/dashboard'
import { ImportDrawingsDialog, NewTaskDialog } from '@/components/dialogs'
import { NewProjectDialog } from '@/components/new-project-dialog'
import { ApproversEditor } from '@/components/approvers-editor'
import { IntegrateButton, useIntegrateZone } from '@/components/integrate-zone'
import { formatBytes, fileName, initials, planos, shortSha, timeAgo } from '@/lib/format'
import { cn } from '@/lib/utils'

export function ProjectPage() {
  const { projectId = '' } = useParams()
  const [searchParams] = useSearchParams()
  const tabParam = searchParams.get('tab')
  const navigate = useNavigate()
  const shell = useOutletContext<{ reloadProjects?: () => void } | null>()
  const [data, setData] = useState<ProjectDetail | null>(null)
  const [loading, setLoading] = useState(true)
  const [newTask, setNewTask] = useState(false)
  const [newZone, setNewZone] = useState(false)
  const [importing, setImporting] = useState(false)
  const [syncing, setSyncing] = useState(false)
  const [deleting, setDeleting] = useState(false)
  const [deleteBusy, setDeleteBusy] = useState(false)

  const load = useCallback(async () => {
    try {
      setData(await api.project(projectId))
    } catch (err) {
      toast.error(err instanceof Error ? err.message : 'no pude cargar el proyecto')
    } finally {
      setLoading(false)
    }
  }, [projectId])

  useEffect(() => {
    load()
  }, [load])

  const refreshAll = useCallback(async () => {
    await load()
    shell?.reloadProjects?.()
  }, [load, shell])
  const { integrate, busyId, dialog: conflictDialog } = useIntegrateZone(refreshAll)

  const active = useMemo(
    () => (data?.branches ?? []).filter((b) => b.status === 'open' || b.status === 'review'),
    [data]
  )
  const done = useMemo(
    () => (data?.branches ?? []).filter((b) => b.status === 'merged' || b.status === 'closed'),
    [data]
  )

  const sync = async () => {
    setSyncing(true)
    try {
      await api.syncProject(projectId)
      await load()
      toast.success('Repositorio sincronizado')
    } catch (err) {
      toast.error(err instanceof Error ? err.message : 'falló la sincronización')
    } finally {
      setSyncing(false)
    }
  }

  if (loading) {
    return (
      <div className="space-y-3 p-6">
        <Skeleton className="h-8 w-64" />
        <Skeleton className="h-28 w-full" />
        <Skeleton className="h-64 w-full" />
      </div>
    )
  }
  if (!data) return <div className="p-6 text-sm">Proyecto no encontrado.</div>

  const { project, parent, children, integration, drawings, mainHead, usage } = data
  const isZone = !!project.parentId
  const zoneChanges = integration ? integration.changed.length + integration.removed.length : 0
  const lateZones = children.filter((z) => z.pendingChanges > 0)

  return (
    <div className="scrollbar-thin flex-1 overflow-y-auto">
      <PageHeader
        eyebrow={
          <>
            <Link
              to={parent ? `/p/${parent.id}` : '/proyectos'}
              className="hover:text-foreground inline-flex items-center gap-1"
            >
              <ArrowLeft className="size-3" /> {parent ? parent.name : 'proyectos'}
            </Link>
            <span className="opacity-40">/</span>
            <span className="font-mono text-[11px] normal-case tracking-normal">{project.id}</span>
            {isZone && <Badge variant="outline">Zona</Badge>}
            {!isZone && children.length > 0 && <Badge variant="default">Master</Badge>}
          </>
        }
        title={project.name}
        description={project.description || 'Repositorio de planos gestionado con git.'}
        actions={
          <>
            <Button
              variant="outline"
              size="sm"
              className="text-destructive hover:text-destructive"
              onClick={() => setDeleting(true)}
            >
              <Trash2 className="size-4" /> Borrar
            </Button>
            <Button variant="outline" size="sm" onClick={sync} disabled={syncing}>
              <RefreshCw className={syncing ? 'size-4 animate-spin' : 'size-4'} /> Sincronizar
            </Button>
            {!isZone && (
              <Button variant="outline" size="sm" onClick={() => setNewZone(true)}>
                <Split className="size-4" /> Nuevo subproyecto
              </Button>
            )}
            <Button variant="outline" size="sm" onClick={() => setImporting(true)}>
              <Upload className="size-4" /> Importar
            </Button>
            <Button size="sm" onClick={() => setNewTask(true)}>
              <Plus className="size-4" /> Nueva tarea
            </Button>
          </>
        }
      />

      {isZone && parent && zoneChanges > 0 && (
        <div className="px-6 pb-4">
          <div className="border-warning/40 bg-warning/10 flex flex-wrap items-center gap-x-4 gap-y-2 rounded-lg border px-4 py-3">
            <AlertTriangle className="text-warning size-5 shrink-0" />
            <div className="min-w-0 flex-1 basis-72">
              <p className="text-[13.5px] font-semibold">
                Hay que actualizar el master «{parent.name}»
              </p>
              <p className="text-muted-foreground mt-0.5 text-[12.5px] leading-relaxed">
                Esta zona tiene {zoneChanges} plano(s) que el master todavía no tiene:{' '}
                <span className="font-mono text-[11.5px]">
                  {[...integration!.changed, ...integration!.removed]
                    .slice(0, 3)
                    .map((p) => fileName(p))
                    .join(', ')}
                  {zoneChanges > 3 ? `, +${zoneChanges - 3}` : ''}
                </span>
                . Al integrar se copian a <span className="font-mono text-[11.5px]">drawings/{project.id}/</span>{' '}
                del master.
              </p>
            </div>
            <Button variant="outline" size="sm" asChild>
              <Link to={`/p/${parent.id}`}>Ver master</Link>
            </Button>
            <IntegrateButton
              zoneId={project.id}
              zoneName={project.name}
              busy={busyId === project.id}
              onIntegrate={integrate}
            >
              Actualizar master
            </IntegrateButton>
          </div>
        </div>
      )}
      {isZone && parent && zoneChanges === 0 && integration && integration.integratedAt > 0 && (
        <div className="px-6 pb-4">
          <div className="text-muted-foreground bg-card flex items-center gap-2 rounded-lg border px-4 py-2 text-[12.5px]">
            <CheckCircle2 className="text-success size-4" />
            El master «{parent.name}» tiene la última versión de esta zona · integrado{' '}
            {timeAgo(integration.integratedAt)}
          </div>
        </div>
      )}
      {!isZone && lateZones.length > 0 && (
        <div className="px-6 pb-4">
          <div className="border-warning/40 bg-warning/10 rounded-lg border">
            <div className="flex items-center gap-2 px-4 pt-3 pb-2 text-[13.5px] font-semibold">
              <AlertTriangle className="text-warning size-4" />
              {lateZones.length === 1
                ? '1 zona tiene cambios que este master todavía no tiene'
                : `${lateZones.length} zonas tienen cambios que este master todavía no tiene`}
            </div>
            <div className="divide-warning/20 divide-y">
              {lateZones.map((z) => (
                <div key={z.id} className="flex items-center gap-3 px-4 py-2">
                  <Link to={`/p/${z.id}`} className="min-w-0 flex-1 text-[13px] font-medium hover:underline">
                    {z.name}
                    <span className="text-muted-foreground font-normal">
                      {' '}
                      · {z.pendingChanges} plano(s) sin integrar
                    </span>
                  </Link>
                  <IntegrateButton
                    zoneId={z.id}
                    zoneName={z.name}
                    busy={busyId === z.id}
                    onIntegrate={integrate}
                  />
                </div>
              ))}
            </div>
          </div>
        </div>
      )}

      <div className="grid grid-cols-2 gap-3 px-6 pb-5 lg:grid-cols-4">
        <Stat label="Planos" value={drawings.length} icon={FileBox} />
        <Stat label="Ramas activas" value={active.length} icon={GitBranch} />
        <Stat label="Integradas" value={done.length} icon={LayoutTemplate} />
        <div className="rounded-lg border bg-card px-4 py-3.5 shadow-xs">
          <div className="text-muted-foreground text-[12.5px] font-medium">
            Repositorio
          </div>
          <div
            className={cn(
              'mt-2 text-[22px] leading-none font-semibold tabular-nums tracking-tight',
              (usage?.repo ?? 0) > 20 * 1024 ** 3 && 'text-warning'
            )}
          >
            {formatBytes(usage?.repo ?? 0)}
          </div>
          <div className="text-muted-foreground mt-1.5 flex items-center gap-1 font-mono text-[10px]">
            <span>main {shortSha(mainHead)}</span>
            <span className="opacity-40">·</span>
            <span>clon {formatBytes(usage?.clone ?? 0)}</span>
          </div>
        </div>
      </div>

      <div className="px-6 pb-10">
        <Tabs defaultValue={tabParam ?? (children.length > 0 ? 'zones' : 'drawings')}>
          <TabsList>
            {!isZone && (
              <TabsTrigger value="zones">
                Subproyectos · {children.length}
              </TabsTrigger>
            )}
            <TabsTrigger value="drawings">Planos · {drawings.length}</TabsTrigger>
            <TabsTrigger value="tasks">Tareas · {active.length}</TabsTrigger>
            <TabsTrigger value="approvers">
              <ShieldCheck /> Aprobadores
            </TabsTrigger>
            <TabsTrigger value="history">Historial</TabsTrigger>
          </TabsList>

          {!isZone && (
            <TabsContent value="zones" className="mt-4">
              {children.length === 0 ? (
                <EmptyState
                  icon={Split}
                  title="Este proyecto no tiene subproyectos"
                  description="Creá una zona o área específica para trabajarla por separado. Cuando cambie, LCS avisará que hay que actualizar este master."
                  action={
                    <Button size="sm" onClick={() => setNewZone(true)}>
                      <Plus className="size-4" /> Nuevo subproyecto
                    </Button>
                  }
                />
              ) : (
                <div className="divide-y overflow-hidden rounded-lg border bg-card shadow-xs">
                  {children.map((z) => (
                    <div key={z.id} className="flex items-center gap-3 px-3.5 py-3">
                      <div className="bg-muted text-muted-foreground grid size-9 shrink-0 place-items-center rounded-md">
                        <Split className="size-4" />
                      </div>
                      <Link to={`/p/${z.id}`} className="min-w-0 flex-1">
                        <div className="truncate text-[13px] font-medium hover:underline">{z.name}</div>
                        <div className="text-muted-foreground tnum mt-0.5 text-[12px]">
                          {planos(z.drawings)}
                          {z.integratedAt ? ` · integrado ${timeAgo(z.integratedAt)}` : ' · nunca integrado'}
                        </div>
                      </Link>
                      {z.pendingChanges > 0 ? (
                        <>
                          <Badge variant="warning" className="tnum">
                            {z.pendingChanges} sin integrar
                          </Badge>
                          <IntegrateButton
                            zoneId={z.id}
                            zoneName={z.name}
                            busy={busyId === z.id}
                            onIntegrate={integrate}
                            size="xs"
                            variant="outline"
                          >
                            Actualizar master
                          </IntegrateButton>
                        </>
                      ) : z.drawings === 0 ? (
                        <Badge variant="muted">Sin planos</Badge>
                      ) : (
                        <Badge variant="success" className="gap-1">
                          <CheckCircle2 /> Al día
                        </Badge>
                      )}
                    </div>
                  ))}
                </div>
              )}
            </TabsContent>
          )}

          <TabsContent value="drawings" className="mt-4">
            {drawings.length === 0 ? (
              <EmptyState
                icon={FileBox}
                title="Todavía no hay planos"
                description="Importá los DWG del proyecto. Cada archivo queda versionado en el repositorio del NAS."
                action={
                  <Button size="sm" onClick={() => setImporting(true)}>
                    <Upload className="size-4" /> Importar DWG
                  </Button>
                }
              />
            ) : (
              <div className="grid gap-2.5 sm:grid-cols-2 xl:grid-cols-3">
                {drawings.map((d: Drawing) => (
                  <Link
                    key={d.id}
                    to={`/p/${projectId}/d/${d.id}`}
                    className="group hover:border-primary/40 rounded-lg border bg-card shadow-xs transition-[border-color,box-shadow] hover:shadow-sm"
                  >
                    <div className="flex items-start gap-3 p-3.5">
                      <div className="bg-muted/50 grid size-9 shrink-0 place-items-center rounded-md border">
                        <FileBox className="text-muted-foreground size-4" />
                      </div>
                      <div className="min-w-0 flex-1">
                        <div className="truncate text-[13px] font-medium">{d.name}</div>
                        <div className="text-muted-foreground mt-0.5 truncate font-mono text-[10px]">
                          {d.path}
                        </div>
                        <div className="text-muted-foreground mt-2 flex items-center gap-2 text-[11px]">
                          <span className="tnum">{formatBytes(d.size)}</span>
                          <span>·</span>
                          <span>{timeAgo(d.updatedAt)}</span>
                          <ArrowUpRight className="ml-auto size-3 opacity-0 transition-opacity group-hover:opacity-60" />
                        </div>
                        {(data.editing?.[d.path]?.length ?? 0) > 0 && (
                          <div
                            className="mt-2"
                            title={data.editing![d.path].map((e) => `${e.author}: ${e.title}`).join('\n')}
                          >
                            <Badge variant="warning" className="gap-1">
                              <PencilLine /> En edición · {data.editing![d.path].map((e) => e.author).join(', ')}
                            </Badge>
                          </div>
                        )}
                      </div>
                    </div>
                  </Link>
                ))}
              </div>
            )}
          </TabsContent>

          <TabsContent value="tasks" className="mt-4">
            {active.length === 0 ? (
              <EmptyState
                icon={GitBranch}
                title="No hay ramas activas"
                description="Cada tarea crea una rama work/… aislada para descargar, editar y subir un plano."
                action={
                  <Button size="sm" onClick={() => setNewTask(true)}>
                    <Plus className="size-4" /> Nueva tarea
                  </Button>
                }
              />
            ) : (
              <div className="divide-y overflow-hidden rounded-lg border bg-card">
                {active.map((b) => (
                  <BranchRow key={b.id} branch={b} projectId={projectId} />
                ))}
              </div>
            )}

            {done.length > 0 && (
              <div className="mt-6">
                <div className="text-muted-foreground px-1 pb-2 text-[11px] font-semibold tracking-[0.14em] uppercase">
                  Cerradas
                </div>
                <div className="divide-y overflow-hidden rounded-lg border bg-card opacity-80">
                  {done.map((b) => (
                    <BranchRow key={b.id} branch={b} projectId={projectId} />
                  ))}
                </div>
              </div>
            )}
          </TabsContent>

          <TabsContent value="approvers" className="mt-4">
            <ApproversEditor projectId={projectId} onSaved={refreshAll} />
          </TabsContent>

          <TabsContent value="history" className="mt-4">
            {data.events.length > 0 ? (
              <ActivityGraph events={data.events} />
            ) : (
              <div className="text-muted-foreground rounded-lg border bg-card px-4 py-6 text-center text-[13px]">
                Sin movimientos.
              </div>
            )}
          </TabsContent>
        </Tabs>
      </div>

      <NewTaskDialog
        open={newTask}
        onOpenChange={setNewTask}
        projectId={projectId}
        drawings={drawings}
        onCreated={(b) => navigate(`/p/${projectId}/t/${b.id}`)}
      />
      <ImportDrawingsDialog
        open={importing}
        onOpenChange={setImporting}
        projectId={projectId}
        onDone={refreshAll}
      />
      {!isZone && (
        <NewProjectDialog
          open={newZone}
          onOpenChange={setNewZone}
          masters={[project]}
          parentId={project.id}
          lockParent
          onCreated={(z) => {
            shell?.reloadProjects?.()
            navigate(`/p/${z.id}`)
          }}
        />
      )}
      <Dialog open={deleting} onOpenChange={(v) => !deleteBusy && setDeleting(v)}>
        <DialogContent className="max-w-md">
          <DialogHeader>
            <DialogTitle>{isZone ? 'Borrar subproyecto' : 'Borrar proyecto'}</DialogTitle>
            <DialogDescription>
              Se elimina <span className="text-foreground font-medium">{project.name}</span> del NAS con
              todo su historial, tareas y aprobaciones.
              {!isZone && children.length > 0
                ? ` También se borran sus ${children.length} subproyecto(s): ${children.map((z) => z.name).join(', ')}.`
                : ''}{' '}
              No se puede deshacer.
            </DialogDescription>
          </DialogHeader>
          <DialogFooter>
            <Button variant="ghost" onClick={() => setDeleting(false)} disabled={deleteBusy}>
              Cancelar
            </Button>
            <Button
              variant="destructive"
              disabled={deleteBusy}
              onClick={async () => {
                setDeleteBusy(true)
                try {
                  await api.deleteProject(projectId)
                  toast.success(isZone ? 'Subproyecto borrado' : 'Proyecto borrado')
                  shell?.reloadProjects?.()
                  navigate(parent ? `/p/${parent.id}` : '/proyectos')
                } catch (err) {
                  toast.error(err instanceof Error ? err.message : 'no pude borrar el proyecto')
                  setDeleteBusy(false)
                }
              }}
            >
              {deleteBusy && <Loader2 className="size-4 animate-spin" />}
              Borrar definitivamente
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
      {conflictDialog}
    </div>
  )
}

function Stat({
  label,
  value,
  icon: Icon
}: {
  label: string
  value: number
  icon: React.ComponentType<{ className?: string }>
}) {
  return (
    <div className="rounded-lg border bg-card px-4 py-3.5 shadow-xs">
      <div className="text-muted-foreground flex items-center gap-2 text-[12.5px] font-medium">
        <Icon className="size-3.5" />
        {label}
      </div>
      <div className="mt-2 text-[26px] leading-none font-semibold tnum tracking-tight">{value}</div>
    </div>
  )
}

export function BranchRow({ branch: b, projectId }: { branch: Branch; projectId?: string }) {
  const pid = projectId ?? b.projectId
  return (
    <Link
      to={`/p/${pid}/t/${b.id}`}
      className="hover:bg-muted/50 flex items-center gap-3 px-3.5 py-3 transition-colors"
    >
      <div className="bg-primary/10 text-primary grid size-8 shrink-0 place-items-center rounded-full text-[11px] font-semibold">
        {initials(b.author)}
      </div>
      <div className="min-w-0 flex-1">
        <div className="flex items-center gap-2">
          <span className="truncate text-[13px] font-medium">{b.title}</span>
          <StatusBadge status={b.status} />
        </div>
        <div className="text-muted-foreground mt-0.5 flex items-center gap-2 truncate text-[12px]">
          <span className="truncate">{b.name}</span>
          {b.files?.length === 1 && (
            <>
              <span>·</span>
              <span className="truncate">{fileName(b.files[0].path)}</span>
            </>
          )}
          {(b.files?.length ?? 0) > 1 && (
            <>
              <span>·</span>
              <span className="shrink-0">{b.files?.length} planos</span>
            </>
          )}
        </div>
      </div>
      <div className="text-muted-foreground flex shrink-0 items-center gap-3 text-[11px]">
        <Badge variant="outline" className="font-mono text-[10px]">
          {shortSha(b.headCommit)}
        </Badge>
        <span className="w-16 text-right">{timeAgo(b.updatedAt)}</span>
      </div>
    </Link>
  )
}
