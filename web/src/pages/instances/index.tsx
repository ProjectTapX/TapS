import React, { useEffect, useMemo, useState } from 'react'
import { Button, Table, Space, Modal, Form, Input, Select, Switch, Popconfirm, App, Card, Tag, Alert, InputNumber, Tooltip } from 'antd'
import {
  ReloadOutlined, CaretRightOutlined, PoweroffOutlined, SafetyOutlined, RocketOutlined,
  PlusOutlined, DeleteOutlined, SettingOutlined,
} from '@ant-design/icons'
import { Link } from 'react-router-dom'
import { useTranslation } from 'react-i18next'
import { daemonsApi, instancesApi, type AggregateRow, type DaemonView, type InstanceStatus } from '@/api/resources'
import { dockerApi, type DockerImage } from '@/api/docker'
import { groupsApi, type NodeGroup, type ResolveResp } from '@/api/groups'
import { useAuthStore } from '@/stores/auth'
import PermissionDrawer from '@/components/PermissionDrawer'
import QuickDeploy from '@/components/QuickDeploy'
import SetupWizard, { needsSetup } from '@/components/SetupWizard'
import ServerDeployModal from '@/components/ServerDeployModal'
import { copyToClipboard } from '@/utils/clipboard'
import PageHeader from '@/components/PageHeader'
import StatusBadge from '@/components/StatusBadge'
import { formatApiError } from '@/api/errors'

const STATUS_VARIANT: Record<InstanceStatus, 'success' | 'warning' | 'danger' | 'neutral' | 'processing'> = {
  stopped: 'neutral', starting: 'processing', running: 'success', stopping: 'processing', crashed: 'danger', hibernating: 'warning',
}
type DisplayStatus = InstanceStatus | 'configuring'
const DISPLAY_VARIANT: Record<DisplayStatus, 'success' | 'warning' | 'danger' | 'neutral' | 'processing'> = {
  ...STATUS_VARIANT,
  configuring: 'warning',
}
function displayStatusOf(info: AggregateRow['info']): DisplayStatus {
  if (info.status === 'stopped' && needsSetup(info)) return 'configuring'
  return info.status
}
const STATUS_PRIORITY: Record<DisplayStatus, number> = {
  crashed: 0, configuring: 1, running: 2, hibernating: 3, starting: 4, stopping: 5, stopped: 6,
}

// splitArgs handles a single line of "shell-ish" args, respecting "double quotes".
//   `java -jar /a/b.jar nogui`  → ["java","-jar","/a/b.jar","nogui"]
//   `sh -lc "echo hi && ls"`    → ["sh","-lc","echo hi && ls"]
function splitArgs(line: string): string[] {
  const out: string[] = []
  let cur = ''
  let inQuote = false
  for (let i = 0; i < line.length; i++) {
    const c = line[i]
    if (c === '"') { inQuote = !inQuote; continue }
    if (!inQuote && (c === ' ' || c === '\t')) {
      if (cur) { out.push(cur); cur = '' }
      continue
    }
    cur += c
  }
  if (cur) out.push(cur)
  return out
}

// Pull the host port out of a docker -p spec like "25565:25565",
// "25565:25565/udp", or "0.0.0.0:25565:25565".
function parseHostPort(spec: string): number | null {
  const t = spec.trim()
  if (!t) return null
  const body = t.includes('/') ? t.slice(0, t.indexOf('/')) : t
  const parts = body.split(':')
  const hostStr = parts.length === 3 ? parts[1] : parts[0]
  const n = Number(hostStr)
  return Number.isFinite(n) && n > 0 ? n : null
}

