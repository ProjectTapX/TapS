import { useEffect, useMemo, useState } from 'react'
import { Card, Col, Row, Progress, Empty, Button, Space, Tag, Tooltip, Pagination, Skeleton, App } from 'antd'
import {
  ClusterOutlined, AppstoreOutlined, ThunderboltOutlined, HddOutlined,
  ReloadOutlined, ArrowRightOutlined,
} from '@ant-design/icons'
import { useTranslation } from 'react-i18next'
import { Link } from 'react-router-dom'
import { daemonsApi, instancesApi, type AggregateRow, type DaemonView, type InstanceStatus } from '@/api/resources'
import { monitorApi, type MonitorSnapshot } from '@/api/fs'
import { useAuthStore } from '@/stores/auth'
import MonitorChart from '@/components/MonitorChart'
import StatTile from '@/components/StatTile'
import StatusBadge from '@/components/StatusBadge'
import PageHeader from '@/components/PageHeader'
import { needsSetup } from '@/components/SetupWizard'
import { copyToClipboard } from '@/utils/clipboard'

function fmtBytes(n: number) {
  if (!n) return '0 B'
  const u = ['B', 'KB', 'MB', 'GB', 'TB']
  let i = 0
  while (n >= 1024 && i < u.length - 1) { n /= 1024; i++ }
  return `${n.toFixed(1)} ${u[i]}`
}

function fmtUptime(s: number) {
  const d = Math.floor(s / 86400)
  const h = Math.floor((s % 86400) / 3600)
  const m = Math.floor((s % 3600) / 60)
  return `${d}d ${h}h ${m}m`
}

interface NodeMon {
  daemon: DaemonView
  snap: MonitorSnapshot | null
  history: MonitorSnapshot[]
}

export default function DashboardPage() {
  const isAdmin = useAuthStore((s) => s.user?.role === 'admin')
  return isAdmin ? <AdminDashboard /> : <UserDashboard />
}

