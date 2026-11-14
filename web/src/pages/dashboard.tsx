import { useEffect, useMemo, useState } from 'react'
import { cn } from '@/lib/utils'
import { Link } from 'react-router-dom'
import {
  ArrowUpRight,
  Check,
  GitCommitHorizontal,
  GitMerge,
  RotateCcw,
  Trash2,
  X,
  type LucideIcon,
  Boxes,
  FileBox,
  GitBranch,
  LayoutTemplate,
  Plus,
  Waypoints
} from 'lucide-react'
import { api, type ActivityEvent, type Branch, type Project } from '@/lib/api'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Button } from '@/components/ui/button'
import { Badge } from '@/components/ui/badge'
import { Skeleton } from '@/components/ui/separator'
import { PageHeader, EmptyState, SectionTitle } from '@/components/page-header'
import { StatusBadge } from '@/components/status-badge'
import { NewProjectDialog } from '@/components/new-project-dialog'
import { formatBytes, fileName, initials, shortSha, timeAgo } from '@/lib/format'
import { useNavigate } from 'react-router-dom'

type BranchRow = Branch & { projectName: string }

export function DashboardPage() {
  const [projects, setProjects] = useState<Project[]>([])
  const [branches, setBranches] = useState<BranchRow[]>([])
  const [events, setEvents] = useState<ActivityEvent[]>([])
  const [loading, setLoading] = useState(true)
  const [newProject, setNewProject] = useState(false)
  const [workspace, setWorkspace] = useState('')
  const navigate = useNavigate()

  useEffect(() => {
    let alive = true
    ;(async () => {
      try {
        const [p, b, e, boot] = await Promise.all([
          api.projects(),
          fetch('/api/branches?status=active').then((r) => r.json()),
          api.activity(),
          api.bootstrap()
        ])
        if (!alive) return
        setProjects(p)
        setBranches(b ?? [])
        setEvents(e)
        setWorkspace(boot.config?.workspace ?? '')
      } finally {
        if (alive) setLoading(false)
      }
    })()
    return () => {
      alive = false
    }
  }, [])

  const totals = useMemo(
    () => ({
      drawings: projects.reduce((n, p) => n + (p.drawings ?? 0), 0),
      open: branches.length,
      review: branches.filter((b) => b.status === 'review').length
    }),
    [projects, branches]
  )

  return (
    <div className="scrollbar-thin flex-1 overflow-y-auto">
      <PageHeader
        eyebrow={
          <>
            <LayoutTemplate className="size-3.5" /> panel de control
          </>
        }
        title="Layout Control System"
        description="Ramas de trabajo sobre DWG: se descarga, se edita en AutoCAD y se vuelve a subir. Todo versionado en el NAS."
        actions={
          <>
            <Button variant="outline" size="sm" onClick={() => navigate('/tareas')}>
              <Waypoints className="size-4" /> Tareas
            </Button>
            <Button size="sm" onClick={() => setNewProject(true)}>
              <Plus className="size-4" /> Nuevo proyecto
            </Button>
          </>
        }
      />

      <div className="grid grid-cols-2 gap-3 px-6 pb-7 lg:grid-cols-4">
        <Metric icon={Boxes} label="Proyectos" value={projects.length} />
        <Metric icon={FileBox} label="Planos" value={totals.drawings} />
        <Metric icon={GitBranch} label="Ramas activas" value={totals.open} />
        <Metric icon={LayoutTemplate} label="Para revisar" value={totals.review} tone="warning" />
      </div>

      <div className="grid gap-6 px-6 pb-10 lg:grid-cols-[1.35fr_1fr]">
        <section>
          <SectionTitle
            action={
              <Button variant="ghost" size="xs" onClick={() => navigate('/tareas')}>
                Ver todas <ArrowUpRight className="size-3" />
              </Button>
            }
          >
            Tareas en curso
          </SectionTitle>

          {loading ? (
            <div className="space-y-2">
              {[0, 1, 2].map((i) => (
                <Skeleton key={i} className="h-16 w-full" />
              ))}
            </div>
          ) : branches.length === 0 ? (
            <EmptyState
              icon={GitBranch}
              title="No hay ramas abiertas"
              description="Abrí un plano y creá una tarea para descargarlo, editarlo y volver a subirlo."
              action={
                projects[0] ? (
                  <Button size="sm" variant="outline" asChild>
                    <Link to={`/p/${projects[0].id}`}>Ir a {projects[0].name}</Link>
                  </Button>
                ) : undefined
              }
            />
          ) : (
            <div className="divide-y overflow-hidden rounded-lg border bg-card shadow-xs">
              {branches.map((b) => (
                <Link
                  key={b.id}
                  to={`/p/${b.projectId}/t/${b.id}`}
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
                      <span className="truncate">{b.projectName}</span>
                      <span>·</span>
                      <span>{(b.files ?? []).map((f) => fileName(f.path)).join(', ') || '—'}</span>
                    </div>
                  </div>
                  <span className="text-muted-foreground shrink-0 text-[11px]">{timeAgo(b.updatedAt)}</span>
                </Link>
              ))}
            </div>
          )}

          <div className="mt-8">
            <SectionTitle
              action={
                <Button variant="ghost" size="xs" asChild>
                  <Link to="/actividad">
                    Historial <ArrowUpRight className="size-3" />
                  </Link>
                </Button>
              }
            >
              Actividad reciente
            </SectionTitle>
            {events.length > 0 ? (
              <ActivityGraph events={events.slice(0, 6)} />
            ) : (
              <div className="text-muted-foreground rounded-lg border bg-card px-4 py-6 text-center text-[13px]">
                Sin movimientos todavía.
              </div>
            )}
          </div>
        </section>

        <section>
          <SectionTitle
            action={
              <Button variant="ghost" size="xs" asChild>
                <Link to="/proyectos">
                  Ver todos <ArrowUpRight className="size-3" />
                </Link>
              </Button>
            }
          >
            Proyectos
          </SectionTitle>
          <div className="grid gap-2.5">
            {projects.filter((p) => !p.parentId).map((p) => (
              <Link key={p.id} to={`/p/${p.id}`}>
                <Card className="hover:border-primary/40 transition-colors">
                  <CardHeader className="pb-2">
                    <CardTitle className="flex items-center justify-between gap-2">
                      <span className="truncate">{p.name}</span>
                      <span className="flex shrink-0 items-center gap-1.5">
                        {!!p.pendingZones && (
                          <Badge variant="warning" className="tnum">
                            {p.pendingZones} zona(s) atrasada(s)
                          </Badge>
                        )}
                        {!!p.zones && (
                          <Badge variant="default" className="tnum">
                            {p.zones} zonas
                          </Badge>
                        )}
                        <Badge variant="outline" className="tnum">
                          {p.drawings ?? 0} dwg
                        </Badge>
                      </span>
                    </CardTitle>
                    {p.description && (
                      <p className="text-muted-foreground line-clamp-2 text-[12px] leading-relaxed">
                        {p.description}
                      </p>
                    )}
                  </CardHeader>
                  <CardContent className="text-muted-foreground flex items-center gap-3 text-[11px]">
                    <span className="font-mono text-[11px]">{p.id}</span>
                    <span className="ml-auto">{timeAgo(p.updatedAt)}</span>
                  </CardContent>
                </Card>
              </Link>
            ))}
            {!loading && projects.length === 0 && (
              <EmptyState
                icon={Boxes}
                title="Sin proyectos"
                description="Creá un proyecto para inicializar el repositorio git en el NAS."
                action={
                  <Button size="sm" onClick={() => setNewProject(true)}>
                    <Plus className="size-4" /> Crear proyecto
                  </Button>
                }
              />
            )}
          </div>

          {workspace && (
            <div className="text-muted-foreground mt-6 rounded-lg border border-dashed px-3.5 py-3">
              <div className="mb-1 text-[11px] font-medium tracking-[0.06em] uppercase">
                Workspace
              </div>
              <div className="break-all font-mono text-[11px]">{workspace}</div>
            </div>
          )}
        </section>
      </div>

      <NewProjectDialog
        open={newProject}
        onOpenChange={setNewProject}
        onCreated={(p) => navigate(`/p/${p.id}`)}
      />
    </div>
  )
}