// Diff helpers: poll responses overwhelmingly contain identical data, and
// blindly setState'ing forces antd's Table to re-render every row, which
// (a) flickers, (b) drops user-side sort/filter state, and (c) collapses
// any open Popconfirm. We compare incoming items to the current state
// element-wise and reuse object references for rows that haven't moved.
function rowKey(r: AggregateRow): string {
  return `${r.daemonId}/${r.info.config.uuid}`
}
function rowEqual(a: AggregateRow, b: AggregateRow): boolean {
  return a.daemonId === b.daemonId
    && a.info.status === b.info.status
    && a.info.pid === b.info.pid
    && a.info.exitCode === b.info.exitCode
    && a.info.config.name === b.info.config.name
    && a.info.config.type === b.info.config.type
}
function mergeRows(prev: AggregateRow[], next: AggregateRow[]): AggregateRow[] {
  if (prev.length !== next.length) {
    // shape changed (added/removed); fall through to full replace
    return next
  }
  const byKey = new Map(prev.map(r => [rowKey(r), r]))
  let dirty = false
  const merged = next.map(n => {
    const p = byKey.get(rowKey(n))
    if (p && rowEqual(p, n)) return p
    dirty = true
    return n
  })
  return dirty ? merged : prev
}
function sameDaemons(a: DaemonView[], b: DaemonView[]): boolean {
  if (a.length !== b.length) return false
  for (let i = 0; i < a.length; i++) {
    const x = a[i], y = b[i]
    if (x.id !== y.id || x.name !== y.name || x.connected !== y.connected || x.requireDocker !== y.requireDocker) return false
  }
  return true
}

// renderGroupResolveStatus returns the inline status line shown under
// the host port input when a group target is selected. It surfaces what
// node would be picked, whether the typed port is free, and any
// scheduler warning (e.g. disk-fallback path).
function renderGroupResolveStatus(r: ResolveResp | null, busy: boolean, t: (k: string, v?: any) => string): React.ReactNode {
  if (busy) return <span style={{ color: 'var(--taps-text-muted)' }}>{t('groups.resolving')}</span>
  if (!r) return <span style={{ color: 'var(--taps-text-muted)' }}>{t('groups.resolveIdle')}</span>
  const node = r.daemonName
  const lines: React.ReactNode[] = []
  if (r.port > 0 && r.portFree) {
    lines.push(<span key="ok" style={{ color: '#10b981' }}>✓ {t('groups.resolveOk', { node, port: r.port })}</span>)
  } else if (r.port > 0 && !r.portFree) {
    lines.push(<span key="busy" style={{ color: '#ef4444' }}>✗ {t('groups.resolveBusy', { node, port: r.port })}</span>)
  } else {
    lines.push(<span key="nofree" style={{ color: '#ef4444' }}>✗ {t('groups.resolveNoPort', { node })}</span>)
  }
  if (r.fallbackUsed || r.warning) {
    lines.push(<span key="warn" style={{ color: '#f59e0b', marginLeft: 8 }}>⚠ {r.warning || t('groups.resolveFallback')}</span>)
  }
  return <span>{lines}</span>
}

