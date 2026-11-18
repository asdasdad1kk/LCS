export type Project = {
  id: string
  name: string
  description: string
  createdAt: number
  updatedAt: number
  drawings?: number
  openBranches?: number
  /** Master al que pertenece este proyecto (vacío si es un proyecto raíz). */
  parentId: string
  /** Zona: planos que todavía no se copiaron al master. */
  pendingChanges?: number
  /** Master: cantidad de zonas y cuántas están atrasadas. */
  zones?: number
  pendingZones?: number
  integratedAt?: number
}

export type ZoneSummary = Project & { drawings: number; pendingChanges: number }

export type Integration = {
  changed: string[]
  removed: string[]
  integratedAt: number
}

export type Drawing = {
  id: string
  projectId: string
  path: string
  name: string
  mainBlob: string
  size: number
  updatedAt: number
}

export type LayoutInfo = {
  name: string
  btrId: string
  tabOrder: number
}

export type BranchStatus = 'open' | 'review' | 'merged' | 'closed'

export type BranchFile = {
  branchId: string
  path: string
  baseBlob: string
  headBlob: string
  changed: boolean
  sizeBytes: number
}

export type Branch = {
  id: string
  projectId: string
  name: string
  ref: string
  title: string
  author: string
  status: BranchStatus
  baseCommit: string
  headCommit: string
  createdAt: number
  updatedAt: number
  mergedAt: number
  authorEmail?: string
  files?: BranchFile[]
  revisions?: Revision[]
}

// ---------------------------------------------------------------- identidad y aprobaciones

export type Identity = {
  email: string
  name: string
  /** false: el correo se dedujo (no vino de la sesión de Windows ni de LCS_EMAIL). */
  verified: boolean
  source: 'config' | 'upn' | 'whoami' | 'dominio' | 'local'
}

export type Approver = { email: string; label: string }

export type ApproversView = {
  approvers: (Approver & { position: number })[]
  owner: string
  canEdit: boolean
  me: Identity
}

export type NodeStatus = 'approved' | 'rejected' | 'pending' | 'waiting'

export type Decision = {
  id: string
  email: string
  name: string
  decision: 'approved' | 'rejected'
  comment: string
  tree: string
  commit: string
  at: number
}

export type ReviewNode = {
  position: number
  email: string
  label: string
  status: NodeStatus
  /** Es el nodo que puede decidir ahora. */
  active: boolean
  /** Decidió, pero sobre una versión anterior de los planos. */
  stale: boolean
  isAuthor: boolean
  isMe: boolean
  decision?: Decision
}

export type TimelineKind = 'opened' | 'upload' | 'approved' | 'rejected' | 'merged' | 'closed'

export type TimelineItem = {
  kind: TimelineKind
  at: number
  actor: string
  email: string
  message: string
  commit?: string
  size?: number
  node: number
  label?: string
  stale: boolean
}

export type Overlap = {
  branchId: string
  title: string
  author: string
  authorEmail: string
  status: string
  paths: string[]
}

export type ReviewView = {
  required: boolean
  hasChanges: boolean
  /** Ya se subió al menos una versión (aunque sea idéntica a main). */
  uploaded?: boolean
  /** Se puede integrar: no hay cadena, o todos aprobaron esta versión. */
  complete: boolean
  nodes: ReviewNode[]
  next: number
  me: Identity
  myIndex: number
  canAct: boolean
  timeline: TimelineItem[]
  overlaps: Overlap[]
}

export type PanelItem = {
  branch: Branch & { projectName: string }
  files: number
  nodes: ReviewNode[]
  myIndex: number
  next: number
}

export type ApprovalsPanel = {
  me: Identity
  pending: PanelItem[]
  queued: PanelItem[]
  decided: PanelItem[]
}

export type Editor = {
  branchId: string
  title: string
  author: string
  authorEmail: string
  createdAt: number
}

export type Revision = {
  id: string
  projectId: string
  drawingPath: string
  commitSha: string
  blobSha: string
  size: number
  author: string
  message: string
  branchId: string
  createdAt: number
}