function AdminDashboard() {
  const { t } = useTranslation()
  const [items, setItems] = useState<NodeMon[]>([])
  const [instances, setInstances] = useState<AggregateRow[]>([])
  const [page, setPage] = useState(1)
  const PAGE_SIZE = 4

  const load = async () => {
    let ds: DaemonView[] = []
    try { ds = await daemonsApi.list() } catch { return }
    try { setInstances(await instancesApi.aggregate()) } catch { /* ignore */ }
    const rows = await Promise.all(ds.map(async d => {
      if (!d.connected) return { daemon: d, snap: null, history: [] }
      try {
        const [snap, history] = await Promise.all([
          monitorApi.snapshot(d.id),
          monitorApi.history(d.id),
        ])
        return { daemon: d, snap, history }
      }
      catch { return { daemon: d, snap: null, history: [] } }
    }))
    setItems(rows)
  }
  useEffect(() => { load(); const t = setInterval(load, 5000); return () => clearInterval(t) }, [])

  const totalNodes = items.length
  const onlineNodes = items.filter(i => i.daemon.connected).length
  const totalInstances = instances.length
  const runningInstances = instances.filter(i => i.info.status === 'running').length

  return (
    <>
      <PageHeader
        title={t('dashboard.heroTitle')}
        subtitle={t('dashboard.heroSubtitle')}
        extra={<Button icon={<ReloadOutlined />} onClick={load}>{t('common.refresh')}</Button>}
      />

      <Row gutter={[16, 16]} style={{ marginBottom: 8 }}>
        <Col xs={12} sm={12} md={6}>
          <StatTile label={t('dashboard.kpiNodes')} value={`${onlineNodes}/${totalNodes}`} hint={t('dashboard.kpiNodesHint')}
            icon={<ClusterOutlined />} accent="#007BFC" />
        </Col>
        <Col xs={12} sm={12} md={6}>
          <StatTile label={t('dashboard.kpiInstances')} value={totalInstances} hint={t('dashboard.kpiInstancesHint')}
            icon={<AppstoreOutlined />} accent="#10b981" />
        </Col>
        <Col xs={12} sm={12} md={6}>
          <StatTile label={t('dashboard.kpiRunning')} value={runningInstances} hint={t('dashboard.kpiRunningHint')}
            icon={<ThunderboltOutlined />} accent="#f59e0b" />
        </Col>
        <Col xs={12} sm={12} md={6}>
          <StatTile label={t('dashboard.kpiCapacity')}
            value={items.length > 0 ? `${Math.round(items.reduce((s, i) => s + (i.snap?.diskPercent ?? 0), 0) / items.length)}%` : '-'}
            hint={t('dashboard.kpiCapacityHint')}
            icon={<HddOutlined />} accent="#ef4444" />
        </Col>
      </Row>

      {items.length === 0 ? (
        <Card style={{ marginTop: 16, textAlign: 'center', padding: 48 }}>
          <Empty description={t('dashboard.noNodes')}>
            <Link to="/nodes"><Button type="primary" icon={<ArrowRightOutlined />} iconPosition="end">{t('node.add')}</Button></Link>
          </Empty>
        </Card>
      ) : (
        <>
        <Row gutter={[16, 16]} style={{ marginTop: 8 }}>
          {items.slice((page - 1) * PAGE_SIZE, page * PAGE_SIZE).map(({ daemon, snap, history }) => (
            <Col xs={24} sm={24} md={12} xl={12} key={daemon.id}>
              <Card
                title={
                  <Space>
                    <div style={{
                      width: 32, height: 32, borderRadius: 8,
                      background: 'linear-gradient(135deg, #007BFC, #00C2FF)',
                      color: '#fff', display: 'flex', alignItems: 'center', justifyContent: 'center',
                    }}>
                      <ClusterOutlined />
                    </div>
                    <div>
                      <div style={{ fontWeight: 600 }}>{daemon.name}</div>
                      <div style={{ fontSize: 12, color: 'var(--taps-text-muted)', fontWeight: 400 }}>
                        {daemon.displayHost || daemon.address}
                      </div>
                    </div>
                  </Space>
                }
                extra={daemon.connected
                  ? <StatusBadge variant="success">{t('node.connected')}</StatusBadge>
                  : <StatusBadge variant="danger">{t('node.offline')}</StatusBadge>}
              >
                {!snap ? (
                  <Empty description={t('dashboard.noData')} image={Empty.PRESENTED_IMAGE_SIMPLE} />
                ) : (
                  <>
                    <Row gutter={16}>
                      <Col span={8}>
                        <MetricBlock label={t('dashboard.cpu')} value={snap.cpuPercent} color="#007BFC" />
                      </Col>
                      <Col span={8}>
                        <MetricBlock label={t('dashboard.memory')} value={snap.memPercent} color="#10b981"
                          hint={`${fmtBytes(snap.memUsed)} / ${fmtBytes(snap.memTotal)}`} />
                      </Col>
                      <Col span={8}>
                        <MetricBlock label={t('dashboard.disk')} value={snap.diskPercent} color="#f59e0b"
                          hint={`${fmtBytes(snap.diskUsed)} / ${fmtBytes(snap.diskTotal)}`} />
                      </Col>
                    </Row>
                    <div style={{ marginTop: 16, padding: '12px 4px 0', borderTop: '1px solid var(--taps-border)' }}>
                      <MonitorChart data={history} height={120} />
                    </div>
                    <div style={{ marginTop: 8, fontSize: 12, color: 'var(--taps-text-muted)', display: 'flex', justifyContent: 'space-between' }}>
                      <span>{daemon.os}/{daemon.arch}</span>
                      <span>uptime {fmtUptime(snap.uptimeSec)}</span>
                    </div>
                  </>
                )}
              </Card>
            </Col>
          ))}
        </Row>
        {items.length > PAGE_SIZE && (
          <div style={{ textAlign: 'center', marginTop: 16 }}>
            <Pagination
              current={page} pageSize={PAGE_SIZE} total={items.length}
              showSizeChanger={false} onChange={setPage} size="small"
            />
          </div>
        )}
        </>
      )}
    </>
  )
}

function MetricBlock({ label, value, color, hint }: { label: string; value: number; color: string; hint?: string }) {
  return (
    <div>
      <div style={{ display: 'flex', alignItems: 'baseline', justifyContent: 'space-between' }}>
        <span style={{ fontSize: 12, color: 'var(--taps-text-muted)', fontWeight: 500 }}>{label}</span>
        <span style={{ fontSize: 16, fontWeight: 600, color }}>{value.toFixed(1)}<span style={{ fontSize: 11, color: 'var(--taps-text-muted)', marginLeft: 2 }}>%</span></span>
      </div>
      <Progress percent={value} showInfo={false} strokeColor={color} size="small" style={{ marginTop: 4, marginBottom: 0 }} />
      {hint && <div style={{ fontSize: 11, color: 'var(--taps-text-muted)', marginTop: 4 }}>{hint}</div>}
    </div>
  )
}

