import { api } from './client'

export interface ServerProvider {
  id: string
  displayName: string
  hasBuilds: boolean
  needsImage: boolean
}

export type DeployStage =
  | 'queued' | 'validating' | 'clearing' | 'downloading' | 'installing' | 'configuring' | 'done' | 'error'

export interface DeployStatus {
  uuid: string
  active: boolean
  stage: DeployStage
  percent: number
  bytesDone: number
  bytesTotal: number
  messageKey?: string
  message: string
  error?: string
  startedAt: number
  finishedAt?: number
}

export const serverDeployApi = {
  types: () => api.get<ServerProvider[]>('/serverdeploy/types').then(r => r.data ?? []),
  versions: (type: string) =>
    api.get<{ versions: string[] }>(`/serverdeploy/versions?type=${encodeURIComponent(type)}`)
      .then(r => r.data.versions ?? []),
  builds: (type: string, version: string) =>
    api.get<{ builds: string[] }>(`/serverdeploy/builds?type=${encodeURIComponent(type)}&version=${encodeURIComponent(version)}`)
      .then(r => r.data.builds ?? []),
  start: (daemonId: number, uuid: string, body: { type: string; version: string; build?: string; acceptEula: boolean }) =>
    api.post(`/daemons/${daemonId}/instances/${uuid}/serverdeploy`, body),
  status: (daemonId: number, uuid: string) =>
    api.get<DeployStatus>(`/daemons/${daemonId}/instances/${uuid}/serverdeploy/status`).then(r => r.data),
}
