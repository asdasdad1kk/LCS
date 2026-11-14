import { useState } from 'react'
import { Check, Loader2, ShieldCheck, X } from 'lucide-react'
import { Button } from '@/components/ui/button'
import { Textarea } from '@/components/ui/input'
import { Badge } from '@/components/ui/badge'
import type { ReviewNode, ReviewView } from '@/lib/api'
import { timeAgo } from '@/lib/format'
import { cn } from '@/lib/utils'

export const nameOf = (email: string) => email.split('@')[0]

/** Círculo de un nodo de la cadena. */
function NodeDot({ node, index, size = 'md' }: { node: ReviewNode; index: number; size?: 'sm' | 'md' }) {
  const dim = size === 'sm' ? 'size-5 text-[10px]' : 'size-7 text-[12px]'
  const icon = size === 'sm' ? 'size-3' : 'size-4'
  const base = cn('relative grid shrink-0 place-items-center rounded-full font-semibold tnum', dim)
  if (node.status === 'approved')
    return (
      <span className={cn(base, 'bg-success text-white')}>
        <Check className={icon} strokeWidth={3} />
      </span>
    )
  if (node.status === 'rejected')
    return (
      <span className={cn(base, 'bg-destructive text-white')}>
        <X className={icon} strokeWidth={3} />
      </span>
    )
  if (node.active)
    return (
      <span className={cn(base, 'bg-warning/20 text-warning ring-warning/60 ring-2')}>
        {index + 1}
        <span className="bg-warning absolute -top-0.5 -right-0.5 size-2 animate-pulse rounded-full" />
      </span>
    )
  return <span className={cn(base, 'bg-muted text-muted-foreground')}>{index + 1}</span>
}

function statusText(node: ReviewNode, prev?: ReviewNode): { text: string; tone: string } {
  const d = node.decision
  if (node.status === 'approved' && d)
    return { text: `Aprobó ${timeAgo(d.at)}`, tone: 'text-success' }
  if (node.status === 'rejected' && d)
    return { text: `Rechazó ${timeAgo(d.at)}`, tone: 'text-destructive' }
  if (node.active) {
    return {
      text: node.stale ? 'Le toca decidir de nuevo: cambió un plano' : 'Le toca decidir',
      tone: 'text-warning'
    }
  }
  return {
    text: prev ? `Espera a ${nameOf(prev.email)}` : 'En espera',
    tone: 'text-muted-foreground'
  }
}

/** La cadena de aprobación completa, en orden, con el estado de cada nodo. */
export function ApprovalChain({ nodes }: { nodes: ReviewNode[] }) {
  return (
    <ol className="relative">
      {nodes.map((node, i) => {
        const prev = i > 0 ? nodes[i - 1] : undefined
        const st = statusText(node, prev)
        const last = i === nodes.length - 1
        return (
          <li key={node.email} className="relative flex gap-3 pb-4 last:pb-0">
            {!last && (
              <span
                className={cn(
                  'absolute top-7 bottom-0 left-[13.5px] w-px',
                  node.status === 'approved' ? 'bg-success/50' : 'bg-border'
                )}
              />
            )}
            <NodeDot node={node} index={i} />
            <div className="min-w-0 flex-1 pt-0.5">
              <div className="flex flex-wrap items-center gap-x-2 gap-y-1">
                <span className="text-[13px] font-medium" title={node.email}>
                  {nameOf(node.email)}
                </span>
                {node.label && <Badge variant="outline">{node.label}</Badge>}
                {node.isMe && <Badge variant="default">Tú</Badge>}
                {node.isAuthor && <Badge variant="muted">Autor</Badge>}
              </div>
              <div className="text-muted-foreground truncate text-[11.5px]">{node.email}</div>
              <div className={cn('mt-0.5 text-[12px]', st.tone)}>{st.text}</div>
              {node.decision?.comment && !node.stale && (
                <p className="bg-muted/60 mt-1.5 rounded-md px-2.5 py-1.5 text-[12px] leading-relaxed">
                  {node.decision.comment}
                </p>
              )}
              {node.stale && node.decision && node.status !== 'approved' && (
                <p className="text-muted-foreground mt-0.5 text-[11.5px]">
                  Su {node.decision.decision === 'approved' ? 'aprobación' : 'rechazo'} era de una
                  versión anterior ({timeAgo(node.decision.at)}) y ya no cuenta.
                </p>
              )}
            </div>
          </li>
        )
      })}
    </ol>
  )
}

/** Puntitos en fila: la cadena resumida para tarjetas y listas. */
export function ChainDots({ nodes }: { nodes: ReviewNode[] }) {
  return (
    <div className="flex items-center gap-1">
      {nodes.map((n, i) => (
        <div key={n.email} className="flex items-center gap-1" title={`${i + 1}. ${n.email} — ${labelOf(n)}`}>
          <NodeDot node={n} index={i} size="sm" />
          {i < nodes.length - 1 && <span className="bg-border h-px w-3" />}
        </div>
      ))}
    </div>
  )
}

function labelOf(n: ReviewNode) {
  return { approved: 'aprobó', rejected: 'rechazó', pending: 'le toca', waiting: 'en espera' }[n.status]
}

/** Aprobar o rechazar, solo para el nodo al que le toca. */
export function ApprovalActions({
  review,
  onApprove,
  onReject
}: {
  review: ReviewView
  onApprove: (comment: string) => Promise<void>
  onReject: (comment: string) => Promise<void>
}) {
  const [comment, setComment] = useState('')
  const [busy, setBusy] = useState<'approve' | 'reject' | null>(null)
  const node = review.nodes[review.myIndex]
  if (!review.canAct || !node) return null

  const run = async (kind: 'approve' | 'reject') => {
    setBusy(kind)
    try {
      await (kind === 'approve' ? onApprove(comment.trim()) : onReject(comment.trim()))
      setComment('')
    } finally {
      setBusy(null)
    }
  }

  return (
    <div className="border-warning/40 bg-warning/10 mt-4 rounded-lg border p-3">
      <div className="mb-2 flex items-center gap-1.5 text-[12.5px] font-semibold">
        <ShieldCheck className="text-warning size-4" /> Te toca decidir
        {node.label ? ` · ${node.label}` : ''}
      </div>
      <Textarea
        rows={2}
        value={comment}
        onChange={(e) => setComment(e.target.value)}
        placeholder="Comentario (obligatorio si rechazas)"
        className="bg-card"
      />
      <div className="mt-2 flex flex-wrap justify-end gap-2">
        <Button
          size="sm"
          variant="outline"
          className="text-destructive hover:text-destructive"
          disabled={!!busy || !comment.trim()}
          title={comment.trim() ? undefined : 'Escribe el motivo del rechazo'}
          onClick={() => run('reject')}
        >
          {busy === 'reject' ? <Loader2 className="size-4 animate-spin" /> : <X className="size-4" />}
          Rechazar
        </Button>
        <Button size="sm" disabled={!!busy} onClick={() => run('approve')}>
          {busy === 'approve' ? <Loader2 className="size-4 animate-spin" /> : <Check className="size-4" />}
          Aprobar esta versión
        </Button>
      </div>
    </div>
  )
}
