import { api } from './client'
import { useAuthStore } from '@/stores/auth'

export interface DockerImage {
  id: string
  repository: string
  tag: string
  size: number
  created: number
  displayName?: string
  description?: string
}

export interface DockerImagesResp {
  available: boolean
  error?: string
  images: DockerImage[]
}

export type PullSseEvent =
  | { type: 'start'; image: string; pullId: string }
  | { type: 'line';  line: string }
  | { type: 'done';  error: string }

export const dockerApi = (daemonId: number) => ({
  images: () => api.get<DockerImagesResp>(`/daemons/${daemonId}/docker/images`).then(r => r.data),
  remove: (id: string) => api.delete(`/daemons/${daemonId}/docker/remove`, { params: { id } }).then(r => r.data),
  setAlias: (ref: string, displayName: string) =>
    api.put(`/daemons/${daemonId}/docker/images/${encodeURIComponent(ref)}/alias`, { displayName }).then(r => r.data),

  // pullStream returns an AsyncGenerator yielding parsed SSE events.
  // Caller can `for await` to consume; cancel by calling abort().
  pullStream: async function* (image: string, signal?: AbortSignal): AsyncGenerator<PullSseEvent> {
    const token = useAuthStore.getState().token ?? ''
    const res = await fetch(`/api/daemons/${daemonId}/docker/pull`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json', Authorization: `Bearer ${token}` },
      body: JSON.stringify({ image }),
      signal,
    })
    if (!res.ok || !res.body) {
      throw new Error(`HTTP ${res.status}`)
    }
    const reader = res.body.getReader()
    const decoder = new TextDecoder()
    let buf = ''
    while (true) {
      const { value, done } = await reader.read()
      if (done) return
      buf += decoder.decode(value, { stream: true })
      // SSE chunks are separated by \n\n
      const parts = buf.split('\n\n')
      buf = parts.pop() ?? ''
      for (const part of parts) {
        const line = part.split('\n').find(l => l.startsWith('data: '))
        if (!line) continue
        try {
          yield JSON.parse(line.slice(6)) as PullSseEvent
        } catch { /* ignore malformed */ }
      }
    }
  },
})
