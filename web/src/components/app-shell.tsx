import { Link, NavLink, Outlet, useLocation, useNavigate } from 'react-router-dom'
import { useEffect, useState } from 'react'
import {
  Activity,
  Boxes,
  GitBranch,
  Moon,
  Plus,
  RefreshCw,
  Sun,
  Waypoints
} from 'lucide-react'
import { api, type Project } from '@/lib/api'
import { Button } from '@/components/ui/button'
import { Badge } from '@/components/ui/badge'
import { cn } from '@/lib/utils'
import { useTheme } from '@/components/theme-provider'
import { NewProjectDialog } from '@/components/new-project-dialog'
import { toast } from 'sonner'

export function AppShell() {
  const [projects, setProjects] = useState<Project[]>([])
  const [loading, setLoading] = useState(true)
  const [newProject, setNewProject] = useState(false)
  const { theme, toggle } = useTheme()
  const location = useLocation()
  const navigate = useNavigate()

  const load = async () => {
    try {
      setProjects(await api.projects())
    } catch (err) {
      toast.error(err instanceof Error ? err.message : 'no pude cargar los proyectos')
    } finally {
      setLoading(false)
    }
  }

  useEffect(() => {
    load()
  }, [location.pathname])

  const activeProjectId = location.pathname.match(/^\/p\/([^/]+)/)?.[1]

  return (
    <div className="flex h-full">
      <aside className="bg-card/60 flex w-[248px] shrink-0 flex-col border-r">
        <div className="flex h-14 items-center gap-2.5 border-b px-4">
          <Link to="/" className="flex items-center gap-2.5">
            <span className="bg-primary text-primary-foreground grid size-7 place-items-center rounded-md font-mono text-[11px] font-bold tracking-tight">
              LC
            </span>
            <span className="flex flex-col leading-none">
              <span className="text-[13px] font-semibold tracking-tight">LCS</span>
              <span className="text-muted-foreground text-[10px] tracking-[0.14em] uppercase">
                layout control
              </span>
            </span>
          </Link>
        </div>

        <nav className="flex flex-col gap-0.5 px-2 py-3">
          <NavItem to="/" icon={Boxes} label="Panel" end />
          <NavItem to="/tareas" icon={Waypoints} label="Tareas" />
          <NavItem to="/actividad" icon={Activity} label="Actividad" />
        </nav>

        <div className="flex items-center justify-between px-4 pt-2 pb-1.5">
          <span className="text-muted-foreground text-[10px] font-semibold tracking-[0.12em] uppercase">
            Proyectos
          </span>
          <Button
            variant="ghost"
            size="icon-sm"
            className="text-muted-foreground hover:text-foreground"
            onClick={() => setNewProject(true)}
          >
            <Plus className="size-3.5" />
          </Button>
        </div>

        <div className="scrollbar-thin flex-1 overflow-y-auto px-2 pb-3">
          {loading && <div className="text-muted-foreground px-2 py-1 text-xs">cargando…</div>}
          {!loading && projects.length === 0 && (
            <button
              onClick={() => setNewProject(true)}
              className="text-muted-foreground hover:text-foreground hover:bg-accent w-full rounded-md border border-dashed px-2 py-3 text-left text-xs"
            >
              Sin proyectos. Creá el primero →
            </button>
          )}
          {projects.map((p) => (
            <Link
              key={p.id}
              to={`/p/${p.id}`}
              className={cn(
                'group flex items-center gap-2 rounded-md px-2 py-1.5 text-[13px] transition-colors',
                activeProjectId === p.id
                  ? 'bg-accent text-accent-foreground font-medium'
                  : 'text-muted-foreground hover:bg-accent/60 hover:text-foreground'
              )}
            >
              <GitBranch className="size-3.5 shrink-0 opacity-70" />
              <span className="truncate">{p.name}</span>
              {!!p.openBranches && (
                <Badge variant="muted" className="ml-auto px-1 text-[10px]">
                  {p.openBranches}
                </Badge>
              )}
            </Link>
          ))}
        </div>

        <div className="border-t p-2">
          <div className="flex items-center gap-1">
            <Button
              variant="ghost"
              size="sm"
              className="text-muted-foreground hover:text-foreground flex-1 justify-start gap-2 px-2 text-xs"
              onClick={async () => {
                try {
                  const b = await api.bootstrap()
                  toast.success('Conectado', { description: b.actor })
                } catch {
                  toast.error('Sin conexión con el servicio')
                }
              }}
            >
              <span className="relative flex size-2">
                <span className="bg-success absolute inline-flex size-2 rounded-full opacity-60" />
                <span className="bg-success relative inline-flex size-2 rounded-full" />
              </span>
              local
            </Button>
            <Button variant="ghost" size="icon-sm" onClick={toggle} title="Cambiar tema">
              {theme === 'dark' ? <Sun className="size-3.5" /> : <Moon className="size-3.5" />}
            </Button>
            <Button
              variant="ghost"
              size="icon-sm"
              title="Sincronizar proyectos"
              onClick={async () => {
                const tid = toast.loading('Sincronizando con el NAS…')
                try {
                  await Promise.all(projects.map((p) => api.syncProject(p.id)))
                  await load()
                  toast.success('Sincronizado', { id: tid })
                } catch (err) {
                  toast.error(err instanceof Error ? err.message : 'falló la sincronización', {
                    id: tid
                  })
                }
              }}
            >
              <RefreshCw className="size-3.5" />
            </Button>
          </div>
        </div>
      </aside>

      <main className="flex min-w-0 flex-1 flex-col overflow-hidden">
        <Outlet context={{ reloadProjects: load }} />
      </main>

      <NewProjectDialog
        open={newProject}
        onOpenChange={setNewProject}
        onCreated={(p) => {
          load()
          navigate(`/p/${p.id}`)
        }}
      />
    </div>
  )
}

function NavItem({
  to,
  icon: Icon,
  label,
  end
}: {
  to: string
  icon: React.ComponentType<{ className?: string }>
  label: string
  end?: boolean
}) {
  return (
    <NavLink
      to={to}
      end={end}
      className={({ isActive }) =>
        cn(
          'flex items-center gap-2.5 rounded-md px-2.5 py-1.5 text-[13px] transition-colors',
          isActive
            ? 'bg-accent text-accent-foreground font-medium'
            : 'text-muted-foreground hover:bg-accent/60 hover:text-foreground'
        )
      }
    >
      <Icon className="size-4" />
      {label}
    </NavLink>
  )
}
