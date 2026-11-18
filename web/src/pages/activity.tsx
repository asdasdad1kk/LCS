import { useCallback, useEffect, useMemo, useState } from 'react'
import { Link } from 'react-router-dom'
import { Activity, Check, GitBranch, LayoutGrid, Table2 } from 'lucide-react'
import { api, type ActivityEvent, type Project, type TaskRow } from '@/lib/api'
import { Skeleton } from '@/components/ui/separator'
import { PageHeader, EmptyState } from '@/components/page-header'
import { ActivityGraph } from '@/pages/dashboard'
import { StatusBadge } from '@/components/status-badge'
import { ChainDots } from '@/components/approval-chain'
import { EventTrack, StageTrack } from '@/components/task-track'
import { initials, timeAgo } from '@/lib/format'
import { ChangesChip } from '@/components/changes-chip'
import { cn } from '@/lib/utils'

type View = 'graph' | 'table'
type Scope = 'all' | 'active' | 'done'

function stored<T extends string>(key: string, allowed: T[], fallback: T): T {
  try {
    const v = localStorage.getItem(key) as T | null
    if (v && allowed.includes(v)) return v
  } catch {
    /* sin almacenamiento */
  }
  return fallback
}

function save(key: string, value: string) {
  try {
    localStorage.setItem(key, value)
  } catch {
    /* ignore */
  }
}

export function ActivityPage() {
  const [projects, setProjects] = useState<Project[]>([])
  const [selected, setSelected] = useState<string[]>(() => {
    try {
      return JSON.parse(localStorage.getItem('lcs-activity-projects') ?? '[]')
    } catch {
      return []
    }
  })
  const [view, setView] = useState<View>(() => stored('lcs-activity-view', ['graph', 'table'], 'table'))
  const [scope, setScope] = useState<Scope>('all')
  const [events, setEvents] = useState<ActivityEvent[]>([])
  const [rows, setRows] = useState<TaskRow[]>([])
  const [loading, setLoading] = useState(true)

  useEffect(() => {
    api.projects().then(setProjects).catch(() => {})
  }, [])

  const toggle = (id: string) =>
    setSelected((cur) => {
      const next = cur.includes(id) ? cur.filter((x) => x !== id) : [...cur, id]
      save('lcs-activity-projects', JSON.stringify(next))
      return next
    })

  const load = useCallback(async () => {
    try {
      if (view === 'graph') setEvents(await api.activity(selected, 200))
      else setRows(await api.timeline(selected))
    } finally {
      setLoading(false)
    }
  }, [view, selected])

  // Otras personas suben y aprueban: la tabla se mantiene al día sola.
  useEffect(() => {
    setLoading(true)
    void load()
    const timer = window.setInterval(load, 15000)
    window.addEventListener('focus', load)
    return () => {
      window.clearInterval(timer)
      window.removeEventListener('focus', load)
    }
  }, [load])

  const shown = useMemo(
    () =>
      rows.filter((r) =>
        scope === 'all' ? true : scope === 'active' ? r.stage !== 'merged' && r.stage !== 'closed' : r.stage === 'merged' || r.stage === 'closed'
      ),
    [rows, scope]
  )

  return (
    <div className="scrollbar-thin flex-1 overflow-y-auto">
      <PageHeader
        eyebrow={
          <>
            <Activity className="size-3.5" /> seguimiento
          </>
        }
        title="Actividad"
        description="En qué paso va cada tarea de cada proyecto: quién la abrió, qué se subió, quién aprobó y qué se integró."
        actions={
          <div className="bg-muted inline-flex rounded-md p-0.5">
            {(
              [
                ['table', Table2, 'Tabla'],
                ['graph', LayoutGrid, 'Eventos']
              ] as const
            ).map(([v, Icon, label]) => (
              <button
                key={v}
                type="button"
                onClick={() => {
                  setView(v)
                  save('lcs-activity-view', v)
                }}
                className={cn(
                  'inline-flex h-7 items-center gap-1.5 rounded-sm px-2.5 text-[13px] font-medium transition-all',
                  view === v ? 'bg-card text-foreground shadow-xs' : 'text-muted-foreground'
                )}
              >
                <Icon className="size-3.5" /> {label}
              </button>
            ))}
          </div>
        }
      />

      <div className="px-6 pb-3">
        <div className="flex flex-wrap items-center gap-1.5">
          <span className="text-muted-foreground mr-1 text-[11px] font-semibold tracking-wider uppercase">Proyectos</span>
          <button
            type="button"
            onClick={() => {
              setSelected([])
              save('lcs-activity-projects', '[]')
            }}
            className={cn(
              'rounded-full border px-2.5 py-1 text-[12px] transition-colors',
              selected.length === 0 ? 'bg-primary text-primary-foreground border-primary' : 'bg-card hover:border-primary/50'
            )}
          >
            Todos
          </button>
          {projects.map((p) => {
            const on = selected.includes(p.id)
            return (
              <button
                key={p.id}
                type="button"
                onClick={() => toggle(p.id)}
                className={cn(
                  'inline-flex items-center gap-1 rounded-full border px-2.5 py-1 text-[12px] transition-colors',
                  on ? 'bg-primary text-primary-foreground border-primary' : 'bg-card hover:border-primary/50'
                )}
              >
                {on && <Check className="size-3" strokeWidth={3} />}
                {p.parentId && <GitBranch className="size-3 opacity-60" />}
                {p.name}
              </button>
            )
          })}
          {view === 'table' && (
            <div className="bg-muted ml-auto inline-flex rounded-md p-0.5">
              {(
                [
                  ['all', 'Todas'],
                  ['active', 'Activas'],
                  ['done', 'Cerradas']
                ] as const
              ).map(([v, label]) => (
                <button
                  key={v}
                  type="button"
                  onClick={() => setScope(v)}
                  className={cn(
                    'h-6 rounded-sm px-2 text-[12px] font-medium transition-all',
                    scope === v ? 'bg-card text-foreground shadow-xs' : 'text-muted-foreground'
                  )}
                >
                  {label}
                </button>
              ))}
            </div>
          )}
        </div>
      </div>

      <div className="px-6 pb-10">
        {loading ? (
          <div className="space-y-2">
            {[0, 1, 2, 3, 4].map((i) => (
              <Skeleton key={i} className="h-14 w-full" />
            ))}
          </div>
        ) : view === 'graph' ? (
          events.length === 0 ? (
            <EmptyState icon={Activity} title="Sin movimientos" description="No hay eventos para esta selección." />
          ) : (
            <ActivityGraph events={events} />
          )
        ) : shown.length === 0 ? (
          <EmptyState icon={Activity} title="Sin tareas" description="No hay tareas para esta selección." />
        ) : (
          <TaskTable rows={shown} showProject={selected.length !== 1} />
        )}
      </div>
    </div>
  )
}