function Metric({
  icon: Icon,
  label,
  value,
  tone
}: {
  icon: React.ComponentType<{ className?: string }>
  label: string
  value: number
  tone?: 'warning'
}) {
  return (
    <div className="rounded-lg border bg-card px-4 py-3.5 shadow-xs">
      <div className="text-muted-foreground flex items-center gap-2 text-[12.5px] font-medium">
        <Icon className="size-3.5" />
        {label}
      </div>
      <div
        className={
          'mt-2 text-[26px] leading-none font-semibold tnum tracking-tight ' +
          (tone === 'warning' && value > 0 ? 'text-warning' : '')
        }
      >
        {value}
      </div>
    </div>
  )
}

const eventLabels: Record<string, string> = {
  'project.created': 'creó el proyecto',
  'drawings.imported': 'importó planos',
  'branch.created': 'abrió una tarea',
  'branch.uploaded': 'subió cambios',
  'branch.merged': 'integró una tarea',
  'branch.closed': 'descartó una tarea',
  'drawing.restored': 'restauró un plano',
  'subproject.created': 'creó el subproyecto',
  'subproject.integrated': 'actualizó el master con la zona',
  'branch.approved': 'aprobó una tarea',
  'branch.rejected': 'rechazó una tarea',
  'project.approvers': 'actualizó los aprobadores',
  'project.discovered': 'encontró el proyecto en la carpeta compartida'
}

