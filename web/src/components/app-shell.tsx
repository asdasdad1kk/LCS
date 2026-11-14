import { Link, NavLink, Outlet, useLocation, useNavigate } from 'react-router-dom'
import { useCallback, useEffect, useState } from 'react'
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
import { Separator } from '@/components/ui/separator'
import {
  Sidebar,
  SidebarContent,
  SidebarFooter,
  SidebarGroup,
  SidebarGroupAction,
  SidebarGroupLabel,
  SidebarHeader,
  SidebarInset,
  SidebarMenu,
  SidebarMenuButton,
  SidebarProvider,
  SidebarTrigger,
  useSidebar
} from '@/components/ui/sidebar'
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

  const load = useCallback(async () => {
    try {
      setProjects(await api.projects())
    } catch (err) {
      toast.error(err instanceof Error ? err.message : 'no pude cargar los proyectos')
    } finally {
      setLoading(false)
    }
  }, [])

  useEffect(() => {
    void load()
  }, [load])

  const activeProjectId = location.pathname.match(/^\/p\/([^/]+)/)?.[1]

  return (
    <SidebarProvider>
      <div className="app-frame flex h-full min-w-0">
        <Sidebar variant="sidebar" collapsible="icon">
          <SidebarHeader>
            <Link to="/" className="flex min-w-0 items-center gap-2.5">
              <span className="brand-mark grid size-8 shrink-0 place-items-center rounded-lg font-mono text-[11px] font-bold tracking-tight">
                LC
              </span>
              <span className="sidebar-label flex min-w-0 flex-col leading-none">
                <span className="text-[13px] font-semibold tracking-tight">LCS</span>
                <span className="text-muted-foreground mt-1 text-[9px] tracking-[0.14em] uppercase">
                  layout control
                </span>
              </span>
            </Link>
            <span className="sidebar-label ml-auto rounded-full border border-white/10 px-1.5 py-0.5 text-[9px] font-medium tracking-wider text-white/40">
              LOCAL
            </span>
          </SidebarHeader>

          <SidebarContent>
            <SidebarGroup className="pt-3 pb-1">
              <SidebarMenu>
                <CreateProjectButton onClick={() => setNewProject(true)} />
              </SidebarMenu>
            </SidebarGroup>

            <SidebarGroup className="pt-1">
              <SidebarMenu>
                <NavItem to="/" icon={Boxes} label="Panel" end />
                <NavItem to="/tareas" icon={Waypoints} label="Tareas" />
                <NavItem to="/actividad" icon={Activity} label="Actividad" />
              </SidebarMenu>
            </SidebarGroup>

            <SidebarGroup className="pt-3">
              <div className="flex h-8 min-w-0 items-center justify-between px-2.5">
                <SidebarGroupLabel className="min-w-0 px-0 py-0">Proyectos</SidebarGroupLabel>
                <SidebarGroupAction title="Crear proyecto" aria-label="Crear proyecto" onClick={() => setNewProject(true)}>
                  <Plus className="size-4" />
                </SidebarGroupAction>
              </div>
              <SidebarMenu className="mt-1">
                {loading && <div className="sidebar-label px-2.5 py-1 text-xs text-white/45">Cargando…</div>}
                {!loading && projects.length === 0 && (
                  <button
                    onClick={() => setNewProject(true)}
                    className="sidebar-label w-full rounded-md border border-dashed border-white/15 px-2.5 py-3 text-left text-xs text-white/55 hover:bg-white/[0.04] hover:text-white"
                  >
                    Sin proyectos. Creá el primero →
                  </button>
                )}
                {projects.map((project) => (
                  <SidebarMenuButton
                    key={project.id}
                    asChild
                    isActive={activeProjectId === project.id}
                    tooltip={project.name}
                  >
                    <Link to={`/p/${project.id}`}>
                      <GitBranch className="size-4 shrink-0 opacity-75" />
                      <span className="sidebar-label min-w-0 flex-1 truncate">{project.name}</span>
                      {!!project.openBranches && (
                        <Badge variant="muted" className="sidebar-label ml-auto px-1 text-[10px]">
                          {project.openBranches}
                        </Badge>
                      )}
                    </Link>
                  </SidebarMenuButton>
                ))}
              </SidebarMenu>
            </SidebarGroup>
          </SidebarContent>

          <SidebarFooter>
            <FooterActions
              theme={theme}
              onToggleTheme={toggle}
              onSync={async () => {
                const id = toast.loading('Sincronizando con el NAS…')
                try {
                  await Promise.all(projects.map((project) => api.syncProject(project.id)))
                  await load()
                  toast.success('Sincronizado', { id })
                } catch (err) {
                  toast.error(err instanceof Error ? err.message : 'falló la sincronización', { id })
                }
              }}
            />
          </SidebarFooter>
        </Sidebar>

        <SidebarInset>
          <div className="app-titlebar flex h-10 shrink-0 items-center border-b px-4" onDoubleClick={() => window.go?.main?.App?.ToggleMaximise?.()}>
            <SidebarTrigger />
            <Separator orientation="vertical" className="mx-3 h-4" />
            <span className="text-muted-foreground text-[11px]">
              LCS <span className="mx-1.5 opacity-40">/</span>{' '}
              {location.pathname === '/' ? 'Panel' : location.pathname.split('/').filter(Boolean).join(' / ')}
            </span>
            <div className="window-controls ml-auto flex h-full items-center">
              <button aria-label="Minimizar" title="Minimizar" onClick={() => window.go?.main?.App?.Minimise?.()}>−</button>
              <button aria-label="Maximizar" title="Maximizar" onClick={() => window.go?.main?.App?.ToggleMaximise?.()}><span className="window-max-icon" /></button>
              <button className="window-close" aria-label="Cerrar" title="Cerrar" onClick={() => window.go?.main?.App?.Quit?.()}>×</button>
            </div>
          </div>
          <Outlet context={{ reloadProjects: load }} />
        </SidebarInset>
      </div>

      <NewProjectDialog
        open={newProject}
        onOpenChange={setNewProject}
        onCreated={(project) => {
          void load()
          navigate(`/p/${project.id}`)
        }}
      />
    </SidebarProvider>
  )
}

