import { useCallback, useEffect, useMemo, useState } from 'react'
import { Link, useNavigate, useParams } from 'react-router-dom'
import {
  ArrowLeft,
  ArrowUpRight,
  FileBox,
  GitBranch,
  LayoutTemplate,
  Plus,
  RefreshCw,
  Upload
} from 'lucide-react'
import { toast } from 'sonner'
import { api, type ActivityEvent, type Branch, type Drawing, type ProjectDetail } from '@/lib/api'
import { Button } from '@/components/ui/button'
import { Badge } from '@/components/ui/badge'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { Skeleton } from '@/components/ui/separator'
import { PageHeader, EmptyState } from '@/components/page-header'
import { StatusBadge } from '@/components/status-badge'
import { ActivityRow } from '@/pages/dashboard'
import { ImportDrawingsDialog, NewTaskDialog } from '@/components/dialogs'
import { formatBytes, fileName, initials, shortSha, timeAgo } from '@/lib/format'
import { cn } from '@/lib/utils'

export function ProjectPage() {
  const { projectId = '' } = useParams()
  const navigate = useNavigate()
  const [data, setData] = useState<ProjectDetail | null>(null)
  const [loading, setLoading] = useState(true)
  const [newTask, setNewTask] = useState(false)
  const [importing, setImporting] = useState(false)
  const [syncing, setSyncing] = useState(false)

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

  const { project, drawings, mainHead, usage } = data

  return (
    <div className="scrollbar-thin flex-1 overflow-y-auto">
      <PageHeader
        eyebrow={
          <>
            <Link to="/" className="hover:text-foreground inline-flex items-center gap-1">
              <ArrowLeft className="size-3" /> panel
            </Link>
            <span className="opacity-40">/</span>
            <span className="font-mono text-[10px] normal-case tracking-normal">{project.id}</span>
          </>
        }
        title={project.name}
        description={project.description || 'Repositorio de planos gestionado con git.'}
        actions={
          <>
            <Button variant="outline" size="sm" onClick={sync} disabled={syncing}>
              <RefreshCw className={syncing ? 'size-4 animate-spin' : 'size-4'} /> Sincronizar
            </Button>
            <Button variant="outline" size="sm" onClick={() => setImporting(true)}>
              <Upload className="size-4" /> Importar
            </Button>
            <Button size="sm" onClick={() => setNewTask(true)}>
              <Plus className="size-4" /> Nueva tarea
            </Button>
          </>
        }
      />

      <div className="grid grid-cols-2 gap-3 px-6 pb-5 lg:grid-cols-4">
        <Stat label="Planos" value={drawings.length} icon={FileBox} />
        <Stat label="Ramas activas" value={active.length} icon={GitBranch} />
        <Stat label="Integradas" value={done.length} icon={LayoutTemplate} />
        <div className="rounded-lg border bg-card px-4 py-3">
          <div className="text-muted-foreground text-[11px] font-medium tracking-wide uppercase">
            Repositorio
          </div>
          <div
            className={cn(
              'mt-1.5 text-lg leading-none font-semibold tabular-nums',
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
        <Tabs defaultValue="drawings">
          <TabsList>
            <TabsTrigger value="drawings">Planos · {drawings.length}</TabsTrigger>
            <TabsTrigger value="tasks">Tareas · {active.length}</TabsTrigger>
            <TabsTrigger value="history">Historial</TabsTrigger>
          </TabsList>

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
                    className="group hover:border-primary/40 rounded-lg border bg-card transition-colors"
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
                          <span className="font-mono">{formatBytes(d.size)}</span>
                          <span>·</span>
                          <span>{timeAgo(d.updatedAt)}</span>
                          <ArrowUpRight className="ml-auto size-3 opacity-0 transition-opacity group-hover:opacity-60" />
                        </div>
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

          <TabsContent value="history" className="mt-4">
            <div className="divide-y overflow-hidden rounded-lg border bg-card">
              {data.events.map((e: ActivityEvent) => (
                <ActivityRow key={e.id} event={e} />
              ))}
              {data.events.length === 0 && (
                <div className="text-muted-foreground px-4 py-6 text-center text-[13px]">
                  Sin movimientos.
                </div>
              )}
            </div>
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
        onDone={load}
      />
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
    <div className="rounded-lg border bg-card px-4 py-3">
      <div className="text-muted-foreground flex items-center gap-1.5 text-[11px] font-medium tracking-wide uppercase">
        <Icon className="size-3.5" />
        {label}
      </div>
      <div className="mt-1.5 text-2xl font-semibold tnum tracking-tight">{value}</div>
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
      <div className="grid size-8 shrink-0 place-items-center rounded-md border bg-muted/40 font-mono text-[10px] font-bold">
        {initials(b.author)}
      </div>
      <div className="min-w-0 flex-1">
        <div className="flex items-center gap-2">
          <span className="truncate text-[13px] font-medium">{b.title}</span>
          <StatusBadge status={b.status} />
        </div>
        <div className="text-muted-foreground mt-0.5 flex items-center gap-2 truncate font-mono text-[11px]">
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