export type ActivityEvent = {
  id: number
  projectId: string
  branchId: string
  kind: string
  actor: string
  payload: string
  createdAt: number
}

export type ProjectDetail = {
  project: Project
  parent: Project | null
  children: ZoneSummary[]
  integration: Integration | null
  /** Plano → personas que lo tienen en edición (tareas abiertas). */
  editing?: Record<string, Editor[]>
  drawings: Drawing[]
  branches: Branch[]
  events: ActivityEvent[]
  mainHead: string
  workspace: string
  usage?: { repo: number; clone: number }
}

export type DrawingDetail = {
  drawing: Drawing
  layouts: LayoutInfo[]
  meta: Record<string, unknown>
  revisions: Revision[]
  branches: Branch[]
}

export type Bootstrap = {
  app: string
  version: string
  actor: string
  me: Identity
  localUrl?: string
  config: {
    projects: number
    drawings: number
    openBranches: number
    workspace: string
    actor: string
  }
}

export class ApiError extends Error {
  status: number
  payload: any
  constructor(status: number, payload: any) {
    super(payload?.error ?? `HTTP ${status}`)
    this.status = status
    this.payload = payload
  }
}

async function req<T>(url: string, init?: RequestInit): Promise<T> {
  const res = await fetch(url, {
    ...init,
    headers: {
      ...(init?.body instanceof FormData ? {} : { 'Content-Type': 'application/json' }),
      ...init?.headers
    }
  })
  const text = await res.text()
  const data = text ? safeParse(text) : null
  if (!res.ok) throw new ApiError(res.status, data)
  return data as T
}

function safeParse(text: string) {
  try {
    return JSON.parse(text)
  } catch {
    return { error: text }
  }
}

export const api = {
  bootstrap: () =>
    req<Bootstrap>('/api/bootstrap').then((b) => {
      if (b.localUrl) directBase = b.localUrl
      return b
    }),
  stats: () => req<Record<string, number | string>>('/api/stats'),
  activity: (project?: string) =>
    req<ActivityEvent[]>(`/api/activity${project ? `?project=${project}` : ''}`),

  projects: () => req<Project[]>('/api/projects'),
  createProject: (body: { name: string; description?: string; parentId?: string }) =>
    req<Project>('/api/projects', { method: 'POST', body: JSON.stringify(body) }),
  project: (id: string) => req<ProjectDetail>(`/api/projects/${id}`),
  deleteProject: (id: string) =>
    req<{ deleted: string[] }>(`/api/projects/${id}`, { method: 'DELETE' }),
  syncProject: (id: string) => req<{ ok: boolean }>(`/api/projects/${id}/sync`, { method: 'POST' }),
  integrateProject: (id: string, strategy?: string) =>
    req<{ commit: string; paths: string[] }>(`/api/projects/${id}/integrate`, {
      method: 'POST',
      body: JSON.stringify({ strategy })
    }),
  branches: (id: string, status = 'all') =>
    req<Branch[]>(`/api/projects/${id}/branches?status=${status}`),
  createBranch: (
    id: string,
    body: { drawings: string[]; title: string; notes?: string; author?: string }
  ) => req<Branch>(`/api/projects/${id}/branches`, { method: 'POST', body: JSON.stringify(body) }),

  drawing: (id: string) => req<DrawingDetail>(`/api/drawings/${id}`),
  revisions: (id: string) => req<Revision[]>(`/api/drawings/${id}/revisions`),
  restore: (id: string, commit: string) =>
    req<Drawing>(`/api/drawings/${id}/restore`, {
      method: 'POST',
      body: JSON.stringify({ commit })
    }),
  saveLayouts: (id: string, body: { layouts: LayoutInfo[]; meta?: Record<string, unknown> }) =>
    req<{ ok: boolean }>(`/api/drawings/${id}/layouts`, {
      method: 'PUT',
      body: JSON.stringify(body)
    }),

  branch: (id: string) => req<Branch>(`/api/branches/${id}`),
  mergeBranch: (id: string, strategy?: string) =>
    req<{ commit: string; paths: string[] }>(`/api/branches/${id}/merge`, {
      method: 'POST',
      body: JSON.stringify({ strategy })
    }),
  closeBranch: (id: string) =>
    req<{ ok: boolean }>(`/api/branches/${id}/close`, { method: 'POST' }),
  snapshots: (id: string) => req<Snapshot[]>(`/api/branches/${id}/snapshots`),
  addSnapshot: (
    id: string,
    image: Blob,
    meta: { caption?: string; drawing?: string; layout?: string }
  ) => {
    const form = new FormData()
    form.append('image', image, 'recorte.png')
    for (const [k, v] of Object.entries(meta)) if (v) form.append(k, v)
    return req<Snapshot>(`/api/branches/${id}/snapshots`, { method: 'POST', body: form })
  },
  snapshotUrl: (id: string, sid: string) => `${directBase}/api/branches/${id}/snapshots/${sid}`,

  me: () => req<Identity>('/api/me'),
  approvers: (projectId: string) => req<ApproversView>(`/api/projects/${projectId}/approvers`),
  setApprovers: (projectId: string, approvers: Approver[]) =>
    req<ApproversView>(`/api/projects/${projectId}/approvers`, {
      method: 'PUT',
      body: JSON.stringify({ approvers })
    }),
  /** Quién tiene cada plano en edición (consulta la carpeta compartida). */
  editing: (projectId: string) => req<Record<string, Editor[]>>(`/api/projects/${projectId}/editing`),
  review: (branchId: string) => req<ReviewView>(`/api/branches/${branchId}/review`),
  approve: (branchId: string, comment?: string) =>
    req<ReviewView>(`/api/branches/${branchId}/approve`, {
      method: 'POST',
      body: JSON.stringify({ comment })
    }),
  reject: (branchId: string, comment: string) =>
    req<ReviewView>(`/api/branches/${branchId}/reject`, {
      method: 'POST',
      body: JSON.stringify({ comment })
    }),
  approvals: () => req<ApprovalsPanel>('/api/approvals'),

  downloadUrl: (id: string) => `/api/branches/${id}/download`,
  fileUrl: (drawingId: string, ref?: string) =>
    `/api/drawings/${drawingId}/file${ref ? `?ref=${encodeURIComponent(ref)}` : ''}`
}