function CreateProjectButton({ onClick }: { onClick: () => void }) {
  const { open } = useSidebar()
  return (
    <button
      onClick={onClick}
      title={open ? undefined : 'Crear proyecto'}
      className="sidebar-create flex h-9 w-full min-w-0 items-center gap-2 overflow-hidden rounded-md px-2.5 text-left text-[13px] font-medium transition-colors"
    >
      <Plus className="size-4 shrink-0" />
      <span className="sidebar-label truncate">Crear proyecto</span>
    </button>
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
    <NavLink to={to} end={end} className="block min-w-0" title={label}>
      {({ isActive }) => (
        <SidebarMenuButton isActive={isActive} tooltip={label}>
          <Icon className="size-4 shrink-0" />
          <span className="sidebar-label truncate">{label}</span>
        </SidebarMenuButton>
      )}
    </NavLink>
  )
}

function FooterActions({
  theme,
  onToggleTheme,
  onSync
}: {
  theme: string
  onToggleTheme: () => void
  onSync: () => Promise<void>
}) {
  const { open } = useSidebar()
  const [connected, setConnected] = useState(false)
  const [syncing, setSyncing] = useState(false)

  const sync = async () => {
    if (syncing) return
    setSyncing(true)
    try {
      await onSync()
    } finally {
      setSyncing(false)
    }
  }

  useEffect(() => {
    api.bootstrap().then(() => setConnected(true)).catch(() => setConnected(false))
  }, [])

  return (
    <div className="sidebar-footer-actions flex min-w-0 items-center gap-1">
      <button
        type="button"
        title={open ? (connected ? 'Conectado' : 'Sin conexión') : 'Estado de conexión'}
        className="sidebar-footer-status flex h-9 min-w-0 flex-1 items-center gap-2 overflow-hidden rounded-md px-2 text-left text-xs text-white/60 hover:bg-white/[0.06] hover:text-white"
      >
        <span className={cn('size-2 shrink-0 rounded-full', connected ? 'bg-success' : 'bg-white/25')} />
        <span className="sidebar-label truncate">{connected ? 'Conectado' : 'Desconectado'}</span>
      </button>
      <FooterIcon title="Cambiar tema" onClick={onToggleTheme}>
        {theme === 'dark' ? <Sun className="size-4" /> : <Moon className="size-4" />}
      </FooterIcon>
      <FooterIcon title="Sincronizar proyectos" onClick={sync} disabled={syncing}>
        <RefreshCw className={cn('size-4', syncing && 'animate-spin')} />
      </FooterIcon>
    </div>
  )
}

function FooterIcon({ title, onClick, children, disabled }: { title: string; onClick: () => void; children: React.ReactNode; disabled?: boolean }) {
  return (
    <Button type="button" variant="ghost" size="icon-sm" title={title} aria-label={title} onClick={onClick} disabled={disabled} className="sidebar-footer-icon shrink-0 text-white/65 hover:bg-white/[0.08] hover:text-white">
      {children}
    </Button>
  )
}
