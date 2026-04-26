import { api } from './client'

export interface DaemonView {
  id: number
  name: string
  address: string
  lastSeen: string
  createdAt: string
  connected: boolean
  os?: string
  arch?: string
  daemonVersion?: string
  requireDocker?: boolean
  dockerReady?: boolean
  displayHost?: string
  portMin?: number
  portMax?: number
  certFingerprint?: string
}

export interface ProbeResult {
  fingerprint: string
  certPem: string
}

export interface RefetchResult {
  fingerprint: string
  currentPinned: string
  matches: boolean
  certPem: string
}

export const daemonsApi = {
  list: () => api.get<DaemonView[]>('/daemons').then(r => r.data),
  publicView: (id: number) => api.get<{ id: number; name: string; displayHost: string; address?: string }>(`/daemons/${id}/public`).then(r => r.data),
  create: (b: { name: string; address: string; token: string; certFingerprint: string; displayHost?: string; portMin?: number; portMax?: number }) =>
    api.post<DaemonView>('/daemons', b).then(r => r.data),
  update: (id: number, b: Partial<{ name: string; address: string; token: string; certFingerprint: string; displayHost: string; portMin: number; portMax: number }>) =>
    api.put<DaemonView>(`/daemons/${id}`, b).then(r => r.data),
  remove: (id: number) => api.delete(`/daemons/${id}`).then(r => r.data),
  // TLS fingerprint helpers — the panel dials the daemon's TLS port
  // without verifying the chain (self-signed) and returns what it sees
  // so the operator can confirm before pinning.
  probeFingerprint: (address: string) =>
    api.post<ProbeResult>('/daemons/probe-fingerprint', { address }).then(r => r.data),
  refetchFingerprint: (id: number) =>
    api.post<RefetchResult>(`/daemons/${id}/refetch-fingerprint`).then(r => r.data),
}

// ----- SSO / OIDC -----

export interface SSOProviderPublic {
  name:        string
  displayName: string
}

export interface SSOProviderAdmin {
  id:           number
  name:         string
  displayName:  string
  enabled:      boolean
  issuer:       string
  clientId:     string
  hasSecret:    boolean
  scopes:       string
  autoCreate:   boolean
  defaultRole:  'admin' | 'user'
  emailDomains: string
  trustUnverifiedEmail: boolean
  callbackUrl:  string
  createdAt:    string
  updatedAt:    string
}

export interface SSOProviderInput {
  name?:         string
  displayName?:  string
  enabled?:      boolean
  issuer?:       string
  clientId?:     string
  clientSecret?: string  // empty on update = keep existing
  scopes?:       string
  autoCreate?:   boolean
  defaultRole?:  'admin' | 'user'
  emailDomains?: string
  trustUnverifiedEmail?: boolean
}

export interface SSOTestResult {
  ok:        boolean
  authUrl?:  string
  tokenUrl?: string
  error?:    string
}

export interface MyIdentity {
  id:                  number
  providerName:        string
  providerDisplayName: string
  email:               string
  linkedAt:            string
  lastUsedAt:          string
}

export const ssoApi = {
  publicProviders: () => api.get<SSOProviderPublic[]>('/oauth/providers').then(r => r.data),
  list:    () => api.get<SSOProviderAdmin[]>('/admin/sso/providers').then(r => r.data),
  get:     (id: number) => api.get<SSOProviderAdmin>(`/admin/sso/providers/${id}`).then(r => r.data),
  create:  (b: SSOProviderInput) => api.post<SSOProviderAdmin>('/admin/sso/providers', b).then(r => r.data),
  update:  (id: number, b: SSOProviderInput) => api.put<SSOProviderAdmin>(`/admin/sso/providers/${id}`, b).then(r => r.data),
  remove:  (id: number) => api.delete(`/admin/sso/providers/${id}`).then(r => r.data),
  test:    (issuer: string) => api.post<SSOTestResult>('/admin/sso/providers/test', { issuer }).then(r => r.data),
  myIdentities: () => api.get<MyIdentity[]>('/oauth/me/identities').then(r => r.data),
  unlinkMine:   (id: number) => api.delete(`/oauth/me/identities/${id}`).then(r => r.data),
}

export type LoginMethod = 'password-only' | 'oidc+password' | 'oidc-only'

