import axios from 'axios'
import { useAuthStore } from '@/stores/auth'

export const api = axios.create({ baseURL: '/api', timeout: 10000 })

api.interceptors.request.use((cfg) => {
  const token = useAuthStore.getState().token
  if (token) cfg.headers.Authorization = `Bearer ${token}`
  return cfg
})

// decodeJwtIat extracts only the `iat` (issued-at) field from a JWT
// payload. We don't verify the signature here — that's the server's
// job — we just need the timestamp to compare token freshness when a
// concurrent refresh might race (B18). Returns 0 on any parse failure
// so the caller falls back to "treat as older / accept new value".
function decodeJwtIat(token: string | null | undefined): number {
  if (!token) return 0
  try {
    const parts = token.split('.')
    if (parts.length < 2) return 0
    const padded = parts[1].replace(/-/g, '+').replace(/_/g, '/')
    const json = JSON.parse(atob(padded + '==='.slice((padded.length + 3) % 4)))
    const iat = Number(json?.iat)
    return Number.isFinite(iat) ? iat : 0
  } catch {
    return 0
  }
}

// Sliding renewal: when the panel decides our JWT is past its halfway
// point, it returns a freshly-signed one in the X-Refreshed-Token
// header. Stash the new token so the next request uses it; the user
// never sees a forced re-login as long as they're actively clicking.
//
// B18: under concurrent requests, both responses can carry refreshed
// tokens (server signs one for each). If the *earlier* refresh's
// response arrives last, naïvely setToken() would overwrite the newer
// one with the older — slowly draining the sliding-renewal window
// and risking a spurious token-revoked 401 mid-session if a password
// change happens in between. Decode iat and only update if strictly
// newer.
api.interceptors.response.use(
  (r) => {
    const fresh = r.headers?.['x-refreshed-token'] as string | undefined
    if (fresh && fresh.length > 0) {
      const cur = useAuthStore.getState().token
      const freshIat = decodeJwtIat(fresh)
      const curIat = decodeJwtIat(cur)
      if (freshIat > curIat) useAuthStore.getState().setToken(fresh)
    }
    return r
  },
  (err) => {
    if (err.response?.status === 401) {
      // B19: skip the global logout-and-redirect when we're mid-OIDC-
      // handoff on /login. The hash handler in pages/login does its
      // own /auth/me call with a one-shot Authorization header; if
      // that 401s, the handler already toasts and the user can just
      // try again — bouncing them back to /login (which they're
      // already on) plus blowing away their pending state would be
      // worse UX. The strict #oauth-token= check keeps this scoped
      // tightly to the actual handoff window.
      const onLogin = location.pathname === '/login'
      const inHandoff = location.hash.startsWith('#oauth-token=')
      if (!(onLogin && inHandoff)) {
        useAuthStore.getState().logout()
        if (location.pathname !== '/login') location.assign('/login')
      }
    }
    return Promise.reject(err)
  },
)
