import { Badge } from '@/components/ui/badge'
import type { BranchStatus } from '@/lib/api'
import { cn } from '@/lib/utils'

const map: Record<BranchStatus, { label: string; variant: 'default' | 'warning' | 'success' | 'muted'; dot: string }> = {
  open: { label: 'En edición', variant: 'default', dot: 'bg-primary' },
  review: { label: 'Para revisar', variant: 'warning', dot: 'bg-warning' },
  merged: { label: 'Integrada', variant: 'success', dot: 'bg-success' },
  closed: { label: 'Descartada', variant: 'muted', dot: 'bg-muted-foreground' }
}

export function StatusBadge({ status, className }: { status: BranchStatus; className?: string }) {
  const s = map[status] ?? map.open
  return (
    <Badge variant={s.variant} className={cn('gap-1.5 pl-1.5', className)}>
      <span className={cn('size-1.5 rounded-full', s.dot)} />
      {s.label}
    </Badge>
  )
}