export const authConfigApi = {
  // Public read: usable from the login page (no token) and from the
  // per-user account page (non-admins). Write stays admin-only.
  getMethod: () => api.get<{ method: LoginMethod }>('/auth/login-method').then(r => r.data.method),
  setMethod: (method: LoginMethod) => api.put('/settings/login-method', { method }).then(r => r.data),
  getPublicUrl: () => api.get<{ url: string }>('/settings/panel-public-url').then(r => r.data.url),
  setPublicUrl: (url: string) => api.put('/settings/panel-public-url', { url }).then(r => r.data),
}

export type InstanceStatus = 'stopped' | 'starting' | 'running' | 'stopping' | 'crashed' | 'hibernating'

export interface InstanceConfig {
  uuid: string
  name: string
  type: string
  workingDir: string
  command: string
  args?: string[]
  stopCmd: string
  autoStart: boolean
  autoRestart?: boolean
  restartDelay?: number
  outputEncoding?: string
  minecraftHost?: string
  minecraftPort?: number
  dockerEnv?: string[]
  dockerVolumes?: string[]
  dockerPorts?: string[]
  dockerCpu?: string
  dockerMemory?: string
  dockerDiskSize?: string
  managedVolume?: string
  autoDataDir?: boolean
  createdAt?: number
  completionWords?: string[]
  hibernationEnabled?: boolean | null
  hibernationIdleMinutes?: number
  hibernationActive?: boolean
}

export interface InstanceInfo {
  config: InstanceConfig
  status: InstanceStatus
  pid: number
  exitCode: number
}

export interface AggregateRow {
  daemonId: number
  info: InstanceInfo
}

export const instancesApi = {
  aggregate: () => api.get<AggregateRow[]>('/instances').then(r => r.data),
  list: (daemonId: number) => api.get<InstanceInfo[]>(`/daemons/${daemonId}/instances`).then(r => r.data),
  create: (daemonId: number, body: Partial<InstanceConfig>) =>
    api.post<InstanceInfo>(`/daemons/${daemonId}/instances`, body).then(r => r.data),
  update: (daemonId: number, uuid: string, body: Partial<InstanceConfig>) =>
    api.put<InstanceInfo>(`/daemons/${daemonId}/instances/${uuid}`, body).then(r => r.data),
  remove: (daemonId: number, uuid: string) =>
    api.delete(`/daemons/${daemonId}/instances/${uuid}`).then(r => r.data),
  start: (daemonId: number, uuid: string) =>
    api.post(`/daemons/${daemonId}/instances/${uuid}/start`).then(r => r.data),
  stop: (daemonId: number, uuid: string) =>
    api.post(`/daemons/${daemonId}/instances/${uuid}/stop`).then(r => r.data),
  kill: (daemonId: number, uuid: string) =>
    api.post(`/daemons/${daemonId}/instances/${uuid}/kill`).then(r => r.data),
  input: (daemonId: number, uuid: string, data: string) =>
    api.post(`/daemons/${daemonId}/instances/${uuid}/input`, { data }).then(r => r.data),
  freePort: (daemonId: number, prefer?: number) =>
    api.get<{ port: number }>(`/daemons/${daemonId}/free-port`, { params: prefer ? { prefer } : {} }).then(r => r.data.port),
  dockerStats: (daemonId: number, uuid: string) =>
    api.get<{
      name: string; running: boolean
      memBytes: number; memLimit: number; memPercent: number
      cpuPercent: number
      netRxBytes: number; netTxBytes: number
      blockReadBytes: number; blockWriteBytes: number
      diskUsedBytes?: number; diskTotalBytes?: number
    }>(`/daemons/${daemonId}/instances/${uuid}/dockerstats`).then(r => r.data),
  dockerStatsAll: (daemonId: number) =>
    api.get<{ items: { name: string; running: boolean; memBytes: number; memLimit: number; memPercent: number; cpuPercent: number; diskUsedBytes?: number; diskTotalBytes?: number }[] }>(`/daemons/${daemonId}/instances-dockerstats`).then(r => r.data.items ?? []),
  playersAll: (daemonId: number) =>
    api.get<{ items: { uuid: string; online: boolean; count: number; max: number }[] }>(`/daemons/${daemonId}/instances-players`).then(r => r.data.items ?? []),
}
