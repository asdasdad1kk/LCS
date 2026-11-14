import { Link, NavLink, Outlet, useLocation, useNavigate } from 'react-router-dom'
import { useCallback, useEffect, useState } from 'react'
import {
  Activity,
  Boxes,
  ChevronRight,
  FolderTree,
  LayoutDashboard,
  Minus,
  Moon,
  Plus,
  RefreshCw,
  ShieldCheck,
  Square,
  Sun,
  Waypoints,
  X
} from 'lucide-react'
import { Logo } from '@/components/logo'
import { api, type Identity, type Project } from '@/lib/api'
import { initials } from '@/lib/format'
import { Button } from '@/components/ui/button'
import { Badge } from '@/components/ui/badge'
import { Separator } from '@/components/ui/separator'
import {
  Sidebar,
  SidebarContent,
  SidebarFooter,
  SidebarGroup,
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
  const [newProject, setNewProject] = useState(false)
  const [me, setMe] = useState<Identity | null>(null)
  const [pendingApprovals, setPendingApprovals] = useState(0)
  const { theme, toggle } = useTheme()
  const location = useLocation()
  const navigate = useNavigate()

  // Quién soy y cuántas aprobaciones me esperan. Otras personas aprueban y
  // suben planos desde sus equipos: se refresca solo.
  const loadApprovals = useCallback(async () => {
    try {
      const panel = await api.approvals()
      setMe(panel.me)
      setPendingApprovals(panel.pending.length)
    } catch {
      /* el contador es secundario */
    }
  }, [])

  useEffect(() => {
    void loadApprovals()
    const timer = window.setInterval(loadApprovals, 30000)
    window.addEventListener('focus', loadApprovals)
    return () => {
      window.clearInterval(timer)
      window.removeEventListener('focus', loadApprovals)
    }
  }, [loadApprovals])

  const load = useCallback(async () => {
    try {
      setProjects(await api.projects())
    } catch (err) {
      toast.error(err instanceof Error ? err.message : 'no pude cargar los proyectos')
    }
  }, [])

  useEffect(() => {
    void load()
  }, [load])

  const crumbs = breadcrumbs(location.pathname, projects)
  const lateZones = projects.filter((p) => p.parentId && (p.pendingChanges ?? 0) > 0).length
  const masters = projects.filter((p) => !p.parentId)

  return (
    <SidebarProvider>
      <div className="app-frame flex h-full min-w-0">
        <Sidebar variant="sidebar" collapsible="icon">
          <SidebarHeader>
            <Link to="/" className="flex min-w-0 items-center gap-2.5">
              <Logo className="size-8 shrink-0" />
              <span className="sidebar-label flex min-w-0 flex-col leading-none">
                <span className="text-[14px] font-semibold tracking-tight">LCS</span>
                <span className="mt-1 truncate text-[11px] text-white/45">Layout Control System</span>
              </span>
            </Link>
          </SidebarHeader>

          <SidebarContent>
            <SidebarGroup className="pt-3 pb-1">
              <SidebarMenu>
                <CreateProjectButton onClick={() => setNewProject(true)} />
              </SidebarMenu>
            </SidebarGroup>

            <SidebarGroup className="pt-1">
              <SidebarMenu>
                <NavItem to="/" icon={LayoutDashboard} label="Panel" end />
                <NavItem
                  to="/proyectos"
                  icon={FolderTree}
                  label="Proyectos"
                  badge={lateZones}
                  badgeTitle={`${lateZones} zona(s) con cambios sin integrar al master`}
                  match={(path) => path === '/proyectos' || path.startsWith('/p/')}
                />
                <NavItem to="/tareas" icon={Waypoints} label="Tareas" />
                <NavItem
                  to="/aprobaciones"
                  icon={ShieldCheck}
                  label="Aprobaciones"
                  badge={pendingApprovals}
                  badgeTitle={`${pendingApprovals} tarea(s) esperan tu decisión`}
                />
                <NavItem to="/actividad" icon={Activity} label="Actividad" />
              </SidebarMenu>
            </SidebarGroup>
          </SidebarContent>

          <SidebarFooter>
            {me && <IdentityChip me={me} />}
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
          <div className="app-titlebar flex h-11 shrink-0 items-center border-b px-4" onDoubleClick={() => window.go?.main?.App?.ToggleMaximise?.()}>
            <SidebarTrigger />
            <Separator orientation="vertical" className="mx-3 h-4" />
            <nav aria-label="Ubicación" className="flex min-w-0 items-center gap-1.5 text-[12.5px]">
              {crumbs.map((crumb, i) => {
                const last = i === crumbs.length - 1
                const cls = cn('truncate', last ? 'text-foreground font-medium' : 'text-muted-foreground')
                return (
                  <span key={i} className="flex min-w-0 items-center gap-1.5">
                    {i > 0 && <ChevronRight className="text-muted-foreground/50 size-3.5 shrink-0" />}
                    {crumb.to && !last ? (
                      <Link to={crumb.to} className={cn(cls, 'hover:text-foreground')} onDoubleClick={(e) => e.stopPropagation()}>
                        {crumb.label}
                      </Link>
                    ) : (
                      <span className={cls}>{crumb.label}</span>
                    )}
                  </span>
                )
              })}
            </nav>
            <div className="window-controls -mr-4 ml-auto flex h-full shrink-0 items-center pl-4">
              <button aria-label="Minimizar" title="Minimizar" onClick={() => window.go?.main?.App?.Minimise?.()}>
                <Minus className="size-4" strokeWidth={1.5} />
              </button>
              <button aria-label="Maximizar" title="Maximizar" onClick={() => window.go?.main?.App?.ToggleMaximise?.()}>
                <Square className="size-3" strokeWidth={1.75} />
              </button>
              <button className="window-close" aria-label="Cerrar" title="Cerrar" onClick={() => window.go?.main?.App?.Quit?.()}>
                <X className="size-4" strokeWidth={1.5} />
              </button>
            </div>
          </div>
          <Outlet context={{ reloadProjects: load }} />
        </SidebarInset>
      </div>

      <NewProjectDialog
        open={newProject}
        onOpenChange={setNewProject}
        masters={masters}
        onCreated={(project) => {
          void load()
          navigate(`/p/${project.id}`)
        }}
      />
    </SidebarProvider>
  )
}

