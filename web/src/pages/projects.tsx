import { useCallback, useEffect, useMemo, useState } from 'react'
import { Link, useNavigate, useOutletContext } from 'react-router-dom'
import {
  AlertTriangle,
  Boxes,
  CheckCircle2,
  CornerDownRight,
  FileBox,
  FolderTree,
  GitBranch,
  Plus,
  Search,
  Split
} from 'lucide-react'
import { toast } from 'sonner'
import { api, type Project } from '@/lib/api'
import { Button } from '@/components/ui/button'
import { Badge } from '@/components/ui/badge'
import { Input } from '@/components/ui/input'
import { Skeleton } from '@/components/ui/separator'
import { PageHeader, EmptyState } from '@/components/page-header'
import { NewProjectDialog } from '@/components/new-project-dialog'
import { IntegrateButton, useIntegrateZone } from '@/components/integrate-zone'
import { planos, timeAgo } from '@/lib/format'

type Shell = { reloadProjects?: () => void }

export function ProjectsPage() {
  const navigate = useNavigate()
  const shell = useOutletContext<Shell | null>()
  const [projects, setProjects] = useState<Project[]>([])
  const [loading, setLoading] = useState(true)
  const [query, setQuery] = useState('')
  const [dialog, setDialog] = useState<{ open: boolean; parentId: string; lock: boolean }>({
    open: false,
    parentId: '',
    lock: false
  })

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

  const refresh = useCallback(async () => {
    await load()
    shell?.reloadProjects?.()
  }, [load, shell])

  const { integrate, busyId, dialog: conflictDialog } = useIntegrateZone(refresh)

  const { groups, pending } = useMemo(() => {
    const ids = new Set(projects.map((p) => p.id))
    const zonesOf = new Map<string, Project[]>()
    for (const p of projects) {
      if (p.parentId && ids.has(p.parentId)) {
        zonesOf.set(p.parentId, [...(zonesOf.get(p.parentId) ?? []), p])
      }
    }
    // Un subproyecto cuyo master no existe más se muestra como proyecto raíz.
    const roots = projects.filter((p) => !p.parentId || !ids.has(p.parentId))
    const q = query.trim().toLowerCase()
    const match = (p: Project) => `${p.name} ${p.id} ${p.description}`.toLowerCase().includes(q)
    const groups = roots
      .map((root) => ({ root, zones: zonesOf.get(root.id) ?? [] }))
      .filter(({ root, zones }) => !q || match(root) || zones.some(match))
    const pending = projects
      .filter((p) => p.parentId && ids.has(p.parentId) && (p.pendingChanges ?? 0) > 0)
      .map((zone) => ({ zone, master: projects.find((m) => m.id === zone.parentId)! }))
    return { groups, pending }
  }, [projects, query])

  const masters = useMemo(() => projects.filter((p) => !p.parentId), [projects])
  const openDialog = (parentId = '', lock = false) => setDialog({ open: true, parentId, lock })

  return (
    <div className="scrollbar-thin flex-1 overflow-y-auto">
      <PageHeader
        eyebrow={
          <>
            <Boxes className="size-3.5" /> espacio de trabajo
          </>
        }
        title="Proyectos"
        description="Cada proyecto es un repositorio de planos. Un master puede tener subproyectos por zona: cuando una zona cambia, hay que actualizar el master."
        actions={
          <>
            <Button variant="outline" size="sm" onClick={() => openDialog(masters[0]?.id ?? '', false)} disabled={masters.length === 0}>
              <Split className="size-4" /> Nuevo subproyecto
            </Button>
            <Button size="sm" onClick={() => openDialog('', false)}>
              <Plus className="size-4" /> Nuevo proyecto
            </Button>
          </>
        }
      />

      {pending.length > 0 && (
        <div className="px-6 pb-4">
          <div className="border-warning/40 bg-warning/10 rounded-lg border">
            <div className="flex items-center gap-2 px-4 pt-3 pb-2 text-[13px] font-semibold">
              <AlertTriangle className="text-warning size-4" />
              {pending.length === 1
                ? '1 zona tiene cambios que el master todavía no tiene'
                : `${pending.length} zonas tienen cambios que el master todavía no tiene`}
            </div>
            <div className="divide-warning/20 divide-y">
              {pending.map(({ zone, master }) => (
                <div key={zone.id} className="flex items-center gap-3 px-4 py-2">
                  <div className="min-w-0 flex-1 text-[13px]">
                    <Link to={`/p/${zone.id}`} className="font-medium hover:underline">
                      {zone.name}
                    </Link>
                    <span className="text-muted-foreground">
                      {' '}
                      → {master.name} · {zone.pendingChanges} plano(s) sin integrar
                    </span>
                  </div>
                  <IntegrateButton
                    zoneId={zone.id}
                    zoneName={zone.name}
                    busy={busyId === zone.id}
                    onIntegrate={integrate}
                  />
                </div>
              ))}
            </div>
          </div>
        </div>
      )}

      <div className="px-6 pb-3">
        <div className="relative max-w-sm">
          <Search className="text-muted-foreground pointer-events-none absolute top-1/2 left-2.5 size-4 -translate-y-1/2" />
          <Input
            value={query}
            onChange={(e) => setQuery(e.target.value)}
            placeholder="Buscar proyecto o zona…"
            className="pl-8"
          />
        </div>
      </div>

      <div className="px-6 pb-10">
        {loading ? (
          <div className="grid gap-3 xl:grid-cols-2">
            {[0, 1, 2, 3].map((i) => (
              <Skeleton key={i} className="h-36 w-full" />
            ))}
          </div>
        ) : groups.length === 0 ? (
          <EmptyState
            icon={Boxes}
            title={query ? 'Nada coincide con la búsqueda' : 'Todavía no hay proyectos'}
            description={
              query
                ? 'Probá con otro nombre.'
                : 'Creá el primero para inicializar su repositorio de planos en el NAS.'
            }
            action={
              !query && (
                <Button size="sm" onClick={() => openDialog()}>
                  <Plus className="size-4" /> Crear proyecto
                </Button>
              )
            }
          />
        ) : (
          <div className="grid items-start gap-3 xl:grid-cols-2">
            {groups.map(({ root, zones }) => (
              <ProjectCard
                key={root.id}
                project={root}
                zones={zones}
                busyId={busyId}
                onIntegrate={integrate}
                onNewZone={() => openDialog(root.id, true)}
              />
            ))}
          </div>
        )}
      </div>

      <NewProjectDialog
        open={dialog.open}
        onOpenChange={(open) => setDialog((d) => ({ ...d, open }))}
        masters={masters}
        parentId={dialog.parentId}
        lockParent={dialog.lock}
        onCreated={(p) => {
          void refresh()
          navigate(`/p/${p.id}`)
        }}
      />
      {conflictDialog}
    </div>
  )
}

