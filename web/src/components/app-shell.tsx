import { Link, NavLink, Outlet, useLocation, useNavigate } from 'react-router-dom'
import { useCallback, useEffect, useRef, useState } from 'react'
import {
  Activity,
  ArrowLeft,
  ArrowRight,
  ChevronRight,
  ClipboardCopy,
  FolderPlus,
  FolderTree,
  Home,
  Info,
  LayoutDashboard,
  LogOut,
  Moon,
  Palette,
  PanelLeft,
  Plus,
  Redo2,
  RefreshCw,
  RotateCw,
  Scissors,
  ShieldCheck,
  Sun,
  TextSelect,
  Undo2,
  Waypoints
} from 'lucide-react'
import { AppTitleBar, type MenuDef } from '@/components/app-titlebar'
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
  SidebarGroupLabel,
  SidebarInset,
  SidebarMenu,
  SidebarMenuButton,
  SidebarProvider,
  SidebarTrigger,
  useSidebar
} from '@/components/ui/sidebar'
import { cn } from '@/lib/utils'
import { useTheme } from '@/components/theme-provider'
import { AppearanceDialog } from '@/components/appearance-dialog'
import { NewProjectDialog } from '@/components/new-project-dialog'
import { toast } from 'sonner'

export function AppShell() {
  const [projects, setProjects] = useState<Project[]>([])
  const [newProject, setNewProject] = useState(false)
  const [appearance, setAppearance] = useState(false)
  const [me, setMe] = useState<Identity | null>(null)
  const [pendingApprovals, setPendingApprovals] = useState(0)
  const [connected, setConnected] = useState(false)
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

  useEffect(() => {
    api.bootstrap().then(() => setConnected(true)).catch(() => setConnected(false))
  }, [])

  const syncAll = useCallback(async () => {
    const id = toast.loading('Sincronizando con el NAS…')
    try {
      await Promise.all(projects.map((project) => api.syncProject(project.id)))
      await load()
      toast.success('Sincronizado', { id })
    } catch (err) {
      toast.error(err instanceof Error ? err.message : 'falló la sincronización', { id })
    }
  }, [projects, load])

  const crumbs = breadcrumbs(location.pathname, projects)
  const lateZones = projects.filter((p) => p.parentId && (p.pendingChanges ?? 0) > 0).length
  const masters = projects.filter((p) => !p.parentId)
  // react-router guarda la posición en el historial: idx 0 = no hay a dónde volver.
  const canGoBack = ((window.history.state?.idx as number | undefined) ?? 0) > 0

  return (
    <SidebarProvider>
      <ShellBody
        crumbs={crumbs}
        me={me}
        connected={connected}
        pendingApprovals={pendingApprovals}
        lateZones={lateZones}
        theme={theme}
        canGoBack={canGoBack}
        onToggleTheme={toggle}
        onSync={syncAll}
        onNewProject={() => setNewProject(true)}
        onAppearance={() => setAppearance(true)}
        reloadProjects={load}
        navigate={(to) => (typeof to === 'number' ? navigate(to) : navigate(to))}
      />

      <AppearanceDialog open={appearance} onOpenChange={setAppearance} />
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

type ShellBodyProps = {
  crumbs: Crumb[]
  me: Identity | null
  connected: boolean
  pendingApprovals: number
  lateZones: number
  theme: string
  canGoBack: boolean
  onToggleTheme: () => void
  onSync: () => Promise<void>
  onNewProject: () => void
  onAppearance: () => void
  reloadProjects: () => Promise<void>
  navigate: (to: string | number) => void
}

function ShellBody({
  crumbs,
  me,
  connected,
  pendingApprovals,
  lateZones,
  theme,
  canGoBack,
  onToggleTheme,
  onSync,
  onNewProject,
  onAppearance,
  reloadProjects,
  navigate
}: ShellBodyProps) {
  const { toggle: toggleSidebar, open } = useSidebar()
  const quit = () => window.go?.main?.App?.Quit?.()
  const exec = (cmd: string) => document.execCommand(cmd)

  const menus: MenuDef[] = [
    {
      id: 'file',
      label: 'Archivo',
      items: [
        { label: 'Nuevo proyecto…', icon: FolderPlus, shortcut: 'Ctrl+N', onSelect: onNewProject },
        { label: 'Sincronizar con el NAS', icon: RefreshCw, shortcut: 'Ctrl+R', onSelect: () => void onSync() },
        { type: 'sep' },
        { label: 'Salir', icon: LogOut, shortcut: 'Ctrl+Q', danger: true, onSelect: quit }
      ]
    },
    {
      id: 'edit',
      label: 'Editar',
      items: [
        { label: 'Deshacer', icon: Undo2, shortcut: 'Ctrl+Z', onSelect: () => exec('undo') },
        { label: 'Rehacer', icon: Redo2, shortcut: 'Ctrl+Y', onSelect: () => exec('redo') },
        { type: 'sep' },
        { label: 'Cortar', icon: Scissors, shortcut: 'Ctrl+X', onSelect: () => exec('cut') },
        { label: 'Copiar', icon: ClipboardCopy, shortcut: 'Ctrl+C', onSelect: () => exec('copy') },
        { type: 'sep' },
        { label: 'Seleccionar todo', icon: TextSelect, shortcut: 'Ctrl+A', onSelect: () => exec('selectAll') }
      ]
    },
    {
      id: 'view',
      label: 'Ver',
      items: [
        { label: 'Panel', icon: LayoutDashboard, shortcut: 'Ctrl+1', onSelect: () => navigate('/') },
        { label: 'Proyectos', icon: FolderTree, shortcut: 'Ctrl+2', onSelect: () => navigate('/proyectos') },
        { label: 'Tareas', icon: Waypoints, shortcut: 'Ctrl+3', onSelect: () => navigate('/tareas') },
        { label: 'Aprobaciones', icon: ShieldCheck, shortcut: 'Ctrl+4', onSelect: () => navigate('/aprobaciones') },
        { label: 'Actividad', icon: Activity, shortcut: 'Ctrl+5', onSelect: () => navigate('/actividad') },
        { type: 'sep' },
        { label: 'Barra lateral', icon: PanelLeft, shortcut: 'Ctrl+B', checked: open, onSelect: toggleSidebar },
        { label: 'Apariencia…', icon: Palette, shortcut: 'Ctrl+,', onSelect: onAppearance },
        { label: theme === 'dark' ? 'Modo claro' : 'Modo oscuro', icon: theme === 'dark' ? Sun : Moon, onSelect: onToggleTheme },
        { type: 'sep' },
        { label: 'Recargar vista', icon: RotateCw, shortcut: 'Ctrl+Shift+R', onSelect: () => window.location.reload() }
      ]
    },
    {
      id: 'go',
      label: 'Ir',
      items: [
        { label: 'Atrás', icon: ArrowLeft, shortcut: 'Alt+←', disabled: !canGoBack, onSelect: () => navigate(-1) },
        { label: 'Adelante', icon: ArrowRight, shortcut: 'Alt+→', onSelect: () => navigate(1) },
        { type: 'sep' },
        { label: 'Inicio', icon: Home, onSelect: () => navigate('/') }
      ]
    },
    {
      id: 'help',
      label: 'Ayuda',
      items: [
        {
          label: 'Acerca de LCS',
          icon: Info,
          onSelect: () => toast('LCS — Layout Control System', { description: 'Gestor de layouts DWG con ramas, revisión y aprobaciones.' })
        }
      ]
    }
  ]

  // Atajos globales: la ventana no tiene menú nativo, así que se resuelven aquí.
  const shortcuts = useRef<(e: KeyboardEvent) => void>(() => {})
  shortcuts.current = (e) => {
    if (e.altKey && !e.ctrlKey && (e.key === 'ArrowLeft' || e.key === 'ArrowRight')) {
      e.preventDefault()
      navigate(e.key === 'ArrowLeft' ? -1 : 1)
      return
    }
    if (!e.ctrlKey || e.altKey) return
    const key = e.key.toLowerCase()
    if (key === 'r' && e.shiftKey) {
      e.preventDefault()
      window.location.reload()
      return
    }
    if (e.shiftKey) return
    const routes: Record<string, string> = { '1': '/', '2': '/proyectos', '3': '/tareas', '4': '/aprobaciones', '5': '/actividad' }
    const action = routes[key]
      ? () => navigate(routes[key])
      : key === 'n'
        ? onNewProject
        : key === 'r'
          ? () => void onSync()
          : key === 'b'
            ? toggleSidebar
            : key === ','
              ? onAppearance
              : key === 'q'
              ? quit
              : null
    if (!action) return
    e.preventDefault()
    action()
  }
  useEffect(() => {
    const handler = (e: KeyboardEvent) => shortcuts.current(e)
    window.addEventListener('keydown', handler)
    return () => window.removeEventListener('keydown', handler)
  }, [])

  const title = `${crumbs[crumbs.length - 1]?.label ?? 'Panel'} — Layout Control System`

  return (
    <div className="app-frame flex h-full min-w-0 flex-col">
      <AppTitleBar menus={menus} title={title} />

      <div className="flex min-h-0 min-w-0 flex-1">
        <Sidebar variant="sidebar" collapsible="icon" >
          <SidebarContent>
            <SidebarGroup className="pt-3 pb-1">
              <SidebarMenu>
                <CreateProjectButton onClick={onNewProject} />
              </SidebarMenu>
            </SidebarGroup>

            <SidebarGroup className="pt-0">
              <SidebarGroupLabel>Workspace</SidebarGroupLabel>
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
              </SidebarMenu>
            </SidebarGroup>

            <SidebarGroup className="pt-0">
              <SidebarGroupLabel>Trabajo</SidebarGroupLabel>
              <SidebarMenu>
                <NavItem to="/tareas" icon={Waypoints} label="Tareas" />
                <NavItem
                  to="/aprobaciones"
                  icon={ShieldCheck}
                  label="Aprobaciones"
                  badge={pendingApprovals}
                  badgeTitle={`${pendingApprovals} tarea(s) esperan tu decisión`}
                />
              </SidebarMenu>
            </SidebarGroup>

            <SidebarGroup className="pt-0">
              <SidebarGroupLabel>Auditoría</SidebarGroupLabel>
              <SidebarMenu>
                <NavItem to="/actividad" icon={Activity} label="Actividad" />
              </SidebarMenu>
            </SidebarGroup>
          </SidebarContent>

          <SidebarFooter>
            {me && <IdentityChip me={me} />}
            <FooterActions theme={theme} connected={connected} onToggleTheme={onToggleTheme} onSync={onSync} />
          </SidebarFooter>
        </Sidebar>

        <SidebarInset >
          <div className="app-titlebar flex h-11 shrink-0 items-center border-b px-3" onDoubleClick={() => window.go?.main?.App?.ToggleMaximise?.()}>
            <div className="titlebar-nodrag flex items-center gap-0.5" onDoubleClick={(e) => e.stopPropagation()}>
              <SidebarTrigger />
              <button className="card-nav-btn" title="Atrás (Alt+←)" aria-label="Atrás" disabled={!canGoBack} onClick={() => navigate(-1)}>
                <ArrowLeft className="size-4" />
              </button>
              <button className="card-nav-btn" title="Adelante (Alt+→)" aria-label="Adelante" onClick={() => navigate(1)}>
                <ArrowRight className="size-4" />
              </button>
            </div>
            <Separator orientation="vertical" className="mx-3 h-4" />
            <nav aria-label="Ubicación" className="flex min-w-0 items-center gap-1.5 text-[12.5px]">
              {crumbs.map((crumb, i) => {
                const last = i === crumbs.length - 1
                const cls = cn('truncate', last ? 'text-foreground font-medium' : 'text-muted-foreground')
                return (
                  <span key={i} className="flex min-w-0 items-center gap-1.5">
                    {i > 0 && <ChevronRight className="text-muted-foreground/50 size-3.5 shrink-0" />}
                    {crumb.to && !last ? (
                      <Link to={crumb.to} className={cn(cls, 'hover:text-foreground titlebar-nodrag')} onDoubleClick={(e) => e.stopPropagation()}>
                        {crumb.label}
                      </Link>
                    ) : (
                      <span className={cls}>{crumb.label}</span>
                    )}
                  </span>
                )
              })}
            </nav>
          </div>
          <Outlet context={{ reloadProjects }} />
        </SidebarInset>
      </div>

    </div>
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
      <span className="relative grid size-6 shrink-0 place-items-center rounded-full bg-sidebar-foreground/10 text-[10px] font-semibold text-sidebar-foreground/80">
        {initials(me.name) || '?'}
        {!me.verified && <span className="bg-warning absolute -right-0.5 -bottom-0.5 size-2 rounded-full" />}
      </span>
      {open && (
        <span className="sidebar-label min-w-0 flex-1 leading-tight">
          <span className="block truncate text-[12px] text-sidebar-foreground/85">{me.name}</span>
          <span className="block truncate text-[10.5px] text-sidebar-foreground/45">{me.email}</span>
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
      <kbd className="sidebar-label ml-auto font-sans text-[10.5px] font-normal opacity-70">Ctrl+N</kbd>
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
  connected,
  onToggleTheme,
  onSync
}: {
  theme: string
  connected: boolean
  onToggleTheme: () => void
  onSync: () => Promise<void>
}) {
  const { open } = useSidebar()
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

  return (
    <div className="sidebar-footer-actions flex min-w-0 items-center gap-1">
      <button
        type="button"
        title={open ? (connected ? 'Conectado' : 'Sin conexión') : 'Estado de conexión'}
        className="sidebar-footer-status flex h-9 min-w-0 flex-1 items-center gap-2 overflow-hidden rounded-md px-2 text-left text-xs text-sidebar-foreground/60 hover:bg-sidebar-foreground/[0.06] hover:text-sidebar-foreground"
      >
        <span className={cn('size-2 shrink-0 rounded-full', connected ? 'bg-success' : 'bg-sidebar-foreground/25')} />
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
    <Button type="button" variant="ghost" size="icon-sm" title={title} aria-label={title} onClick={onClick} disabled={disabled} className="sidebar-footer-icon shrink-0 text-sidebar-foreground/65 hover:bg-sidebar-foreground/[0.08] hover:text-sidebar-foreground">
      {children}
    </Button>
  )
}