// --- UserDashboard ---------------------------------------------------------
//
// User-role landing page. Same shape as the admin node dashboard, but the
// cards are per-instance (CPU / Memory / Disk live from docker stats) and
// the KPI tiles count the user's accessible instances and how many are
// running. Disk only renders when the instance has a managed loopback
// volume — otherwise we just skip that metric.
type InstanceCard = {
  row: AggregateRow
  daemon: { displayHost: string; address?: string } | null
  stats: {
    running: boolean
    cpuPercent: number
    memBytes: number; memLimit: number
    diskUsedBytes?: number; diskTotalBytes?: number
  } | null
  players: { count: number; max: number } | null
}

const STATUS_VARIANT: Record<InstanceStatus, 'success' | 'warning' | 'danger' | 'neutral' | 'processing'> = {
  stopped: 'neutral', starting: 'processing', running: 'success', stopping: 'processing', crashed: 'danger', hibernating: 'warning',
}

// "configuring" is a UI-only pseudo-status: stopped + needsSetup is shown
// as 待配置 to the user, since "stopped" reads as "I started this and it
// crashed" instead of "I never finished setting it up".
type DisplayStatus = InstanceStatus | 'configuring'
function displayStatus(info: AggregateRow['info']): DisplayStatus {
  if (info.status === 'stopped' && needsSetup(info)) return 'configuring'
  return info.status
}
const DISPLAY_VARIANT: Record<DisplayStatus, 'success' | 'warning' | 'danger' | 'neutral' | 'processing'> = {
  ...STATUS_VARIANT,
  configuring: 'warning',
}

function displayHostOf(d: { displayHost: string; address?: string } | null): string {
  if (!d) return ''
  if (d.displayHost && d.displayHost.trim()) return d.displayHost.trim()
  if (d.address) {
    const i = d.address.lastIndexOf(':')
    return i > 0 ? d.address.slice(0, i) : d.address
  }
  return ''
}

function parseHostPort(spec: string): number | null {
  const t = spec.trim()
  if (!t) return null
  const body = t.includes('/') ? t.slice(0, t.indexOf('/')) : t
  const parts = body.split(':')
  const hostStr = parts.length === 3 ? parts[1] : parts[0]
  const n = Number(hostStr)
  return Number.isFinite(n) && n > 0 ? n : null
}

