import { api } from './client'

export type TaskAction = 'command' | 'start' | 'stop' | 'restart'

export interface ScheduledTask {
  id: number
  daemonId: number
  uuid: string
  name: string
  cron: string
  action: TaskAction
  data: string
  enabled: boolean
  lastRun: string
  createdAt: string
}

export const tasksApi = (daemonId: number, uuid: string) => ({
  list: () => api.get<ScheduledTask[]>(`/daemons/${daemonId}/instances/${uuid}/tasks`).then(r => r.data),
  create: (b: Partial<ScheduledTask>) => api.post<ScheduledTask>(`/daemons/${daemonId}/instances/${uuid}/tasks`, b).then(r => r.data),
  update: (id: number, b: Partial<ScheduledTask>) => api.put<ScheduledTask>(`/daemons/${daemonId}/instances/${uuid}/tasks/${id}`, b).then(r => r.data),
  remove: (id: number) => api.delete(`/daemons/${daemonId}/instances/${uuid}/tasks/${id}`).then(r => r.data),
})

export interface ApiKeyRow {
  id: number
  userId: number
  name: string
  prefix: string
  ipWhitelist?: string
  scopes?: string
  lastUsed: string
  createdAt: string
  expiresAt?: string
  revokedAt?: string
}

export const apiKeysApi = {
  list: () => api.get<ApiKeyRow[]>('/apikeys').then(r => r.data),
  create: (b: { name: string; ipWhitelist?: string; scopes?: string; expiresAt?: string }) =>
    api.post<{ key: string; row: ApiKeyRow }>('/apikeys', b).then(r => r.data),
  remove: (id: number) => api.delete(`/apikeys/${id}`).then(r => r.data),
  revoke: (id: number) => api.post(`/apikeys/${id}/revoke`).then(r => r.data),
  revokeAll: () => api.post<{ ok: boolean; revoked: number }>('/apikeys/revoke-all').then(r => r.data),
}

export interface PermissionRow {
  userId: number
  daemonId: number
  uuid: string
  perms: number
  username: string
}

export const permsApi = {
  list: (params?: { daemonId?: number; uuid?: string; userId?: number }) =>
    api.get<PermissionRow[]>('/permissions', { params }).then(r => r.data),
  grant: (b: { userId: number; daemonId: number; uuid: string; perms?: number }) =>
    api.post<PermissionRow>('/permissions', b).then(r => r.data),
  revoke: (params: { userId: number; daemonId: number; uuid: string }) =>
    api.delete('/permissions', { params }).then(r => r.data),
}
