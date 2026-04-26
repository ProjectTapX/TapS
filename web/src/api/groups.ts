import { api } from './client'

export interface NodeGroup {
  id: number
  name: string
  daemonIds: number[]
}

export interface ResolveResp {
  daemonId: number
  daemonName: string
  port: number
  portFree: boolean
  fallbackUsed: boolean
  warning?: string
}

export const groupsApi = {
  list: () => api.get<NodeGroup[]>('/groups').then(r => r.data ?? []),
  create: (name: string, daemonIds: number[]) =>
    api.post<NodeGroup>('/groups', { name, daemonIds }).then(r => r.data),
  update: (id: number, name: string, daemonIds: number[]) =>
    api.put<NodeGroup>(`/groups/${id}`, { name, daemonIds }).then(r => r.data),
  remove: (id: number) => api.delete(`/groups/${id}`),
  resolve: (id: number, body: { type?: string; port?: number }) =>
    api.post<ResolveResp>(`/groups/${id}/resolve`, body).then(r => r.data),
  createInstance: (id: number, body: any) =>
    api.post<{ daemonId: number; daemonName: string; warning?: string }>(`/groups/${id}/instances`, body).then(r => r.data),
}