function UserDashboard() {
  const { t } = useTranslation()
  const { message } = App.useApp()
  const [cards, setCards] = useState<InstanceCard[]>([])
  const [loading, setLoading] = useState(true)
  const [page, setPage] = useState(1)
  const PAGE_SIZE = 4

  // Two-phase load:
  //   1. Pull aggregate + per-daemon publicView (fast, just DB reads on
  //      the panel side) and render cards immediately so the user sees
  //      *something* on first paint instead of an "empty" flash.
  //   2. Fire one batched dockerStatsAll per daemon and stitch the live
  //      mem/cpu/disk numbers onto the cards as they come in.
  // Silent polls skip the phase-1 setCards (which would briefly null out
  // existing stats and flicker the bars) and instead merge the fresh
  // stats into whatever's already on screen.
  const load = async (silent = false) => {
    if (!silent) setLoading(true)
    let rows: AggregateRow[] = []
    try { rows = await instancesApi.aggregate() } catch { /* ignore */ }
    const daemonIds = Array.from(new Set(rows.map(r => r.daemonId)))
    const daemonMap = new Map<number, { displayHost: string; address?: string } | null>()
    await Promise.all(daemonIds.map(async id => {
      try {
        const d = await daemonsApi.publicView(id)
        daemonMap.set(id, { displayHost: d.displayHost, address: d.address })
      } catch { daemonMap.set(id, null) }
    }))
    if (!silent) {
      setCards(rows.map(row => ({ row, daemon: daemonMap.get(row.daemonId) ?? null, stats: null, players: null })))
      setLoading(false)
    }
    const daemonsWithRunning = Array.from(new Set(
      rows.filter(r => r.info.config.type === 'docker' && r.info.status === 'running')
        .map(r => r.daemonId)
    ))
    const statsByName = new Map<string, InstanceCard['stats']>()
    const playersByUuid = new Map<string, { count: number; max: number }>()
    await Promise.all([
      ...daemonsWithRunning.map(async id => {
        try {
          const items = await instancesApi.dockerStatsAll(id)
          for (const it of items) statsByName.set(it.name, it as any)
        } catch { /* ignore */ }
      }),
      ...daemonsWithRunning.map(async id => {
        try {
          const items = await instancesApi.playersAll(id)
          for (const it of items) playersByUuid.set(it.uuid, { count: it.count, max: it.max })
        } catch { /* ignore */ }
      }),
    ])
    setCards(prev => rows.map(row => {
      const fresh = statsByName.get(`taps-${row.info.config.uuid}`)
      const previousCard = prev.find(c => c.row.info.config.uuid === row.info.config.uuid)
      return {
        row,
        daemon: daemonMap.get(row.daemonId) ?? null,
        stats: fresh ?? previousCard?.stats ?? null,
        players: playersByUuid.get(row.info.config.uuid) ?? previousCard?.players ?? null,
      }
    }))
  }
  useEffect(() => {
    load()
    const t = setInterval(() => load(true), 5000)
    return () => clearInterval(t)
  }, [])

  // Sort: crashed → configuring (needsSetup but not yet set up) → running →
  // others (stopped/stopping/starting). Within the same group, newer first.
  const sortedCards = useMemo(() => {
    const PRIORITY: Record<string, number> = { crashed: 0, configuring: 1, running: 2, hibernating: 3, starting: 4, stopping: 5, stopped: 6 }
    return [...cards].sort((a, b) => {
      const sa = displayStatus(a.row.info)
      const sb = displayStatus(b.row.info)
      const pa = PRIORITY[sa] ?? 99
      const pb = PRIORITY[sb] ?? 99
      if (pa !== pb) return pa - pb
      const ca = a.row.info.config.createdAt ?? 0
      const cb = b.row.info.config.createdAt ?? 0
      return cb - ca
    })
  }, [cards])

  const total = sortedCards.length
  const running = sortedCards.filter(c => c.row.info.status === 'running').length

  return (
    <>
      <PageHeader
        title={t('dashboard.heroTitle')}
        subtitle={t('dashboard.heroSubtitleUser')}
        extra={<Button icon={<ReloadOutlined />} onClick={() => load()}>{t('common.refresh')}</Button>}
      />

      <Row gutter={[16, 16]} style={{ marginBottom: 8 }}>
        <Col xs={12} md={8}>
          <StatTile label={t('dashboard.kpiInstances')} value={total} hint={t('dashboard.kpiInstancesHint')}
            icon={<AppstoreOutlined />} accent="#10b981" />
        </Col>
        <Col xs={12} md={8}>
          <StatTile label={t('dashboard.kpiRunning')} value={running} hint={t('dashboard.kpiRunningHint')}
            icon={<ThunderboltOutlined />} accent="#f59e0b" />
        </Col>
      </Row>

      {loading ? (
        <Row gutter={[16, 16]} style={{ marginTop: 8 }}>
          {[0, 1, 2, 3].map(i => (
            <Col xs={24} sm={24} md={12} xl={12} key={i}>
              <Card>
                <Skeleton avatar active paragraph={{ rows: 3 }} />
              </Card>
            </Col>
          ))}
        </Row>
      ) : sortedCards.length === 0 ? (
        <Card style={{ marginTop: 16, textAlign: 'center', padding: 48 }}>
          <Empty description={t('dashboard.noInstances')}>
            <Link to="/instances"><Button type="primary" icon={<ArrowRightOutlined />} iconPosition="end">{t('menu.instances')}</Button></Link>
          </Empty>
        </Card>
      ) : (
        <>
        <Row gutter={[16, 16]} style={{ marginTop: 8 }}>
          {sortedCards.slice((page - 1) * PAGE_SIZE, page * PAGE_SIZE).map(({ row, daemon, stats, players }) => {
            const cfg = row.info.config
            const status = row.info.status
            const dStatus = displayStatus(row.info)
            const cpuLimit = parseFloat(cfg.dockerCpu || '') || 1
            const cpuTotalPct = cpuLimit * 100
            const port = (cfg.dockerPorts ?? []).map(parseHostPort).find(Boolean) ?? null
            const host = displayHostOf(daemon)
            const address = port && host ? `${host}:${port}` : null
            return (
              <Col xs={24} sm={24} md={12} xl={12} key={`${row.daemonId}/${cfg.uuid}`}>
                <Card
                  title={
                    <Space>
                      <div style={{
                        width: 32, height: 32, borderRadius: 8,
                        background: 'linear-gradient(135deg, #10b981, #34d399)',
                        color: '#fff', display: 'flex', alignItems: 'center', justifyContent: 'center',
                      }}>
                        <AppstoreOutlined />
                      </div>
                      <div>
                        <div style={{ fontWeight: 600 }}>
                          <Link to={`/instances/${row.daemonId}/${cfg.uuid}`}>{cfg.name}</Link>
                        </div>
                        <div style={{ fontSize: 12, color: 'var(--taps-text-muted)', fontWeight: 400 }}>
                          {cfg.type} · {cfg.uuid.slice(0, 8)}
                        </div>
                      </div>
                    </Space>
                  }
                  extra={
                    <Space>
                      {players && (
                        <Tag color="green" bordered={false} style={{ fontWeight: 500 }}>
                          {t('instance.players')} {players.count}/{players.max}
                        </Tag>
                      )}
                      <StatusBadge variant={DISPLAY_VARIANT[dStatus]}>{t(`instance.status.${dStatus}`)}</StatusBadge>
                    </Space>
                  }
                >
                  {status !== 'running' || cfg.type !== 'docker' || !stats ? (
                    <Empty
                      description={status !== 'running' ? t('instance.notRunning') : t('dashboard.noData')}
                      image={Empty.PRESENTED_IMAGE_SIMPLE}
                    />
                  ) : (
                    <Row gutter={16}>
                      <Col span={stats.diskTotalBytes ? 8 : 12}>
                        <MetricBlock label={t('instance.cpuUsage')} value={Math.min(100, (stats.cpuPercent / cpuTotalPct) * 100)} color="#007BFC"
                          hint={`${stats.cpuPercent.toFixed(1)}% / ${cpuTotalPct}%`} />
                      </Col>
                      <Col span={stats.diskTotalBytes ? 8 : 12}>
                        <MetricBlock label={t('instance.memUsage')}
                          value={stats.memLimit > 0 ? (stats.memBytes / stats.memLimit) * 100 : 0}
                          color="#10b981"
                          hint={`${fmtBytes(stats.memBytes)} / ${fmtBytes(stats.memLimit)}`} />
                      </Col>
                      {stats.diskTotalBytes ? (
                        <Col span={8}>
                          <MetricBlock label={t('instance.diskUsage')}
                            value={(stats.diskUsedBytes ?? 0) / stats.diskTotalBytes * 100}
                            color="#f59e0b"
                            hint={`${fmtBytes(stats.diskUsedBytes ?? 0)} / ${fmtBytes(stats.diskTotalBytes)}`} />
                        </Col>
                      ) : null}
                    </Row>
                  )}
                  {address && (
                    <div style={{ marginTop: 16, paddingTop: 12, borderTop: '1px solid var(--taps-border)' }}>
                      <Space>
                        <span style={{ fontSize: 12, color: 'var(--taps-text-muted)' }}>{t('docker.address')}</span>
                        <Tooltip title={t('common.clickCopy')}>
                          <Tag color="blue" bordered={false} className="taps-mono"
                            style={{ cursor: 'pointer' }}
                            onClick={async () => {
                              if (await copyToClipboard(address)) message.success(t('common.copied'))
                              else message.error(t('common.error'))
                            }}>
                            {address}
                          </Tag>
                        </Tooltip>
                      </Space>
                    </div>
                  )}
                </Card>
              </Col>
            )
          })}
        </Row>
        {sortedCards.length > PAGE_SIZE && (
          <div style={{ textAlign: 'center', marginTop: 16 }}>
            <Pagination
              current={page} pageSize={PAGE_SIZE} total={sortedCards.length}
              showSizeChanger={false} onChange={setPage} size="small"
            />
          </div>
        )}
        </>
      )}
    </>
  )
}