function ProjectCard({
  project,
  zones,
  busyId,
  onIntegrate,
  onNewZone
}: {
  project: Project
  zones: Project[]
  busyId: string | null
  onIntegrate: (id: string, name: string) => void
  onNewZone: () => void
}) {
  const isMaster = zones.length > 0
  const late = zones.filter((z) => (z.pendingChanges ?? 0) > 0).length
  return (
    <div className="bg-card overflow-hidden rounded-lg border shadow-xs">
      <Link
        to={`/p/${project.id}`}
        className="hover:bg-muted/40 flex items-start gap-3 px-4 py-3.5 transition-colors"
      >
        <div className="bg-muted text-muted-foreground grid size-9 shrink-0 place-items-center rounded-md">
          {isMaster ? <FolderTree className="size-4" /> : <FileBox className="size-4" />}
        </div>
        <div className="min-w-0 flex-1">
          <div className="flex items-center gap-2">
            <span className="truncate text-[14px] font-semibold tracking-tight">{project.name}</span>
            {isMaster && <Badge variant="default">Master</Badge>}
            {late > 0 && (
              <Badge variant="warning" className="tnum">
                {late} {late === 1 ? 'zona atrasada' : 'zonas atrasadas'}
              </Badge>
            )}
          </div>
          <p className="text-muted-foreground mt-0.5 line-clamp-1 text-[12.5px]">
            {project.description || <span className="font-mono text-[11px]">{project.id}</span>}
          </p>
          <div className="text-muted-foreground tnum mt-2 flex items-center gap-3 text-[12px]">
            <span className="inline-flex items-center gap-1">
              <FileBox className="size-3.5" /> {planos(project.drawings ?? 0)}
            </span>
            <span className="inline-flex items-center gap-1">
              <GitBranch className="size-3.5" /> {project.openBranches ?? 0} tareas
            </span>
            <span className="ml-auto">{timeAgo(project.updatedAt)}</span>
          </div>
        </div>
      </Link>

      <div className="bg-muted/30 border-t">
        {zones.map((zone) => (
          <ZoneRow key={zone.id} zone={zone} busy={busyId === zone.id} onIntegrate={onIntegrate} />
        ))}
        <button
          type="button"
          onClick={onNewZone}
          className="text-muted-foreground hover:text-foreground hover:bg-muted/60 flex h-9 w-full items-center gap-2 px-4 text-left text-[12.5px] transition-colors"
        >
          <Plus className="size-3.5" />
          {isMaster ? 'Nuevo subproyecto' : 'Añadir subproyecto (zona)'}
        </button>
      </div>
    </div>
  )
}

function ZoneRow({
  zone,
  busy,
  onIntegrate
}: {
  zone: Project
  busy: boolean
  onIntegrate: (id: string, name: string) => void
}) {
  const pending = zone.pendingChanges ?? 0
  const empty = (zone.drawings ?? 0) === 0
  return (
    <div className="flex min-h-11 items-center gap-2 border-b px-4 py-1.5 last:border-b-0">
      <CornerDownRight className="text-muted-foreground/60 size-3.5 shrink-0" />
      <Link to={`/p/${zone.id}`} className="min-w-0 flex-1 hover:underline">
        <span className="truncate text-[13px] font-medium">{zone.name}</span>
        <span className="text-muted-foreground tnum ml-2 text-[11.5px]">
          {planos(zone.drawings ?? 0)}
        </span>
      </Link>
      {pending > 0 ? (
        <>
          <Badge variant="warning" className="tnum">
            {pending} sin integrar
          </Badge>
          <IntegrateButton
            zoneId={zone.id}
            zoneName={zone.name}
            busy={busy}
            onIntegrate={onIntegrate}
            size="xs"
            variant="outline"
          >
            Actualizar master
          </IntegrateButton>
        </>
      ) : empty ? (
        <Badge variant="muted">Sin planos</Badge>
      ) : (
        <Badge variant="success" className="gap-1">
          <CheckCircle2 /> Al día
        </Badge>
      )}
    </div>
  )
}
