import { api } from './client'

export interface Volume {
  name: string
  sizeBytes: number
  usedBytes?: number
  fsType: string
  imagePath: string
  mountPath: string
  mounted: boolean
  createdAt: number
}

export interface VolumeListResp {
  available: boolean
  error?: string
  volumes: Volume[]
}

export const volumesApi = (daemonId: number) => ({
  list: () => api.get<VolumeListResp>(`/daemons/${daemonId}/volumes`).then(r => r.data),
  create: (b: { name: string; sizeBytes: number; fsType?: string }) =>
    api.post<Volume>(`/daemons/${daemonId}/volumes`, b, { timeout: 60_000 }).then(r => r.data),
  remove: (name: string) =>
    api.delete(`/daemons/${daemonId}/volumes`, { params: { name } }).then(r => r.data),
})