const graphKind: Record<string, { icon: LucideIcon; dot: string }> = {
  'branch.merged': { icon: GitMerge, dot: 'bg-success text-white' },
  'subproject.integrated': { icon: GitMerge, dot: 'bg-success text-white' },
  'branch.approved': { icon: Check, dot: 'bg-success text-white' },
  'branch.rejected': { icon: X, dot: 'bg-destructive text-white' },
  'branch.closed': { icon: Trash2, dot: 'bg-muted text-muted-foreground' },
  'branch.uploaded': { icon: GitCommitHorizontal, dot: 'bg-primary text-primary-foreground' },
  'drawing.restored': { icon: RotateCcw, dot: 'bg-primary text-primary-foreground' }
}
const graphDefault = { icon: GitBranch, dot: 'bg-muted text-muted-foreground' }

/** Historial al estilo árbol de commits (GitHub / Azure DevOps): riel vertical con nodos. */
export function ActivityGraph({ events }: { events: ActivityEvent[] }) {
  return (
    <ol className="bg-card rounded-lg border px-3.5 py-3 shadow-xs">
      {events.map((e, i) => (
        <ActivityRow key={e.id} event={e} last={i === events.length - 1} />
      ))}
    </ol>
  )
}

export function ActivityRow({ event, last = true }: { event: ActivityEvent; last?: boolean }) {
  let payload: any = {}
  try {
    payload = JSON.parse(event.payload)
  } catch {
    /* ignore */
  }
  const detail =
    payload.title ??
    payload.branch ??
    payload.zone ??
    payload.name ??
    payload.drawing ??
    (payload.count ? `${payload.count} archivos` : '')
  const sha: string | undefined = typeof payload.commit === 'string' ? payload.commit : undefined
  const g = graphKind[event.kind] ?? graphDefault
  const Icon = g.icon
  return (
    <li className={cn('relative flex gap-3', last ? 'pb-0' : 'pb-4')}>
      {!last && <span className="bg-border absolute top-6 bottom-0 left-[11px] w-0.5" />}
      <span className={cn('ring-card relative z-10 grid size-6 shrink-0 place-items-center rounded-full ring-4', g.dot)}>
        <Icon className="size-3.5" />
      </span>
      <div className="min-w-0 flex-1 pt-0.5">
        <p className="text-[13px] leading-snug">
          <span className="font-medium">{event.actor}</span>{' '}
          <span className="text-muted-foreground">
            {eventLabels[event.kind] ?? event.kind.replace(/\./g, ' ')}
          </span>
          {detail && <span className="text-foreground font-medium"> · {detail}</span>}
        </p>
        <p className="text-muted-foreground tnum mt-0.5 flex items-center gap-2 text-[11.5px]">
          {timeAgo(event.createdAt)}
          {payload.size ? <span>· {formatBytes(payload.size)}</span> : null}
          {sha && <span className="bg-muted rounded px-1.5 font-mono text-[10.5px]">{shortSha(sha)}</span>}
        </p>
      </div>
    </li>
  )
}