// Texto de la barra de título: nombres legibles en vez de la ruta cruda.
type Crumb = { label: string; to?: string }

function breadcrumbs(pathname: string, projects: Project[]): Crumb[] {
  const [section, id, kind] = pathname.split('/').filter(Boolean)
  if (section === 'proyectos') return [{ label: 'Proyectos' }]
  if (section === 'tareas') return [{ label: 'Tareas' }]
  if (section === 'aprobaciones') return [{ label: 'Aprobaciones' }]
  if (section === 'actividad') return [{ label: 'Actividad' }]
  if (section === 'p' && id) {
    const project = projects.find((p) => p.id === id)
    const parent = project?.parentId ? projects.find((p) => p.id === project.parentId) : undefined
    const out: Crumb[] = [{ label: 'Proyectos', to: '/proyectos' }]
    if (parent) out.push({ label: parent.name, to: `/p/${parent.id}` })
    out.push({ label: project?.name ?? id, to: `/p/${id}` })
    if (kind === 'd') out.push({ label: 'Plano' })
    if (kind === 't') out.push({ label: 'Tarea' })
    return out
  }
  return [{ label: 'Panel' }]
}

// Quién eres para LCS: sale de tu sesión de Windows (whoami /upn), sin iniciar sesión.
function IdentityChip({ me }: { me: Identity }) {
  const { open } = useSidebar()
  const hint = me.verified
    ? `${me.email} (identificado por ${me.source === 'config' ? 'LCS_EMAIL' : 'tu sesión de Windows'})`
    : `${me.email}: no pude obtener tu correo de Windows. Define LCS_EMAIL.`
  return (
    <div
      title={hint}
      className="sidebar-footer-status mb-1 flex h-10 min-w-0 items-center gap-2.5 overflow-hidden rounded-md px-2 text-left"
    >
      <span className="relative grid size-6 shrink-0 place-items-center rounded-full bg-white/10 text-[10px] font-semibold text-white/80">
        {initials(me.name) || '?'}
        {!me.verified && <span className="bg-warning absolute -right-0.5 -bottom-0.5 size-2 rounded-full" />}
      </span>
      {open && (
        <span className="sidebar-label min-w-0 flex-1 leading-tight">
          <span className="block truncate text-[12px] text-white/85">{me.name}</span>
          <span className="block truncate text-[10.5px] text-white/45">{me.email}</span>
        </span>
      )}
    </div>
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
  end,
  badge,
  badgeTitle,
  match
}: {
  to: string
  icon: React.ComponentType<{ className?: string }>
  label: string
  end?: boolean
  /** Contador resaltado (p. ej. zonas atrasadas); no se muestra si es 0. */
  badge?: number
  badgeTitle?: string
  /** Rutas extra que también dejan el ítem activo (p. ej. /p/* cuelga de Proyectos). */
  match?: (pathname: string) => boolean
}) {
  const { pathname } = useLocation()
  return (
    <NavLink to={to} end={end} className="block min-w-0" title={label}>
      {({ isActive }) => (
        <SidebarMenuButton isActive={isActive || !!match?.(pathname)} tooltip={label}>
          <Icon className="size-4 shrink-0" />
          <span className="sidebar-label min-w-0 flex-1 truncate">{label}</span>
          {!!badge && (
            <Badge
              variant="warning"
              title={badgeTitle}
              className="sidebar-label tnum ml-auto min-w-5 rounded-full px-1.5 text-[10.5px]"
            >
              {badge}
            </Badge>
          )}
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
