import { useCallback, useEffect, useState } from 'react'
import { Link, useNavigate } from 'react-router-dom'
import { AlertTriangle, ArrowRight, CheckCircle2, Clock, Columns2, FileBox, ShieldCheck } from 'lucide-react'
import { toast } from 'sonner'
import { api, type ApprovalsPanel, type PanelItem } from '@/lib/api'
import { Button } from '@/components/ui/button'
import { Badge } from '@/components/ui/badge'
import { Skeleton } from '@/components/ui/separator'
import { PageHeader, EmptyState, SectionTitle } from '@/components/page-header'
import { ChainDots, nameOf } from '@/components/approval-chain'
import { initials, timeAgo } from '@/lib/format'
import { ChangesChip } from '@/components/changes-chip'

export function ApprovalsPage() {
  const [panel, setPanel] = useState<ApprovalsPanel | null>(null)
  const [loading, setLoading] = useState(true)

  const load = useCallback(async () => {
    try {
      setPanel(await api.approvals())
    } catch (err) {
      toast.error(err instanceof Error ? err.message : 'no pude cargar las aprobaciones')
    } finally {
      setLoading(false)
    }
  }, [])

  // Otras personas aprueban y suben planos desde sus equipos: refrescar solo.
  useEffect(() => {
    void load()
    const timer = window.setInterval(load, 10000)
    window.addEventListener('focus', load)
    return () => {
      window.clearInterval(timer)
      window.removeEventListener('focus', load)
    }
  }, [load])

  const me = panel?.me

  return (
    <div className="scrollbar-thin flex-1 overflow-y-auto">
      <PageHeader
        eyebrow={
          <>
            <ShieldCheck className="size-3.5" /> revisión
          </>
        }
        title="Aprobaciones"
        description="Tareas que esperan tu decisión. Cada proyecto define el orden de aprobación: no puedes decidir hasta que apruebe quien va antes que tú."
        actions={
          me && (
            <div className="text-muted-foreground bg-card flex items-center gap-2 rounded-md border px-3 py-1.5 text-[12px]" title={`Identidad: ${me.source}`}>
              <span className="bg-primary/10 text-primary grid size-6 place-items-center rounded-full text-[10px] font-semibold">
                {initials(me.name)}
              </span>
              {me.email}
            </div>
          )
        }
      />

      {me && !me.verified && (
        <div className="px-6 pb-4">
          <div className="border-warning/40 bg-warning/10 flex items-start gap-2.5 rounded-lg border px-4 py-3 text-[12.5px] leading-relaxed">
            <AlertTriangle className="text-warning mt-0.5 size-4 shrink-0" />
            <div>
              <strong>No pude obtener tu correo de la sesión de Windows</strong>, así que usé{' '}
              <span className="font-mono">{me.email}</span>. Ese correo no coincide con ningún
              aprobador. Defínelo en la variable de entorno <span className="font-mono">LCS_EMAIL</span>{' '}
              (o en <span className="font-mono">email</span> de <span className="font-mono">config.json</span>).
            </div>
          </div>
        </div>
      )}

      <div className="space-y-8 px-6 pb-10">
        {loading ? (
          <div className="space-y-2">
            {[0, 1, 2].map((i) => (
              <Skeleton key={i} className="h-20 w-full" />
            ))}
          </div>
        ) : (
          <>
            <Section
              title="Esperan tu decisión"
              count={panel?.pending.length ?? 0}
              items={panel?.pending ?? []}
              tone="pending"
              empty={
                <EmptyState
                  icon={CheckCircle2}
                  title="No tienes nada pendiente"
                  description="Cuando alguien suba cambios a una tarea de un proyecto donde eres aprobador, y te toque a ti, aparecerá aquí."
                />
              }
            />
            {(panel?.queued.length ?? 0) > 0 && (
              <Section
                title="En cola: aún no es tu turno"
                count={panel!.queued.length}
                items={panel!.queued}
                tone="queued"
              />
            )}
            {(panel?.decided.length ?? 0) > 0 && (
              <Section
                title="Ya aprobaste: falta el resto o integrar"
                count={panel!.decided.length}
                items={panel!.decided}
                tone="decided"
              />
            )}
          </>
        )}
      </div>
    </div>
  )
}

