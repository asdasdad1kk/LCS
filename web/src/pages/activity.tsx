import { useEffect, useState } from 'react'
import { Activity } from 'lucide-react'
import { api, type ActivityEvent } from '@/lib/api'
import { Skeleton } from '@/components/ui/separator'
import { PageHeader, EmptyState } from '@/components/page-header'
import { ActivityRow } from '@/pages/dashboard'

export function ActivityPage() {
  const [events, setEvents] = useState<ActivityEvent[]>([])
  const [loading, setLoading] = useState(true)

  useEffect(() => {
    api
      .activity()
      .then(setEvents)
      .finally(() => setLoading(false))
  }, [])

  return (
    <div className="scrollbar-thin flex-1 overflow-y-auto">
      <PageHeader
        eyebrow={
          <>
            <Activity className="size-3.5" /> auditoría
          </>
        }
        title="Actividad"
        description="Quién abrió cada rama, qué se subió y qué se integró. Los commits quedan en el repositorio del NAS."
      />

      <div className="px-6 pb-10">
        {loading ? (
          <div className="space-y-2">
            {[0, 1, 2, 3, 4].map((i) => (
              <Skeleton key={i} className="h-14 w-full" />
            ))}
          </div>
        ) : events.length === 0 ? (
          <EmptyState icon={Activity} title="Sin movimientos" description="Todavía no hay eventos." />
        ) : (
          <div className="divide-y overflow-hidden rounded-lg border bg-card">
            {events.map((e) => (
              <ActivityRow key={e.id} event={e} />
            ))}
          </div>
        )}
      </div>
    </div>
  )
}
