import { Check, GitMerge, PencilLine, ShieldCheck, Upload, X } from 'lucide-react'
import type { TaskStage, TimelineItem } from '@/lib/api'
import { formatDate, timeAgo } from '@/lib/format'
import { cn } from '@/lib/utils'

const STAGES: { key: TaskStage; label: string }[] = [
  { key: 'open', label: 'Edición' },
  { key: 'review', label: 'Subida' },
  { key: 'approving', label: 'Aprobación' },
  { key: 'ready', label: 'Lista' },
  { key: 'merged', label: 'Integrada' }
]

const stageIndex = (s: TaskStage) => (s === 'closed' ? -1 : STAGES.findIndex((x) => x.key === s))

/** Los cinco pasos de una tarea; el actual resaltado. */
export function StageTrack({ stage }: { stage: TaskStage }) {
  const at = stageIndex(stage)
  if (stage === 'closed')
    return <span className="text-muted-foreground text-[12px] font-medium">Descartada</span>
  return (
    <div className="flex items-center">
      {STAGES.map((s, i) => {
        const done = i < at || stage === 'merged'
        const current = i === at && stage !== 'merged'
        return (
          <div key={s.key} className="flex items-center">
            <div className="flex flex-col items-center gap-1">
              <span
                className={cn(
                  'grid size-4 place-items-center rounded-full border-2 transition-colors',
                  done && 'border-success bg-success text-white',
                  current && 'border-warning bg-warning/20 ring-warning/30 ring-4',
                  !done && !current && 'border-border bg-card'
                )}
              >
                {done && <Check className="size-2.5" strokeWidth={4} />}
              </span>
              <span
                className={cn(
                  'text-[10px] leading-none font-medium whitespace-nowrap',
                  current ? 'text-foreground' : 'text-muted-foreground'
                )}
              >
                {s.label}
              </span>
            </div>
            {i < STAGES.length - 1 && (
              <span className={cn('mx-1 mb-3.5 h-0.5 w-6 rounded-full sm:w-8', i < at || stage === 'merged' ? 'bg-success' : 'bg-border')} />
            )}
          </div>
        )
      })}
    </div>
  )
}

const KIND: Record<string, { icon: typeof Check; tone: string; verb: string }> = {
  opened: { icon: PencilLine, tone: 'bg-primary text-primary-foreground', verb: 'Abrió la tarea' },
  upload: { icon: Upload, tone: 'bg-sky-500 text-white', verb: 'Subió una versión' },
  approved: { icon: ShieldCheck, tone: 'bg-success text-white', verb: 'Aprobó' },
  rejected: { icon: X, tone: 'bg-destructive text-white', verb: 'Rechazó' },
  merged: { icon: GitMerge, tone: 'bg-success text-white', verb: 'Integrada en main' },
  closed: { icon: X, tone: 'bg-muted-foreground text-white', verb: 'Descartada' }
}

/** Línea de tiempo horizontal: un punto por evento, con quién y cuándo. */
export function EventTrack({ items }: { items: TimelineItem[] }) {
  if (!items.length) return null
  // Con muchas subidas se muestran las últimas: la tabla es un resumen.
  const shown = items.length > 9 ? [items[0], ...items.slice(-8)] : items
  return (
    <div className="flex items-center overflow-x-auto py-1">
      {shown.map((it, i) => {
        const k = KIND[it.kind] ?? KIND.opened
        const Icon = k.icon
        const who = it.actor || it.email.split('@')[0]
        return (
          <div key={`${it.kind}-${it.at}-${i}`} className="flex items-center">
            <div
              className="group relative flex flex-col items-center"
              title={`${k.verb}${who && it.kind !== 'merged' ? ` · ${who}` : ''} · ${formatDate(it.at)}${it.message ? `\n${it.message}` : ''}${it.stale ? '\n(versión anterior)' : ''}`}
            >
              <span
                className={cn(
                  'grid size-6 place-items-center rounded-full shadow-xs',
                  k.tone,
                  it.stale && 'opacity-45'
                )}
              >
                <Icon className="size-3" strokeWidth={2.5} />
              </span>
              <span className="text-muted-foreground mt-1 text-[9.5px] leading-none whitespace-nowrap">
                {timeAgo(it.at)}
              </span>
            </div>
            {i < shown.length - 1 && <span className="bg-border mx-1 mb-3 h-px w-5 shrink-0" />}
          </div>
        )
      })}
    </div>
  )
}