function TaskTable({ rows, showProject }: { rows: TaskRow[]; showProject: boolean }) {
  return (
    <div className="bg-card overflow-x-auto rounded-lg border shadow-xs">
      <table className="w-full min-w-[980px] border-collapse text-left">
        <thead>
          <tr className="text-muted-foreground border-b text-[11px] font-semibold tracking-wider uppercase">
            <th className="px-4 py-2.5">Tarea</th>
            {showProject && <th className="px-3 py-2.5">Proyecto</th>}
            <th className="px-3 py-2.5">Estado</th>
            <th className="px-3 py-2.5">Avance</th>
            <th className="px-3 py-2.5">Aprobadores</th>
            <th className="px-3 py-2.5">Línea de tiempo</th>
            <th className="px-4 py-2.5 text-right">Actualizada</th>
          </tr>
        </thead>
        <tbody className="divide-y">
          {rows.map((r) => {
            const b = r.branch
            return (
              <tr key={b.id} className="hover:bg-muted/40 align-middle transition-colors">
                <td className="px-4 py-3">
                  <Link to={`/p/${b.projectId}/t/${b.id}`} className="flex items-center gap-2.5">
                    <span className="bg-primary/10 text-primary grid size-8 shrink-0 place-items-center rounded-full text-[10.5px] font-semibold">
                      {initials(b.author)}
                    </span>
                    <span className="min-w-0">
                      <span className="block max-w-56 truncate text-[13px] font-medium hover:underline">{b.title}</span>
                      <span className="text-muted-foreground block truncate text-[11.5px]">
                        {b.author}
                        {r.files.length > 0 && ` · ${r.files.length} ${r.files.length === 1 ? 'plano' : 'planos'}`}
                      </span>
                    </span>
                  </Link>
                </td>
                {showProject && (
                  <td className="px-3 py-3 text-[12.5px]">
                    <Link to={`/p/${b.projectId}`} className="hover:underline">
                      {b.projectName}
                    </Link>
                  </td>
                )}
                <td className="px-3 py-3">
                  <div className="flex flex-col items-start gap-1">
                    <StatusBadge status={b.status} />
                    <ChangesChip changes={r.changes} />
                  </div>
                </td>
                <td className="px-3 py-3">
                  <StageTrack stage={r.stage} />
                </td>
                <td className="px-3 py-3">{r.nodes.length > 0 ? <ChainDots nodes={r.nodes} /> : <span className="text-muted-foreground text-[11.5px]">sin cadena</span>}</td>
                <td className="px-3 py-3">
                  <EventTrack items={r.timeline} />
                </td>
                <td className="text-muted-foreground px-4 py-3 text-right text-[12px] whitespace-nowrap">{timeAgo(b.updatedAt)}</td>
              </tr>
            )
          })}
        </tbody>
      </table>
    </div>
  )
}
