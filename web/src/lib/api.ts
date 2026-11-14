export type Project = {
  id: string
  name: string
  description: string
  createdAt: number
  updatedAt: number
  drawings?: number
  openBranches?: number
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
  files?: BranchFile[]
  revisions?: Revision[]
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
  bootstrap: () => req<Bootstrap>('/api/bootstrap'),
  stats: () => req<Record<string, number | string>>('/api/stats'),
  activity: (project?: string) =>
    req<ActivityEvent[]>(`/api/activity${project ? `?project=${project}` : ''}`),

  projects: () => req<Project[]>('/api/projects'),
  createProject: (body: { name: string; description?: string }) =>
    req<Project>('/api/projects', { method: 'POST', body: JSON.stringify(body) }),
  project: (id: string) => req<ProjectDetail>(`/api/projects/${id}`),
  syncProject: (id: string) => req<{ ok: boolean }>(`/api/projects/${id}/sync`, { method: 'POST' }),
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

  downloadUrl: (id: string) => `/api/branches/${id}/download`,
  fileUrl: (drawingId: string, ref?: string) =>
    `/api/drawings/${drawingId}/file${ref ? `?ref=${encodeURIComponent(ref)}` : ''}`
}

export function uploadWithProgress<T>(
  url: string,
  form: FormData,
  onProgress?: (pct: number) => void
): Promise<T> {
  return new Promise((resolve, reject) => {
    const xhr = new XMLHttpRequest()
    xhr.open('POST', url)
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
