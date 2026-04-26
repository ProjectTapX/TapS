import { api } from './client'

export interface FsEntry {
  name: string
  isDir: boolean
  size: number
  modified: number
  mode: string
}

export interface FsListResp {
  path: string
  entries: FsEntry[]
}

export const fsApi = (daemonId: number) => ({
  list: (path: string) => api.get<FsListResp>(`/daemons/${daemonId}/fs/list`, { params: { path } }).then(r => r.data),
  read: (path: string) => api.get<{ content: string; size: number }>(`/daemons/${daemonId}/fs/read`, { params: { path } }).then(r => r.data),
  write: (path: string, content: string) => api.post(`/daemons/${daemonId}/fs/write`, { path, content }).then(r => r.data),
  mkdir: (path: string) => api.post(`/daemons/${daemonId}/fs/mkdir`, null, { params: { path } }).then(r => r.data),
  remove: (path: string) => api.delete(`/daemons/${daemonId}/fs/delete`, { params: { path } }).then(r => r.data),
  rename: (from: string, to: string) => api.post(`/daemons/${daemonId}/fs/rename`, { from, to }).then(r => r.data),
  copy: (from: string, to: string) => api.post(`/daemons/${daemonId}/fs/copy`, { from, to }).then(r => r.data),
  move: (from: string, to: string) => api.post(`/daemons/${daemonId}/fs/move`, { from, to }).then(r => r.data),
  zip: (paths: string[], dest: string) => api.post(`/daemons/${daemonId}/fs/zip`, { paths, dest }).then(r => r.data),
  unzip: (src: string, destDir: string) => api.post(`/daemons/${daemonId}/fs/unzip`, { src, destDir }).then(r => r.data),
})

export interface MonitorSnapshot {
  cpuPercent: number
  memTotal: number
  memUsed: number
  memPercent: number
  diskTotal: number
  diskUsed: number
  diskPercent: number
  uptimeSec: number
  timestamp: number
}

export const monitorApi = {
  snapshot: (daemonId: number) => api.get<MonitorSnapshot>(`/daemons/${daemonId}/monitor`).then(r => r.data),
  history: (daemonId: number) => api.get<MonitorSnapshot[]>(`/daemons/${daemonId}/monitor/history`).then(r => r.data),
  process: (daemonId: number, uuid: string) =>
    api.get<ProcessSnapshot>(`/daemons/${daemonId}/instances/${uuid}/process`).then(r => r.data),
}

export interface ProcessSnapshot {
  uuid: string
  pid: number
  running: boolean
  cpuPercent: number
  memBytes: number
  numThreads: number
  timestamp: number
}

export interface BackupEntry {
  name: string
  size: number
  created: number
  instanceUuid: string
}

export const backupApi = (daemonId: number, uuid: string) => ({
  list: () => api.get<{ entries: BackupEntry[] }>(`/daemons/${daemonId}/instances/${uuid}/backups`).then(r => r.data.entries ?? []),
  create: (note?: string) => api.post<BackupEntry>(`/daemons/${daemonId}/instances/${uuid}/backups`, { note }, { timeout: 10 * 60 * 1000 }).then(r => r.data),
  restore: (name: string) => api.post(`/daemons/${daemonId}/instances/${uuid}/backups/restore`, { name }, { timeout: 10 * 60 * 1000 }).then(r => r.data),
  remove: (name: string) => api.delete(`/daemons/${daemonId}/instances/${uuid}/backups`, { params: { name } }).then(r => r.data),
  downloadUrl: (name: string, token: string) =>
    `/api/daemons/${daemonId}/instances/${uuid}/backups/download?token=${encodeURIComponent(token)}&name=${encodeURIComponent(name)}`,
})
