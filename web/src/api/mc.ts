import { api } from './client'

export interface Template {
  id: string
  name: string
  description: string
  type: string
}

export interface DeployReq {
  template: string
  version?: string
  instanceName: string
  maxMemory?: string
  hostPort?: number
}

export const deployApi = {
  templates: () => api.get<Template[]>('/templates').then(r => r.data),
  paperVersions: () => api.get<{ versions: string[]; fallback: boolean; error?: string }>('/templates/paper/versions').then(r => r.data),
  deploy: (daemonId: number, body: DeployReq) =>
    api.post(`/daemons/${daemonId}/templates/deploy`, body, { timeout: 15 * 60 * 1000 }).then(r => r.data),
}

export interface McPlayer { name: string; uuid?: string }

export interface McPlayersResp {
  online: boolean
  error?: string
  description?: string
  version?: string
  max: number
  count: number
  players: McPlayer[]
}

export const mcApi = {
  players: (daemonId: number, uuid: string) =>
    api.get<McPlayersResp>(`/daemons/${daemonId}/instances/${uuid}/players`).then(r => r.data),
}
