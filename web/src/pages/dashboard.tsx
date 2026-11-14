import { useEffect, useMemo, useState } from 'react'
import { Link } from 'react-router-dom'
import {
  ArrowUpRight,
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

      <div className="grid grid-cols-2 gap-3 px-6 pb-6 lg:grid-cols-4">
        <Metric icon={Boxes} label="Proyectos" value={totals.drawings ? projects.length : projects.length} />
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
            <div className="divide-y overflow-hidden rounded-lg border bg-card">
              {branches.map((b) => (
                <Link
                  key={b.id}
                  to={`/p/${b.projectId}/t/${b.id}`}
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
            <div className="divide-y overflow-hidden rounded-lg border bg-card">
              {events.slice(0, 6).map((e) => (
                <ActivityRow key={e.id} event={e} />
              ))}
              {events.length === 0 && (
                <div className="text-muted-foreground px-4 py-6 text-center text-[13px]">
                  Sin movimientos todavía.
                </div>
              )}
            </div>
          </div>
        </section>

        <section>
          <SectionTitle>Proyectos</SectionTitle>
          <div className="grid gap-2.5">
            {projects.map((p) => (
              <Link key={p.id} to={`/p/${p.id}`}>
                <Card className="hover:border-primary/40 transition-colors">
                  <CardHeader className="pb-2">
                    <CardTitle className="flex items-center justify-between gap-2">
                      <span className="truncate">{p.name}</span>
                      <Badge variant="outline" className="font-mono text-[10px]">
                        {p.drawings ?? 0} dwg
                      </Badge>
                    </CardTitle>
                    {p.description && (
                      <p className="text-muted-foreground line-clamp-2 text-[12px] leading-relaxed">
                        {p.description}
                      </p>
                    )}
                  </CardHeader>
                  <CardContent className="text-muted-foreground flex items-center gap-3 text-[11px]">
                    <span className="font-mono">{p.id}</span>
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
              <div className="mb-1 text-[10px] font-semibold tracking-[0.14em] uppercase">
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
    <div className="rounded-lg border bg-card px-4 py-3">
      <div className="text-muted-foreground flex items-center gap-1.5 text-[11px] font-medium tracking-wide uppercase">
        <Icon className="size-3.5" />
        {label}
      </div>
      <div
        className={
          'mt-1.5 text-2xl font-semibold tnum tracking-tight ' +
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
  'branch.closed': 'descartó una tarea'
}

export function ActivityRow({ event }: { event: ActivityEvent }) {
  let payload: any = {}
  try {
    payload = JSON.parse(event.payload)
  } catch {
    /* ignore */
  }
  const detail =
    payload.title ?? payload.branch ?? payload.name ?? (payload.count ? `${payload.count} archivos` : '')
  return (
    <div className="flex items-start gap-3 px-3.5 py-2.5">
      <div className="mt-1 size-1.5 shrink-0 rounded-full bg-primary/60" />
      <div className="min-w-0 flex-1">
        <p className="text-[13px] leading-snug">
          <span className="font-medium">{event.actor}</span>{' '}
          <span className="text-muted-foreground">
            {eventLabels[event.kind] ?? event.kind.replace(/\./g, ' ')}
          </span>
          {detail && <span className="text-foreground font-medium"> · {detail}</span>}
        </p>
        <p className="text-muted-foreground mt-0.5 font-mono text-[10px]">
          {timeAgo(event.createdAt)}
          {payload.size ? ` · ${formatBytes(payload.size)}` : ''}
        </p>
      </div>
    </div>
  )
}
