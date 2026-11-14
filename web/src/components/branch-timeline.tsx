import { Check, GitBranch, GitMerge, Trash2, Upload, X, type LucideIcon } from 'lucide-react'
import type { TimelineItem, TimelineKind } from '@/lib/api'
import { formatBytes, formatDate, shortSha, timeAgo } from '@/lib/format'
import { cn } from '@/lib/utils'

const style: Record<TimelineKind, { icon: LucideIcon; dot: string; title: (t: TimelineItem) => string }> = {
  opened: {
    icon: GitBranch,
    dot: 'bg-muted text-muted-foreground',
    title: (t) => `${who(t)} abrió la tarea`
  },
  upload: {
    icon: Upload,
    dot: 'bg-primary/15 text-primary',
    title: (t) => `${who(t)} subió una versión`
  },
  approved: {
    icon: Check,
    dot: 'bg-success text-white',
    title: (t) => `${who(t)} aprobó${t.label ? ` · ${t.label}` : ''}`
  },
  rejected: {
    icon: X,
    dot: 'bg-destructive text-white',
    title: (t) => `${who(t)} rechazó${t.label ? ` · ${t.label}` : ''}`
  },
  merged: { icon: GitMerge, dot: 'bg-success text-white', title: () => 'Integrada en main' },
  closed: { icon: Trash2, dot: 'bg-muted text-muted-foreground', title: () => 'Tarea descartada' }
}

const who = (t: TimelineItem) => t.actor || t.email.split('@')[0] || 'alguien'

/** Historia de la rama: subidas, aprobaciones y rechazos en orden. */
export function BranchTimeline({ items }: { items: TimelineItem[] }) {
  if (items.length === 0) {
    return (
      <div className="text-muted-foreground rounded-lg border border-dashed py-8 text-center text-[13px]">
        Todavía no pasó nada en esta tarea.
      </div>
    )
  }
  return (
    <ol className="bg-card relative rounded-lg border p-4">
      {items.map((item, i) => {
        const s = style[item.kind]
        const Icon = s.icon
        const last = i === items.length - 1
        const decision = item.kind === 'approved' || item.kind === 'rejected'
        return (
          <li key={`${item.kind}-${item.at}-${i}`} className="relative flex gap-3 pb-5 last:pb-0">
            {!last && <span className="bg-border absolute top-7 bottom-0 left-[11.5px] w-px" />}
            <span className={cn('relative grid size-6 shrink-0 place-items-center rounded-full', s.dot, item.stale && decision && 'opacity-50')}>
              <Icon className="size-3.5" strokeWidth={decision ? 3 : 2} />
            </span>
            <div className="min-w-0 flex-1">
              <div className="flex flex-wrap items-baseline gap-x-2">
                <span className={cn('text-[13px] font-medium', item.stale && decision && 'text-muted-foreground line-through')}>
                  {s.title(item)}
                </span>
                {item.stale && decision && (
                  <span className="bg-muted text-muted-foreground rounded px-1.5 text-[10.5px]">
                    obsoleta: se subió una versión nueva
                  </span>
                )}
                <span className="text-muted-foreground text-[11.5px]" title={formatDate(item.at)}>
                  {timeAgo(item.at)}
                </span>
              </div>
              {item.kind === 'opened' && item.message && (
                <p className="text-muted-foreground mt-0.5 text-[12.5px]">{item.message}</p>
              )}
              {item.kind === 'upload' && (
                <p className="text-muted-foreground mt-0.5 text-[12.5px]">
                  {item.message || 'Sin mensaje'}
                  <span className="tnum ml-2 font-mono text-[11px]">
                    {shortSha(item.commit)}
                    {item.size ? ` · ${formatBytes(item.size)}` : ''}
                  </span>
                </p>
              )}
              {decision && item.message && (
                <p className="bg-muted/60 mt-1.5 rounded-md px-2.5 py-1.5 text-[12.5px] leading-relaxed">
                  {item.message}
                </p>
              )}
            </div>
          </li>
        )
      })}
    </ol>
  )
}