// En la ventana nativa las subidas van directo al puerto local: el servidor de
// assets de Wails no entrega bien los cuerpos multipart.
let directBase = ''

export function uploadWithProgress<T>(
  url: string,
  form: FormData,
  onProgress?: (pct: number) => void
): Promise<T> {
  return new Promise((resolve, reject) => {
    const xhr = new XMLHttpRequest()
    const target = directBase && location.origin !== directBase ? directBase + url : url
    xhr.open('POST', target)
    xhr.upload.onprogress = (e) => {
      if (e.lengthComputable && onProgress) onProgress(Math.round((e.loaded / e.total) * 100))
    }
    xhr.onload = () => {
      const data = safeParse(xhr.responseText || '{}')
      if (xhr.status >= 200 && xhr.status < 300) resolve(data as T)
      else reject(new ApiError(xhr.status, data))
    }
    xhr.onerror = () => reject(new ApiError(0, { error: 'falló la conexión con LCS' }))
    xhr.send(form)
  })
}

export function importDrawings(projectId: string, files: File[], onProgress?: (pct: number) => void) {
  const form = new FormData()
  for (const f of files) form.append('file', f, f.name)
  return uploadWithProgress<{ ok: boolean; count: number }>(
    `/api/projects/${projectId}/import`,
    form,
    onProgress
  )
}

export function uploadToBranch(
  branchId: string,
  file: File,
  message: string,
  onProgress?: (pct: number) => void
) {
  const form = new FormData()
  form.append('file', file, file.name)
  form.append('message', message)
  return uploadWithProgress<Branch>(`/api/branches/${branchId}/upload`, form, onProgress)
}

/** Recorte de la zona de un layout que se va a cambiar, guardado con la tarea. */
export type Snapshot = {
  id: string
  caption: string
  author: string
  email: string
  drawing?: string
  layout?: string
  at: number
  size: number
}