function Section({
  title,
  count,
  items,
  tone,
  empty
}: {
  title: string
  count: number
  items: PanelItem[]
  tone: 'pending' | 'queued' | 'decided'
  empty?: React.ReactNode
}) {
  return (
    <section>
      <SectionTitle action={<Badge variant={tone === 'pending' && count > 0 ? 'warning' : 'muted'} className="tnum">{count}</Badge>}>
        {title}
      </SectionTitle>
      {items.length === 0 ? (
        empty
      ) : (
        <div className="divide-y overflow-hidden rounded-lg border bg-card shadow-xs">
          {items.map((item) => (
            <Row key={item.branch.id} item={item} tone={tone} />
          ))}
        </div>
      )}
    </section>
  )
}

function Row({ item, tone }: { item: PanelItem; tone: 'pending' | 'queued' | 'decided' }) {
  const b = item.branch
  const waitingFor = item.nodes[item.next]
  const navigate = useNavigate()
  const base = `/p/${b.projectId}/t/${b.id}`
  return (
    <div
      role="link"
      tabIndex={0}
      onClick={() => navigate(base)}
      onKeyDown={(e) => e.key === 'Enter' && navigate(base)}
      className="hover:bg-muted/50 flex cursor-pointer flex-wrap items-center gap-x-4 gap-y-2 px-4 py-3.5 transition-colors"
    >
      <div className="bg-primary/10 text-primary grid size-9 shrink-0 place-items-center rounded-full text-[11px] font-semibold">
        {initials(b.author)}
      </div>
      <div className="min-w-0 flex-1 basis-60">
        <div className="flex items-center gap-2">
          <span className="truncate text-[13.5px] font-medium">{b.title}</span>
        </div>
        <div className="text-muted-foreground mt-0.5 flex flex-wrap items-center gap-x-2 text-[12px]">
          <span className="font-medium">{b.projectName}</span>
          <span>·</span>
          <span>{b.author}</span>
          <span>·</span>
          <span className="inline-flex items-center gap-1">
            <FileBox className="size-3" /> {item.files} {item.files === 1 ? 'plano' : 'planos'}
          </span>
          <span>·</span>
          <span>{timeAgo(b.updatedAt)}</span>
        </div>
        {item.changed?.length > 0 && (
          <div className="mt-1.5 flex flex-wrap items-center gap-1.5" onClick={(e) => e.stopPropagation()}>
            <ChangesChip changes={item.changes} />
            {item.changed.map((f) => (
              <Link
                key={f.path}
                to={`${base}?tab=compare&file=${encodeURIComponent(f.path)}`}
                className="bg-muted hover:bg-primary/10 hover:text-primary inline-flex items-center gap-1 rounded-md px-2 py-0.5 text-[11.5px] font-medium transition-colors"
                title="Ver qué cambió: compara la versión nueva con main"
              >
                <Columns2 className="size-3" /> {f.name}
              </Link>
            ))}
          </div>
        )}
      </div>
      <ChainDots nodes={item.nodes} />
      <div className="flex w-44 shrink-0 items-center justify-end gap-2 text-[12px]">
        {tone === 'pending' && (
          <Button size="xs">
            Revisar <ArrowRight className="size-3" />
          </Button>
        )}
        {tone === 'queued' && waitingFor && (
          <span className="text-muted-foreground inline-flex items-center gap-1">
            <Clock className="size-3.5" /> espera a {nameOf(waitingFor.email)}
          </span>
        )}
        {tone === 'decided' && (
          <span className="text-success inline-flex items-center gap-1">
            <CheckCircle2 className="size-3.5" />
            {waitingFor ? `falta ${nameOf(waitingFor.email)}` : 'lista para integrar'}
          </span>
        )}
      </div>
    </div>
  )
}
