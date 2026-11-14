import type { ReactNode } from 'react'
import { cn } from '@/lib/utils'

export function PageHeader({
  eyebrow,
  title,
  description,
  actions,
  className
}: {
  eyebrow?: ReactNode
  title: ReactNode
  description?: ReactNode
  actions?: ReactNode
  className?: string
}) {
  return (
    <div className={cn('flex flex-wrap items-start justify-between gap-4 px-6 pt-6 pb-5', className)}>
      <div className="min-w-0">
        {eyebrow && (
          <div className="text-muted-foreground mb-2 flex items-center gap-1.5 text-[11px] font-medium tracking-[0.06em] uppercase">
            {eyebrow}
          </div>
        )}
        <h1 className="truncate text-[21px] leading-tight font-semibold tracking-tight">{title}</h1>
        {description && (
          <p className="text-muted-foreground mt-1.5 max-w-2xl text-[13.5px] leading-relaxed">
            {description}
          </p>
        )}
      </div>
      {actions && <div className="flex shrink-0 items-center gap-2">{actions}</div>}
    </div>
  )
}

export function SectionTitle({
  children,
  action
}: {
  children: ReactNode
  action?: ReactNode
}) {
  return (
    <div className="flex h-8 items-center justify-between px-0.5 pb-2">
      <h2 className="text-[13.5px] font-semibold tracking-tight">
        {children}
      </h2>
      {action}
    </div>
  )
}

export function EmptyState({
  icon: Icon,
  title,
  description,
  action
}: {
  icon?: React.ComponentType<{ className?: string }>
  title: string
  description?: string
  action?: ReactNode
}) {
  return (
    <div className="bg-card/60 flex flex-col items-center justify-center gap-2 rounded-lg border border-dashed px-6 py-14 text-center">
      {Icon && (
        <span className="bg-muted text-muted-foreground mb-2 grid size-10 place-items-center rounded-full">
          <Icon className="size-5" />
        </span>
      )}
      <p className="text-sm font-medium">{title}</p>
      {description && (
        <p className="text-muted-foreground max-w-sm text-[13px] leading-relaxed">{description}</p>
      )}
      {action && <div className="mt-2">{action}</div>}
    </div>
  )
}
