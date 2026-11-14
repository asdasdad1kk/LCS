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
    <div className={cn('flex flex-wrap items-start justify-between gap-4 px-6 pt-5 pb-4', className)}>
      <div className="min-w-0">
        {eyebrow && (
          <div className="text-muted-foreground mb-1.5 flex items-center gap-2 text-[11px] font-semibold tracking-[0.14em] uppercase">
            {eyebrow}
          </div>
        )}
        <h1 className="truncate text-[19px] leading-tight font-semibold tracking-tight">{title}</h1>
        {description && (
          <p className="text-muted-foreground mt-1 max-w-2xl text-[13px] leading-relaxed">
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
    <div className="flex items-center justify-between px-1 pb-2">
      <h2 className="text-[11px] font-semibold tracking-[0.14em] uppercase text-muted-foreground">
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
    <div className="flex flex-col items-center justify-center gap-2 rounded-lg border border-dashed px-6 py-12 text-center">
      {Icon && <Icon className="text-muted-foreground/60 mb-1 size-6" />}
      <p className="text-sm font-medium">{title}</p>
      {description && (
        <p className="text-muted-foreground max-w-sm text-[13px] leading-relaxed">{description}</p>
      )}
      {action && <div className="mt-2">{action}</div>}
    </div>
  )
}