export default function InstancesPage() {
  const { t } = useTranslation()
  const { message } = App.useApp()
  const isAdmin = useAuthStore((s) => s.user?.role === 'admin')
  const [rows, setRows] = useState<AggregateRow[]>([])
  const [daemons, setDaemons] = useState<DaemonView[]>([])
  const [loading, setLoading] = useState(false)
  const [open, setOpen] = useState(false)
  const [deployOpen, setDeployOpen] = useState(false)
  const [permTarget, setPermTarget] = useState<AggregateRow | null>(null)
  const [setupTarget, setSetupTarget] = useState<AggregateRow | null>(null)
  const [form] = Form.useForm()
  // target encodes either "n-<daemonId>" or "g-<groupId>" so a single
  // Select can offer both. Derived values feed the rest of the form.
  const target: string | undefined = Form.useWatch('target', form)
  const watchedHostPort: number | undefined = Form.useWatch('hostPort', form)
  const watchedType: string | undefined = Form.useWatch('type', form)
  const selectedDaemonId = target?.startsWith('n-') ? Number(target.slice(2)) : undefined
  const selectedGroupId = target?.startsWith('g-') ? Number(target.slice(2)) : undefined
  const [groups, setGroups] = useState<NodeGroup[]>([])
  const [resolveStatus, setResolveStatus] = useState<ResolveResp | null>(null)
  const [resolving, setResolving] = useState(false)
  // After-create deploy: when user checks "deploy server after create",
  // we open ServerDeployModal pre-targeted at the freshly created
  // (daemonId, uuid).
  const [postDeploy, setPostDeploy] = useState<{ daemonId: number; uuid: string } | null>(null)
  const [imagesByDaemon, setImagesByDaemon] = useState<Record<number, DockerImage[]>>({})
  // Player counts per instance UUID, refreshed on the same poll cadence
  // as the instance list. Only populated for running MC servers; missing
  // entries render as "—" so the column doesn't shift width.
  const [playersByUuid, setPlayersByUuid] = useState<Record<string, { count: number; max: number }>>({})
  const [search, setSearch] = useState('')

  // Compose the externally-visible game address ("host:port") for a row.
  // Returns '' when the instance has no docker port mapping or the daemon
  // has no resolvable host — same conditions under which the address
  // column renders a dash. Reused for the search filter so what the user
  // sees on screen is what they can match against.
  const addressOf = (r: AggregateRow): string => {
    const cfg = r.info.config
    if (cfg.type !== 'docker' || !cfg.dockerPorts || cfg.dockerPorts.length === 0) return ''
    const port = parseHostPort(cfg.dockerPorts[0])
    if (!port) return ''
    const d = daemons.find(x => x.id === r.daemonId)
    const host = (d?.displayHost && d.displayHost.trim()) ||
      (d?.address ? d.address.replace(/:\d+$/, '') : '')
    if (!host) return ''
    return `${host}:${port}`
  }

  const fetchImagesFor = async (id: number) => {
    if (imagesByDaemon[id]) return
    try {
      const r = await dockerApi(id).images()
      if (r.available) {
        setImagesByDaemon(s => ({ ...s, [id]: r.images ?? [] }))
      }
    } catch { /* ignore */ }
  }
  useEffect(() => {
    if (selectedDaemonId && open) {
      fetchImagesFor(selectedDaemonId)
      // Auto-suggest a free host port if the user hasn't filled one in yet.
      const cur = form.getFieldValue('hostPort')
      if (!cur) {
        instancesApi.freePort(selectedDaemonId).then(p => {
          // Re-check: don't clobber a value the user typed while we waited.
          if (!form.getFieldValue('hostPort')) form.setFieldValue('hostPort', p)
        }).catch(() => { /* ignore */ })
      }
    }
  }, [selectedDaemonId, open])

  // Load the group list once when the modal opens; admin-only API,
  // failures just leave the dropdown empty.
  useEffect(() => {
    if (!open || !isAdmin) return
    groupsApi.list().then(setGroups).catch(() => { /* ignore */ })
  }, [open, isAdmin])

  // Real-time resolve when target=group: ask panel which node would be
  // picked and whether the typed port (if any) is free there. Debounced
  // 300ms so typing doesn't spam the resolver.
  useEffect(() => {
    if (!open || !selectedGroupId) { setResolveStatus(null); return }
    let cancelled = false
    setResolving(true)
    const handle = setTimeout(async () => {
      try {
        const r = await groupsApi.resolve(selectedGroupId, {
          type: watchedType ?? 'docker',
          port: watchedHostPort ?? 0,
        })
        if (!cancelled) setResolveStatus(r)
      } catch {
        if (!cancelled) setResolveStatus(null)
      } finally {
        if (!cancelled) setResolving(false)
      }
    }, 300)
    return () => { cancelled = true; clearTimeout(handle) }
  }, [selectedGroupId, watchedHostPort, watchedType, open])

  const imageOptions = useMemo(() => {
    const list = (selectedDaemonId && imagesByDaemon[selectedDaemonId]) || []
    return list
      .filter(im => im.repository && im.repository !== '<none>' && im.tag && im.tag !== '<none>')
      .map(im => {
        const ref = `${im.repository}:${im.tag}`
        return { label: im.displayName || ref, value: ref, size: im.size }
      })
  }, [selectedDaemonId, imagesByDaemon])

  // load(silent=true) avoids the loading spinner and the wholesale state
  // replacement that would otherwise reset selection / sort / scroll on
  // every poll. We diff incoming data against current state and only set
  // the parts that actually changed (and even then, mutate row-by-row to
  // preserve referential identity for unchanged rows).
  const load = async (silent = false) => {
    if (!silent) setLoading(true)
    try {
      const r = await instancesApi.aggregate()
      let ds: DaemonView[] = []
      if (isAdmin) {
        ds = await daemonsApi.list()
      } else {
        // Non-admins can't read /api/daemons (admin-only). Fetch the
        // public view for each daemon they have at least one instance on
        // so we can render game addresses (host:port) in the table.
        const ids = Array.from(new Set(r.map(x => x.daemonId)))
        const pubs = await Promise.all(ids.map(async id => {
          try { return await daemonsApi.publicView(id) } catch { return null }
        }))
        ds = pubs.filter((p): p is NonNullable<typeof p> => !!p).map(p => ({
          id: p.id, name: p.name, address: '', displayHost: p.displayHost,
          lastSeen: '', createdAt: '', connected: false,
        } as DaemonView))
      }
      setRows(prev => mergeRows(prev, r))
      setDaemons(prev => sameDaemons(prev, ds) ? prev : ds)
      // Batch-fetch online player counts per daemon (one RPC per daemon
      // that has at least one running docker instance, daemon does N
      // concurrent SLP pings internally). Failures are silent so a
      // non-MC instance just doesn't get a number.
      const daemonsWithRunning = Array.from(new Set(
        r.filter(it => it.info.config.type === 'docker' && it.info.status === 'running')
          .map(it => it.daemonId)
      ))
      const next: Record<string, { count: number; max: number }> = {}
      await Promise.all(daemonsWithRunning.map(async id => {
        try {
          const items = await instancesApi.playersAll(id)
          for (const it of items) next[it.uuid] = { count: it.count, max: it.max }
        } catch { /* ignore */ }
      }))
      setPlayersByUuid(prev => {
        // Equal-content guard so the table doesn't re-render every poll.
        const prevKeys = Object.keys(prev)
        const nextKeys = Object.keys(next)
        if (prevKeys.length === nextKeys.length
          && prevKeys.every(k => prev[k].count === next[k]?.count && prev[k].max === next[k]?.max)) {
          return prev
        }
        return next
      })
    } finally { if (!silent) setLoading(false) }
  }
  useEffect(() => {
    load()
    const t = setInterval(() => load(true), 3000)
    return () => clearInterval(t)
  }, [])

  // Sort: crashed → configuring → running → starting → stopping → stopped.
  // Within the same status, newer instances first.
  const sortedRows = useMemo(() => {
    const q = search.trim().toLowerCase()
    const filtered = q
      ? rows.filter(r => {
          const name = (r.info.config.name || '').toLowerCase()
          const uuid = (r.info.config.uuid || '').toLowerCase()
          const pid = r.info.pid ? String(r.info.pid) : ''
          const addr = addressOf(r).toLowerCase()
          return name.includes(q) || uuid.includes(q) || pid.includes(q) || addr.includes(q)
        })
      : rows
    return [...filtered].sort((a, b) => {
      const sa = displayStatusOf(a.info)
      const sb = displayStatusOf(b.info)
      const pa = STATUS_PRIORITY[sa] ?? 99
      const pb = STATUS_PRIORITY[sb] ?? 99
      if (pa !== pb) return pa - pb
      return (b.info.config.createdAt ?? 0) - (a.info.config.createdAt ?? 0)
    })
  }, [rows, search, daemons])

  const onCreate = async () => {
    const v = await form.validateFields()
    const isDocker = (v.type ?? 'generic') === 'docker'
    const isGroupTarget = typeof v.target === 'string' && v.target.startsWith('g-')
    const isNodeTarget = typeof v.target === 'string' && v.target.startsWith('n-')
    if (!isGroupTarget && !isNodeTarget) {
      message.error(t('instance.targetRequired'))
      return
    }

    // ----- Group path: panel does the resolve, port pick, and create.
    if (isGroupTarget) {
      const groupId = Number(v.target.slice(2))
      try {
        const r = await groupsApi.createInstance(groupId, {
          name: v.name, type: v.type ?? 'generic', command: v.command,
          workingDir: v.workingDir ?? '', stopCmd: v.stopCmd || 'stop', autoStart: !!v.autoStart,
          args: v.argsText ? splitArgs(v.argsText) : undefined,
          hostPort: v.hostPort || undefined,
          containerPort: v.containerPort || undefined,
          dockerMemory: v.dockerMemory || undefined,
          dockerCpu: v.dockerCpu || undefined,
          dockerDiskSize: v.dockerDiskSize || undefined,
        })
        if (r.warning) {
          message.warning(`${t('common.success')} → ${r.daemonName} (${r.warning})`)
        } else {
          message.success(`${t('common.success')} → ${r.daemonName}`)
        }
        setOpen(false); form.resetFields(); setResolveStatus(null); load()
        // Auto-open server deploy modal pre-targeted at the new instance.
        const newUuid = (r as any)?.info?.config?.uuid
        if (v.autoDeploy && newUuid) {
          setPostDeploy({ daemonId: r.daemonId, uuid: newUuid })
        }
      } catch (e: any) {
        message.error(formatApiError(e, 'common.error'))
      }
      return
    }

    // ----- Node path: existing logic, unchanged behavior.
    const daemonId = Number(v.target.slice(2))
    try {
      let hostPort: number | undefined = v.hostPort
      if (isDocker) {
        const d = daemons.find(x => x.id === daemonId)
        const lo = d?.portMin || 25565
        const hi = d?.portMax || 25600
        if (!hostPort) {
          try { hostPort = await instancesApi.freePort(daemonId, lo) }
          catch { message.error(t('docker.portNoFree')); return }
        } else {
          if (hostPort < lo || hostPort > hi) { message.error(t('docker.portOutOfRange', { min: lo, max: hi })); return }
          try {
            const list = await instancesApi.list(daemonId)
            for (const it of list) {
              for (const p of it.config.dockerPorts ?? []) {
                const m = p.split('/')[0].split(':')
                const candidate = Number(m.length === 3 ? m[1] : m[0])
                if (candidate === hostPort) { message.error(t('docker.portInUse', { port: hostPort })); return }
              }
            }
          } catch { /* best effort */ }
        }
      }
      const ports: string[] = []
      if (hostPort) {
        const containerPort = v.containerPort || hostPort
        ports.push(`${hostPort}:${containerPort}`)
      }
      try {
        const created = await instancesApi.create(daemonId, {
          name: v.name, type: v.type ?? 'generic', command: v.command,
          workingDir: v.workingDir ?? '', stopCmd: v.stopCmd || 'stop', autoStart: !!v.autoStart,
          args: v.argsText ? splitArgs(v.argsText) : undefined,
          dockerPorts: ports.length ? ports : undefined,
          dockerMemory: v.dockerMemory || undefined,
          dockerCpu: v.dockerCpu || undefined,
          dockerDiskSize: v.dockerDiskSize || undefined,
        })
        message.success(t('common.success'))
        setOpen(false); form.resetFields(); setResolveStatus(null); load()
        if (v.autoDeploy && created?.config?.uuid) {
          setPostDeploy({ daemonId, uuid: created.config.uuid })
        }
      } catch (e: any) {
        message.error(formatApiError(e, 'common.error'))
      }
    } catch { /* validation errors */ }
  }

  return (
    <>
      <PageHeader
        title={t('menu.instances')}
        subtitle={t('instance.pageSubtitle')}
        extra={
          <>
            <Input.Search
              allowClear
              placeholder={t('instance.searchPh')}
              value={search}
              onChange={(e) => setSearch(e.target.value)}
              style={{ width: 240 }}
            />
            <Button icon={<ReloadOutlined />} onClick={() => load()}>{t('common.refresh')}</Button>
            {isAdmin && (
              <Button icon={<RocketOutlined />} onClick={() => setDeployOpen(true)} disabled={!daemons.some(d => d.connected)}>
                {t('deploy.open')}
              </Button>
            )}
            {isAdmin && (
              <Button type="primary" icon={<PlusOutlined />} onClick={() => {
                form.resetFields()
                // Pre-fill a friendly random name so the user sees the
                // default and can either keep it or overwrite it before
                // submit. Without this they had to either type a name or
                // accept whatever the server makes up — confusing UX.
                form.setFieldValue('name', `inst-${Math.random().toString(16).slice(2, 10)}`)
                setOpen(true)
              }}
                disabled={!daemons.some(d => d.connected)}>
                {t('instance.new')}
              </Button>
            )}
          </>
        }
      />

      <Card bodyStyle={{ padding: 0 }}>
        <Table<AggregateRow>
          rowKey={(r) => `${r.daemonId}/${r.info.config.uuid}`}
          loading={loading}
          dataSource={sortedRows}
          pagination={{ pageSize: 10, showSizeChanger: false, hideOnSinglePage: true }}
          columns={[
            {
              title: t('instance.name'),
              render: (_, r) => {
                const p = playersByUuid[r.info.config.uuid]
                return (
                  <Space direction="vertical" size={0}>
                    <Space size={6}>
                      <Link to={`/instances/${r.daemonId}/${r.info.config.uuid}`} style={{ fontWeight: 500 }}>
                        {r.info.config.name}
                      </Link>
                      {p && p.max > 0 && <Tag color="green" bordered={false} style={{ marginInlineEnd: 0 }}>{p.count}/{p.max}</Tag>}
                    </Space>
                    <Tooltip title={r.info.config.uuid}>
                      <span className="taps-mono" style={{ color: 'var(--taps-text-muted)', fontSize: 11, cursor: 'pointer' }}
                        onClick={async () => { if (await copyToClipboard(r.info.config.uuid)) message.success(t('common.copied')) }}>
                        {r.info.config.uuid}
                      </span>
                    </Tooltip>
                  </Space>
                )
              },
            },
            {
              title: t('instance.node'), dataIndex: 'daemonId', width: 140,
              render: (id: number) => <Tag bordered={false}>{daemons.find(d => d.id === id)?.name ?? `#${id}`}</Tag>,
            },
            { title: t('instance.type'), width: 110, render: (_, r) => <span className="taps-mono">{r.info.config.type || 'generic'}</span> },
            {
              title: t('instance.status_'), width: 130,
              render: (_, r) => {
                const ds = displayStatusOf(r.info)
                return <StatusBadge variant={DISPLAY_VARIANT[ds]}>{t(`instance.status.${ds}`)}</StatusBadge>
              },
            },
            { title: 'PID', width: 90, render: (_, r) => r.info.pid || <span style={{ color: 'var(--taps-text-muted)' }}>—</span> },
            {
              title: t('docker.address'), width: 200,
              render: (_, r) => {
                const cfg = r.info.config
                if (cfg.type !== 'docker' || !cfg.dockerPorts || cfg.dockerPorts.length === 0) {
                  return <span style={{ color: 'var(--taps-text-muted)' }}>—</span>
                }
                const port = parseHostPort(cfg.dockerPorts[0])
                if (!port) return <span style={{ color: 'var(--taps-text-muted)' }}>—</span>
                const d = daemons.find(x => x.id === r.daemonId)
                const host = (d?.displayHost && d.displayHost.trim()) ||
                  (d?.address ? d.address.replace(/:\d+$/, '') : '')
                if (!host) return <span style={{ color: 'var(--taps-text-muted)' }}>—</span>
                const addr = `${host}:${port}`
                return (
                  <Tooltip title={t('common.clickCopy')}>
                    <Tag color="blue" bordered={false} className="taps-mono"
                      style={{ cursor: 'pointer' }}
                      onClick={async () => { if (await copyToClipboard(addr)) message.success(t('common.copied')) }}>
                      {addr}
                    </Tag>
                  </Tooltip>
                )
              },
            },
            {
              title: t('common.actions'), width: isAdmin ? 320 : 230, align: 'right',
              render: (_, r) => (
                <Space size={4}>
                  {needsSetup(r.info) ? (
                    <Button size="small" type="primary" icon={<SettingOutlined />}
                      onClick={() => setSetupTarget(r)}>
                      {t('setup.button')}
                    </Button>
                  ) : (
                    <>
                      <Button size="small" icon={<CaretRightOutlined />}
                        disabled={r.info.status === 'running' || r.info.status === 'starting'}
                        onClick={async () => { await instancesApi.start(r.daemonId, r.info.config.uuid); load() }}>
                        {t('instance.start')}
                      </Button>
                      <Button size="small" icon={<PoweroffOutlined />}
                        disabled={r.info.status !== 'running'}
                        onClick={async () => { await instancesApi.stop(r.daemonId, r.info.config.uuid); load() }}>
                        {t('instance.stop')}
                      </Button>
                    </>
                  )}
                  {isAdmin && (
                    <Button size="small" icon={<SafetyOutlined />} onClick={() => setPermTarget(r)}>
                      {t('instance.permission')}
                    </Button>
                  )}
                  {isAdmin && (
                    <Popconfirm title={t('common.confirmDelete')} onConfirm={async () => { await instancesApi.remove(r.daemonId, r.info.config.uuid); load() }}>
                      <Button size="small" danger icon={<DeleteOutlined />} />
                    </Popconfirm>
                  )}
                </Space>
              ),
            },
          ]}
        />
      </Card>

      <Modal title={t('instance.new')} open={open} onCancel={() => setOpen(false)} onOk={onCreate} destroyOnClose width={620}>
        <Form form={form} layout="vertical">
          <Form.Item name="target" label={t('instance.target')} rules={[{ required: true }]}>
            <Select placeholder={t('instance.targetPh')}
              options={[
                {
                  label: t('instance.targetNodes'),
                  options: daemons.filter(d => d.connected).map(d => ({
                    label: d.name + (d.requireDocker ? ' · docker-only' : ''),
                    value: `n-${d.id}`,
                  })),
                },
                ...(groups.length > 0 ? [{
                  label: t('instance.targetGroups'),
                  options: groups.map(g => ({
                    label: `${g.name} (${g.daemonIds?.length ?? 0})`,
                    value: `g-${g.id}`,
                  })),
                }] : []),
              ]} />
          </Form.Item>
          <Form.Item noStyle shouldUpdate={(p, n) => p.target !== n.target}>
            {({ getFieldValue, setFieldValue }) => {
              const tgt = getFieldValue('target') as string | undefined
              const isGroup = tgt?.startsWith('g-')
              const did = tgt?.startsWith('n-') ? Number(tgt.slice(2)) : undefined
              const d = daemons.find(x => x.id === did)
              // Groups are docker-only by design — the scheduler picks
              // among containerized members (managed volumes / docker
              // health), so force the type to docker just like a
              // docker-only node would.
              const dockerOnly = !!d?.requireDocker || !!isGroup
              if (dockerOnly && getFieldValue('type') !== 'docker') setFieldValue('type', 'docker')
              return (
                <>
                  {dockerOnly && !isGroup && (
                    <Alert
                      type="info" showIcon
                      style={{ marginBottom: 12 }}
                      message={t('instance.dockerOnly')}
                      description={d?.dockerReady === false ? t('instance.dockerNotReady') : undefined}
                    />
                  )}
                  {isGroup && (
                    <Alert
                      type="info" showIcon
                      style={{ marginBottom: 12 }}
                      message={t('instance.groupTargetNote')}
                    />
                  )}
                  <Form.Item name="name" label={t('instance.name')} extra={t('instance.nameAuto')}><Input placeholder={t('instance.nameAutoPh')} /></Form.Item>
                  <Form.Item name="type" label={t('instance.type')} initialValue={dockerOnly ? 'docker' : 'generic'}>
                    <Select disabled={dockerOnly}
                      options={(dockerOnly ? ['docker'] : ['generic', 'minecraft', 'bedrock', 'terraria', 'docker']).map(v => ({ label: v, value: v }))} />
                  </Form.Item>
                  <Form.Item noStyle shouldUpdate={(p, n) => p.type !== n.type}>
                    {() => getFieldValue('type') === 'docker' ? (
                      <>
                        <Form.Item name="command" label={t('instance.runtime')}
                          extra={imageOptions.length === 0
                            ? <span>{t('instance.imageEmpty')} <Link to="/images">{t('instance.imageGoPull')}</Link></span>
                            : t('instance.runtimeOptHelp')}
                        >
                          <Select
                            showSearch
                            placeholder={imageOptions.length === 0 ? t('instance.imageEmptyPh') : t('instance.imagePickPh')}
                            options={imageOptions}
                            notFoundContent={t('instance.imageEmpty')}
                          />
                        </Form.Item>
                        <Form.Item name="argsText" label={t('instance.dockerCmd')} extra={t('instance.dockerCmdHelp')}>
                          <Input placeholder='java -Xmx2G -jar server.jar nogui' className="taps-mono" />
                        </Form.Item>
                      </>
                    ) : (
                      <Form.Item name="command" label={t('instance.command')} rules={[{ required: true }]} extra={t('instance.commandHelp')}>
                        <Input.TextArea rows={2} placeholder='java -Xmx2G -jar server.jar nogui' />
                      </Form.Item>
                    )}
                  </Form.Item>
                  <Form.Item name="workingDir" label={t('instance.workingDir')}><Input placeholder={t('instance.workingDirPlaceholder')} /></Form.Item>
                  <Form.Item name="stopCmd" label={t('instance.stopCmd')} extra={t('instance.stopCmdHelp')}><Input placeholder="stop" /></Form.Item>
                  {getFieldValue('type') === 'docker' && (
                    <>
                      <div style={{ marginTop: 12, marginBottom: 8, fontSize: 12, color: 'var(--taps-text-muted)', textTransform: 'uppercase', letterSpacing: '0.04em', fontWeight: 500 }}>
                        {t('docker.limitsHeader')}
                      </div>
                      <Form.Item name="hostPort" label={t('docker.port')}
                        extra={isGroup
                          ? renderGroupResolveStatus(resolveStatus, resolving, t)
                          : t('docker.portEditHelp', { min: d?.portMin || 25565, max: d?.portMax || 25600 })}>
                        <InputNumber
                          min={isGroup ? 1 : (d?.portMin || 25565)}
                          max={isGroup ? 65535 : (d?.portMax || 25600)}
                          placeholder={t('docker.portAutoPh')} style={{ width: '100%' }} />
                      </Form.Item>
                      <Form.Item name="containerPort" label={t('docker.containerPort')} extra={t('docker.containerPortHelp')}>
                        <InputNumber min={1} max={65535} placeholder={t('docker.containerPortPh')} style={{ width: '100%' }} />
                      </Form.Item>
                      <Space.Compact style={{ width: '100%' }}>
                        <Form.Item name="dockerMemory" label={t('docker.memory')} rules={[{ required: true, message: t('docker.required') }]} style={{ flex: 1, marginRight: 8 }}>
                          <Input placeholder="2g" />
                        </Form.Item>
                        <Form.Item name="dockerCpu" label={t('docker.cpu')} rules={[{ required: true, message: t('docker.required') }]} style={{ flex: 1, marginRight: 8 }}>
                          <Input placeholder="1.5" />
                        </Form.Item>
                        <Form.Item name="dockerDiskSize" label={t('docker.disk')} rules={[{ required: true, message: t('docker.required') }]} style={{ flex: 1 }}>
                          <Input placeholder="10g" />
                        </Form.Item>
                      </Space.Compact>
                    </>
                  )}
                  <Form.Item name="autoStart" valuePropName="checked" label={t('instance.autoStart')}><Switch /></Form.Item>
                  {getFieldValue('type') === 'docker' && (
                    <Form.Item name="autoDeploy" valuePropName="checked" label={t('instance.autoDeploy')} extra={t('instance.autoDeployHelp')}>
                      <Switch />
                    </Form.Item>
                  )}
                </>
              )
            }}
          </Form.Item>
        </Form>
      </Modal>

      {permTarget && (
        <PermissionDrawer
          open={!!permTarget}
          onClose={() => setPermTarget(null)}
          daemonId={permTarget.daemonId}
          uuid={permTarget.info.config.uuid}
          instanceName={permTarget.info.config.name}
        />
      )}
      <QuickDeploy open={deployOpen} onClose={() => setDeployOpen(false)} onDeployed={load} />
      {setupTarget && (
        <SetupWizard
          open={!!setupTarget}
          onClose={() => setSetupTarget(null)}
          daemonId={setupTarget.daemonId}
          info={setupTarget.info}
          onDone={load}
        />
      )}
      {postDeploy && (
        <ServerDeployModal
          open={!!postDeploy}
          daemonId={postDeploy.daemonId}
          uuid={postDeploy.uuid}
          onClose={() => setPostDeploy(null)}
          onDone={() => { setPostDeploy(null); load() }}
        />
      )}
    </>
  )
}
