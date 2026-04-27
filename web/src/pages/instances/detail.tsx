import { useEffect, useMemo, useState } from 'react'
import { useParams, Link } from 'react-router-dom'
import { Tabs, Card, Descriptions, Tag, Button, Space, App, Modal, Form, Input, Switch, Select, InputNumber, Tooltip, Progress, Radio, Skeleton } from 'antd'
import {
  ArrowLeftOutlined, CaretRightOutlined, PoweroffOutlined, CloseOutlined, EditOutlined, SettingOutlined, DownloadOutlined,
} from '@ant-design/icons'
import { useTranslation } from 'react-i18next'
import { instancesApi, type InstanceConfig, type InstanceInfo } from '@/api/resources'
import { dockerApi, type DockerImage } from '@/api/docker'
import { daemonsApi, type DaemonView } from '@/api/resources'
import { volumesApi, type Volume } from '@/api/volumes'
import SetupWizard, { needsSetup } from '@/components/SetupWizard'
import { copyToClipboard } from '@/utils/clipboard'
import { builtinCommands, parseUserCompletions } from '@/data/commands'
import { useAuthStore } from '@/stores/auth'
import InstanceTerminal from '@/components/Terminal'
import FileExplorer from '@/components/FileExplorer'
import TaskList from '@/components/TaskList'
import PlayerList from '@/components/PlayerList'
import ServerPropertiesEditor from '@/components/ServerPropertiesEditor'
import BackupList from '@/components/BackupList'
import PageHeader from '@/components/PageHeader'
import StatusBadge from '@/components/StatusBadge'
import ServerDeployModal from '@/components/ServerDeployModal'
import { formatApiError } from '@/api/errors'

const STATUS_VARIANT: Record<string, 'success' | 'warning' | 'danger' | 'neutral' | 'processing'> = {
  stopped: 'neutral', starting: 'processing', running: 'success', stopping: 'processing', crashed: 'danger',
  configuring: 'warning', hibernating: 'warning',
}
function dispStatus(info: InstanceInfo): string {
  if (info.status === 'stopped' && needsSetup(info)) return 'configuring'
  return info.status
}

// helpers to convert string[] ↔ textarea value
const arrToText = (a?: string[]) => (a ?? []).join('\n')
const textToArr = (s?: string) => (s ?? '').split(/\r?\n/).map(x => x.trim()).filter(Boolean)

// Extract host port + protocol from a docker -p spec ("25565:25565[/udp]" /
// "0.0.0.0:25565:25565" / "25565"). Returns null if it can't be parsed.
function parsePortSpec(spec: string): { host: number; proto: string } | null {
  const t = spec.trim()
  if (!t) return null
  let proto = ''
  let body = t
  const slash = body.indexOf('/')
  if (slash >= 0) { proto = body.slice(slash + 1); body = body.slice(0, slash) }
  const parts = body.split(':')
  let hostStr = ''
  if (parts.length === 1) hostStr = parts[0]
  else if (parts.length === 2) hostStr = parts[0]
  else if (parts.length === 3) hostStr = parts[1]
  else return null
  const n = Number(hostStr)
  if (!Number.isFinite(n) || n <= 0 || n > 65535) return null
  return { host: n, proto }
}

// Pick the best display hostname for a daemon: explicit displayHost first,
// else strip the panel-side port off daemon.address.
function displayHostOf(d: DaemonView | null): string {
  if (d?.displayHost && d.displayHost.trim()) return d.displayHost.trim()
  if (d?.address) {
    const a = d.address.trim()
    const i = a.lastIndexOf(':')
    return i > 0 ? a.slice(0, i) : a
  }
  return ''
}

// shell-ish single-line argv split (respects "double quotes")
function splitArgs(line: string): string[] {
  const out: string[] = []
  let cur = '', inQ = false
  for (const c of line) {
    if (c === '"') { inQ = !inQ; continue }
    if (!inQ && (c === ' ' || c === '\t')) { if (cur) { out.push(cur); cur = '' }; continue }
    cur += c
  }
  if (cur) out.push(cur)
  return out
}

export default function InstanceDetailPage() {
  const { t } = useTranslation()
  const { daemonId = '0', uuid = '' } = useParams()
  const did = Number(daemonId)
  const { message, modal } = App.useApp()
  const isAdmin = useAuthStore((s) => s.user?.role === 'admin')
  const [info, setInfo] = useState<InstanceInfo | null>(null)
  const [daemon, setDaemon] = useState<DaemonView | null>(null)
  const [editOpen, setEditOpen] = useState(false)
  const [editType, setEditType] = useState('docker')
  const [srvDeployOpen, setSrvDeployOpen] = useState(false)
  const [setupOpen, setSetupOpen] = useState(false)
  // Live docker stats and managed-volume usage. Both refresh on the same
  // 2 s reload tick as the instance status; we render a Resource card
  // only when the instance is actually a docker one with limits.
  const [stats, setStats] = useState<{ memBytes: number; memLimit: number; cpuPercent: number; running: boolean; diskUsedBytes?: number; diskTotalBytes?: number } | null>(null)
  const [vol, setVol] = useState<Volume | null>(null)
  const [players, setPlayers] = useState<{ count: number; max: number } | null>(null)
  const [form] = Form.useForm<InstanceConfig & { argsText?: string; dockerEnvText?: string; dockerVolumesText?: string; hostPort?: number; containerPort?: number; completionWordsText?: string; hibernationMode?: 'default' | 'on' | 'off' }>()
  const [images, setImages] = useState<DockerImage[]>([])
  const [usedPorts, setUsedPorts] = useState<Set<number>>(new Set())

  useEffect(() => {
    if (!editOpen) return
    dockerApi(did).images().then(r => setImages(r.images ?? [])).catch(() => { /* ignore */ })
    // Snapshot used ports on this daemon (excluding self) for uniqueness checks.
    instancesApi.list(did).then(list => {
      const u = new Set<number>()
      for (const it of list) {
        if (it.config.uuid === uuid) continue
        for (const p of it.config.dockerPorts ?? []) {
          const m = parsePortSpec(p)
          if (m) u.add(m.host)
        }
      }
      setUsedPorts(u)
    }).catch(() => { /* ignore */ })
  }, [editOpen, did, uuid])

  const imageOptions = useMemo(() => images
    .filter(im => im.repository && im.repository !== '<none>' && im.tag && im.tag !== '<none>')
    .map(im => {
      const ref = `${im.repository}:${im.tag}`
      return { label: im.displayName || ref, value: ref }
    }),
  [images])

  const reload = async () => {
    try {
      const list = await instancesApi.list(did)
      const found = list.find(i => i.config.uuid === uuid) ?? null
      setInfo(found)
      // Fetch dockerStats whenever the instance is docker-type, even
      // when stopped/hibernating: the daemon stitches managed-volume
      // usage onto the response, which is the only way for non-admins
      // to see disk for a non-running instance (volumes API is admin-
      // only). cpu/mem just come back zeroed for stopped containers.
      if (found?.config.type === 'docker') {
        try { setStats(await instancesApi.dockerStats(did, uuid)) } catch { setStats(null) }
        if (found.status === 'running') {
          // Player count via the daemon's batch SLP-ping endpoint — pick
          // out our row by uuid. Failed pings (server starting up, not MC,
          // wrong port) just leave players null and the badge hides.
          try {
            const items = await instancesApi.playersAll(did)
            const me = items.find(it => it.uuid === uuid)
            setPlayers(me ? { count: me.count, max: me.max } : null)
          } catch { /* ignore */ }
        } else {
          setPlayers(null)
        }
      } else {
        setStats(null)
        setPlayers(null)
      }
    } catch { /* ignore */ }
  }
  useEffect(() => { reload(); const t = setInterval(reload, 2000); return () => clearInterval(t) }, [did, uuid])

  // Volume usage is independent of run state — even a stopped instance
  // has files taking up space. Refetch every 5 s; cheap.
  useEffect(() => {
    if (!isAdmin) return
    if (!info?.config.managedVolume) { setVol(null); return }
    const tick = async () => {
      try {
        const r = await volumesApi(did).list()
        setVol(r.volumes.find(v => v.name === info.config.managedVolume) ?? null)
      } catch { /* ignore */ }
    }
    tick()
    const t = setInterval(tick, 5000)
    return () => clearInterval(t)
  }, [did, isAdmin, info?.config.managedVolume])

  // Fetch this daemon's display info once so we can render game addresses.
  // Admins use the full daemon record (already loaded for the dashboard);
  // non-admins fall back to the public-view endpoint that exposes only
  // name + address + displayHost.
  useEffect(() => {
    if (isAdmin) {
      daemonsApi.list().then(ds => setDaemon(ds.find(d => d.id === did) ?? null)).catch(() => { /* ignore */ })
    } else {
      daemonsApi.publicView(did).then(d => setDaemon({
        id: d.id, name: d.name, address: d.address ?? '', displayHost: d.displayHost,
        lastSeen: '', createdAt: '', connected: false,
      } as DaemonView)).catch(() => { /* ignore */ })
    }
  }, [did, isAdmin])

  const action = async (fn: () => Promise<unknown>, msg: string) => {
    try { await fn(); message.success(msg); reload() }
    catch (e: any) { message.error(formatApiError(e, 'common.error')) }
  }

  const onEdit = () => {
    if (!info) return
    // Pull the first parseable host port out of dockerPorts so the field
    // round-trips when the user just edits other fields.
    const first = (info.config.dockerPorts ?? []).map(parsePortSpec).find(Boolean)
    // Container side: parse from the same spec (defaults to host if the
    // entry was a single number).
    let containerPort: number | undefined
    if (info.config.dockerPorts && info.config.dockerPorts.length > 0) {
      const spec = info.config.dockerPorts[0]
      const body = spec.split('/')[0]
      const parts = body.split(':')
      const c = parts.length === 3 ? parts[2] : (parts.length === 2 ? parts[1] : parts[0])
      const n = Number(c)
      if (Number.isFinite(n) && n > 0) containerPort = n
    }
    setEditType(info.config.type || 'docker')
    form.setFieldsValue({
      ...info.config,
      argsText: (info.config.args ?? []).join(' '),
      dockerEnvText: arrToText(info.config.dockerEnv),
      dockerVolumesText: arrToText(info.config.dockerVolumes),
      hostPort: first ? first.host : undefined,
      containerPort: containerPort,
      completionWordsText: (info.config.completionWords ?? []).join('\n'),
      hibernationMode:
        info.config.hibernationEnabled === true ? 'on'
        : info.config.hibernationEnabled === false ? 'off'
        : 'default',
      hibernationIdleMinutes: info.config.hibernationIdleMinutes,
    })
    setEditOpen(true)
  }

  const portMin = daemon?.portMin || 25565
  const portMax = daemon?.portMax || 25600

  const onSave = async () => {
    const v = await form.validateFields()
    // Non-admins don't see hostPort / freePort allocation. Send a
    // synthetic spec — the backend's user-update path preserves the
    // previous host port and only adopts the container half from us.
    if (!isAdmin) {
      const cont = v.containerPort || 0
      try {
        await instancesApi.update(did, uuid, {
          name: v.name,
          command: v.command,
          args: v.argsText ? splitArgs(v.argsText) : undefined,
          stopCmd: v.stopCmd || 'stop',
          outputEncoding: v.outputEncoding || 'utf-8',
          dockerPorts: cont ? [`0:${cont}`] : undefined,
          completionWords: parseUserCompletions(v.completionWordsText),
        })
        message.success(t('common.success'))
        setEditOpen(false)
        reload()
      } catch (e: any) {
        message.error(formatApiError(e, 'common.error'))
      }
      return
    }
    let hostPort = v.hostPort
    if (v.type === 'docker') {
      if (!hostPort) {
        try {
          hostPort = await instancesApi.freePort(did, portMin)
        } catch {
          message.error(t('docker.portNoFree'))
          return
        }
      } else {
        if (hostPort < portMin || hostPort > portMax) {
          message.error(t('docker.portOutOfRange', { min: portMin, max: portMax }))
          return
        }
        if (usedPorts.has(hostPort)) {
          message.error(t('docker.portInUse', { port: hostPort }))
          return
        }
      }
    }
    const ports = (v.type === 'docker' && hostPort) ? [`${hostPort}:${v.containerPort || hostPort}`] : []
    const cfg: Partial<InstanceConfig> = {
      name: v.name, type: v.type, command: v.command,
      workingDir: v.workingDir ?? '', stopCmd: v.stopCmd || 'stop',
      autoStart: !!v.autoStart, autoRestart: !!v.autoRestart, restartDelay: v.restartDelay ?? 5,
      outputEncoding: v.outputEncoding || 'utf-8',
      minecraftHost: v.minecraftHost || '127.0.0.1',
      minecraftPort: v.minecraftPort || 25565,
      args: v.argsText ? splitArgs(v.argsText) : undefined,
      dockerEnv: textToArr(v.dockerEnvText),
      dockerVolumes: textToArr(v.dockerVolumesText),
      dockerPorts: ports,
      dockerCpu: v.dockerCpu, dockerMemory: v.dockerMemory,
      dockerDiskSize: v.dockerDiskSize,
      completionWords: parseUserCompletions(v.completionWordsText),
      hibernationEnabled:
        v.hibernationMode === 'on' ? true
        : v.hibernationMode === 'off' ? false
        : null,
      hibernationIdleMinutes: v.hibernationIdleMinutes ?? 0,
    }
    try {
      await instancesApi.update(did, uuid, cfg)
      message.success(t('common.success'))
      setEditOpen(false)
      reload()
    } catch (e: any) {
      message.error(formatApiError(e, 'common.error'))
    }
  }

  const isMc = info?.config.type === 'minecraft'

  // Per-instance file root: docker instances get their auto /data dir as
  // the explorer's root, so users land inside the bind they can reference
  // with `java -jar server.jar` (relative to /data inside the container).
  const instShort = uuid.replace(/-/g, '').slice(0, 12)
  const filesRoot =
    info?.config.type === 'docker' && (info.config.managedVolume || info.config.autoDataDir)
      ? `/data/inst-${instShort}`
      : '/'

  const tabs = [
    { key: 'terminal', label: t('instance.terminal'),
      children: <InstanceTerminal
        daemonId={did} uuid={uuid}
        localEcho={info?.config.type === 'docker'}
        completionCandidates={() => [
          ...(info?.config.completionWords ?? []),
          ...builtinCommands(info?.config.type),
        ]}
      /> },
    { key: 'files', label: t('instance.files'), children: <FileExplorer daemonId={did} rootPath={filesRoot} /> },
    ...(isMc ? [
      { key: 'players', label: t('mc.players'), children: <PlayerList daemonId={did} uuid={uuid} /> },
      { key: 'props', label: 'server.properties', children: <ServerPropertiesEditor daemonId={did} workingDir={info!.config.workingDir} /> },
    ] : []),
    { key: 'tasks', label: t('instance.tasks'), children: <TaskList daemonId={did} uuid={uuid} /> },
    { key: 'backups', label: t('backup.tab'), children: <BackupList daemonId={did} uuid={uuid} /> },
  ]

  return (
    <>
      <PageHeader
        title={info?.config.name ?? uuid}
        subtitle={
          info ? (
            <Space size={12}>
              <StatusBadge variant={STATUS_VARIANT[dispStatus(info)]}>{t(`instance.status.${dispStatus(info)}`)}</StatusBadge>
              {players && (
                <Tag color="green" bordered={false} style={{ marginInlineEnd: 0 }}>
                  {t('instance.players')} {players.count}/{players.max}
                </Tag>
              )}
              <span className="taps-mono">{info.config.type}</span>
              {info.pid > 0 && <span style={{ color: 'var(--taps-text-muted)' }}>PID {info.pid}</span>}
            </Space>
          ) : undefined
        }
        crumbs={[
          { title: <ArrowLeftOutlined />, to: '/instances' },
          { title: t('menu.instances'), to: '/instances' },
          { title: info?.config.name ?? uuid },
        ]}
        extra={
          <>
            {info && needsSetup(info) ? (
              <Button type="primary" icon={<SettingOutlined />} onClick={() => setSetupOpen(true)}>{t('setup.button')}</Button>
            ) : (
              <>
                <Button type="primary" icon={<CaretRightOutlined />} disabled={info?.status === 'running'} onClick={() => action(() => instancesApi.start(did, uuid), t('instance.started'))}>{t('instance.start')}</Button>
                <Button icon={<PoweroffOutlined />} disabled={info?.status !== 'running'} onClick={() => action(() => instancesApi.stop(did, uuid), t('instance.stopSent'))}>{t('instance.stop')}</Button>
              </>
            )}
            <Button icon={<CloseOutlined />} danger disabled={info?.status === 'stopped'}
              onClick={() => modal.confirm({
                title: t('instance.killConfirmTitle'),
                content: t('instance.killConfirmBody'),
                okText: t('instance.kill'), okButtonProps: { danger: true },
                cancelText: t('common.cancel'),
                onOk: () => action(() => instancesApi.kill(did, uuid), t('instance.killed')),
              })}>{t('instance.kill')}</Button>
            <Tooltip title={info && needsSetup(info) ? t('setup.editDisabled') : ''}>
              <Button icon={<EditOutlined />} onClick={onEdit} disabled={!info || needsSetup(info) || info.status === 'hibernating'}>{t('common.edit')}</Button>
            </Tooltip>
            {info?.config.type === 'docker' && (info.status === 'stopped' || info.status === 'crashed') && !needsSetup(info) && (
              <Button icon={<DownloadOutlined />} onClick={() => setSrvDeployOpen(true)}>{t('serverDeploy.button')}</Button>
            )}
          </>
        }
      />

      {info && isAdmin && (
        <Card style={{ marginBottom: 16 }} bodyStyle={{ padding: 18 }}>
          <Descriptions column={3} size="small">
            <Descriptions.Item label={t('instance.workingDir')}>
              <span className="taps-mono">{info.config.workingDir || '(default)'}</span>
            </Descriptions.Item>
            <Descriptions.Item label={t('instance.stopCmd')}>
              <span className="taps-mono">{info.config.stopCmd || '(SIGTERM)'}</span>
            </Descriptions.Item>
            <Descriptions.Item label={t('encoding.label')}>
              <Tag bordered={false}>{info.config.outputEncoding || 'utf-8'}</Tag>
            </Descriptions.Item>
            <Descriptions.Item label={t('instance.command')} span={3}>
              {/* For docker instances the `command` field holds the image
                  reference and the user-typed CMD lives in args; showing the
                  image inline is just noise (the image is implied by the
                  type/badge above), so we render only the args. */}
              <code className="taps-mono">{
                info.config.type === 'docker'
                  ? (info.config.args?.join(' ') || '—')
                  : info.config.command + (info.config.args?.length ? ' ' + info.config.args.join(' ') : '')
              }</code>
            </Descriptions.Item>
            <Descriptions.Item label={t('instance.autoStart')}>{info.config.autoStart ? '✓' : '—'}</Descriptions.Item>
            <Descriptions.Item label={t('restart.auto')}>{info.config.autoRestart ? `${info.config.restartDelay ?? 5}s` : '—'}</Descriptions.Item>
            {isMc && (
              <Descriptions.Item label="Minecraft">
                <span className="taps-mono">{info.config.minecraftHost || '127.0.0.1'}:{info.config.minecraftPort || 25565}</span>
              </Descriptions.Item>
            )}
            {info.config.type === 'docker' && (
              <>
                <Descriptions.Item label={t('docker.memory')}>
                  {info.config.dockerMemory ? <Tag bordered={false} color="blue">{info.config.dockerMemory}</Tag> : <span style={{ color: 'var(--taps-text-muted)' }}>—</span>}
                </Descriptions.Item>
                <Descriptions.Item label={t('docker.cpu')}>
                  {info.config.dockerCpu ? <Tag bordered={false} color="blue">{info.config.dockerCpu}</Tag> : <span style={{ color: 'var(--taps-text-muted)' }}>—</span>}
                </Descriptions.Item>
                <Descriptions.Item label={t('docker.disk')}>
                  {info.config.dockerDiskSize ? <Tag bordered={false} color="blue">{info.config.dockerDiskSize}</Tag> : <span style={{ color: 'var(--taps-text-muted)' }}>—</span>}
                </Descriptions.Item>
                <Descriptions.Item label={t('docker.address')} span={3}>
                  {info.config.dockerPorts && info.config.dockerPorts.length > 0
                    ? <Space wrap size={4}>{info.config.dockerPorts.map((p, i) => {
                        const parsed = parsePortSpec(p)
                        const host = displayHostOf(daemon) || '<host>'
                        const label = parsed
                          ? `${host}:${parsed.host}${parsed.proto ? '/' + parsed.proto : ''}`
                          : p
                        return (
                          <Tooltip key={i} title={t('common.clickCopy')}>
                            <Tag
                              color="blue" bordered={false} className="taps-mono"
                              style={{ cursor: 'pointer' }}
                              onClick={async () => {
                                if (await copyToClipboard(label)) message.success(t('common.copied'))
                                else message.error(t('common.error'))
                              }}
                            >{label}</Tag>
                          </Tooltip>
                        )
                      })}</Space>
                    : <span style={{ color: 'var(--taps-text-muted)' }}>—</span>}
                </Descriptions.Item>
                {info.config.dockerVolumes && info.config.dockerVolumes.length > 0 && (
                  <Descriptions.Item label={t('docker.volumes')} span={3}>
                    <Space wrap size={4}>{info.config.dockerVolumes.map((v, i) => <Tag key={i} bordered={false} className="taps-mono">{v}</Tag>)}</Space>
                  </Descriptions.Item>
                )}
                {info.config.dockerEnv && info.config.dockerEnv.length > 0 && (
                  <Descriptions.Item label={t('docker.env')} span={3}>
                    <Space wrap size={4}>{info.config.dockerEnv.map((e, i) => <Tag key={i} bordered={false} className="taps-mono">{e}</Tag>)}</Space>
                  </Descriptions.Item>
                )}
              </>
            )}
          </Descriptions>
        </Card>
      )}

      {/* Game address: always visible (even for the user role) — they need
          it to actually connect. Admins already see it in the descriptions
          card above, so render this slim card only for non-admins. */}
      {!isAdmin && info?.config.type === 'docker' && info.config.dockerPorts && info.config.dockerPorts.length > 0 && (
        <Card style={{ marginBottom: 16 }} bodyStyle={{ padding: 18 }}>
          <div style={{ fontSize: 12, color: 'var(--taps-text-muted)', marginBottom: 12, textTransform: 'uppercase', letterSpacing: '0.04em', fontWeight: 500 }}>
            {t('docker.address')}
          </div>
          <Space wrap size={4}>{info.config.dockerPorts.map((p, i) => {
            const parsed = parsePortSpec(p)
            const host = displayHostOf(daemon) || '<host>'
            const label = parsed
              ? `${host}:${parsed.host}${parsed.proto ? '/' + parsed.proto : ''}`
              : p
            return (
              <Tooltip key={i} title={t('common.clickCopy')}>
                <Tag
                  color="blue" bordered={false} className="taps-mono"
                  style={{ cursor: 'pointer' }}
                  onClick={async () => {
                    if (await copyToClipboard(label)) message.success(t('common.copied'))
                    else message.error(t('common.error'))
                  }}
                >{label}</Tag>
              </Tooltip>
            )
          })}</Space>
        </Card>
      )}

      {/* Always render the resource card for docker instances so the page
          layout is stable from first paint. Without this, the card would
          appear once stats/volume RPCs return, pushing the terminal down
          and triggering its height-recomputation bug. */}
      {info?.config.type === 'docker' && (
        <Card style={{ marginBottom: 16 }} bodyStyle={{ padding: 18 }}>
          <div style={{ fontSize: 12, color: 'var(--taps-text-muted)', marginBottom: 12, textTransform: 'uppercase', letterSpacing: '0.04em', fontWeight: 500 }}>
            {t('instance.resources')}
          </div>
          <div style={{ display: 'grid', gridTemplateColumns: 'repeat(3, 1fr)', gap: 24 }}>
            {(() => {
              // CPU usage from docker stats is reported as % of a single
              // core, so a 4-core process maxes at 400%. The limit comes
              // from dockerCpu (e.g. "1.5" → 150%); without a limit we
              // show against a 100% (single core) baseline.
              const cpuLimit = parseFloat(info.config.dockerCpu || '') || 1
              const limitPct = cpuLimit * 100
              if (stats && info.status === 'running') {
                return (
                  <ResourceBar
                    label={t('instance.cpuUsage')}
                    used={stats.cpuPercent}
                    total={limitPct}
                    fmt={(n) => n.toFixed(1) + '%'}
                  />
                )
              }
              return <ResourceBar label={t('instance.cpuUsage')}
                loading={info.status === 'running'}
                placeholder={info.status === 'running' ? undefined : t('instance.notRunning')} />
            })()}
            {stats && info.status === 'running' && stats.memLimit > 0 ? (
              <ResourceBar
                label={t('instance.memUsage')}
                used={stats.memBytes}
                total={stats.memLimit}
                fmt={fmtBytes}
              />
            ) : (
              <ResourceBar
                label={t('instance.memUsage')}
                loading={info.status === 'running'}
                placeholder={info.status === 'running' ? undefined : t('instance.notRunning')}
              />
            )}
            {(() => {
              // Disk usage: prefer the volumes API result (admin-only,
              // most precise), fall back to the disk fields stitched
              // onto dockerStats by the daemon — that path also works
              // for non-admin users since per-instance dockerStats is
              // access-checked, not admin-only.
              const diskUsed = vol?.usedBytes ?? stats?.diskUsedBytes
              const diskTotal = vol?.sizeBytes ?? stats?.diskTotalBytes
              if (diskTotal && diskTotal > 0) {
                return <ResourceBar label={t('instance.diskUsage')}
                  used={diskUsed ?? 0} total={diskTotal} fmt={fmtBytes} />
              }
              if (info.config.dockerDiskSize) {
                return <ResourceBar label={t('instance.diskUsage')} loading />
              }
              return <ResourceBar label={t('instance.diskUsage')} placeholder={t('docker.diskNoVol')} />
            })()}
          </div>
        </Card>
      )}

      <Card bodyStyle={{ padding: 0 }}>
        <Tabs items={tabs} tabBarStyle={{ padding: '0 18px', margin: 0 }} />
      </Card>

      <Modal
        title={t('common.edit')}
        open={editOpen}
        onCancel={() => { setEditOpen(false); form.resetFields() }}
        onOk={onSave}
        forceRender
        width={720}
      >
        <Form form={form} layout="vertical">
          <Form.Item name="name" label={t('instance.name')} rules={[{ required: true }]}><Input /></Form.Item>
          {isAdmin && (
            <Form.Item name="type" label={t('instance.type')} initialValue={info?.config.type || 'docker'}>
              <Select disabled={!!daemon?.requireDocker}
                onChange={(v: string) => setEditType(v)}
                options={(daemon?.requireDocker ? ['docker'] : ['generic', 'minecraft', 'bedrock', 'terraria', 'docker']).map(v => ({ label: v, value: v }))} />
            </Form.Item>
          )}
          {editType === 'docker' ? (
            <Form.Item name="command" label={t('instance.runtime')} rules={[{ required: true }]}
              extra={imageOptions.length === 0
                ? <span>{t('instance.imageEmpty')} {isAdmin && <Link to="/images">{t('instance.imageGoPull')}</Link>}</span>
                : t('instance.runtimeHelp')}
            >
              <Select showSearch options={imageOptions}
                placeholder={imageOptions.length === 0 ? t('instance.imageEmptyPh') : t('instance.imagePickPh')} />
            </Form.Item>
          ) : isAdmin ? (
            <Form.Item name="command" label={t('instance.command')} rules={[{ required: true }]}><Input.TextArea rows={2} /></Form.Item>
          ) : null}
          {editType === 'docker' && (
            <Form.Item name="argsText" label={t('instance.dockerCmd')} extra={t('instance.dockerCmdHelp')}>
              <Input className="taps-mono" placeholder='java -Xmx2G -jar server.jar nogui' />
            </Form.Item>
          )}
          {isAdmin && <Form.Item name="workingDir" label={t('instance.workingDir')}><Input /></Form.Item>}
          <Form.Item name="stopCmd" label={t('instance.stopCmd')}><Input /></Form.Item>
          <Form.Item name="completionWordsText" label={t('instance.completion')} extra={t('instance.completionHelp')}>
            <Input.TextArea rows={3} placeholder={'/list\n/save-all\n/op <player>'} className="taps-mono" />
          </Form.Item>
          <Space wrap>
            {isAdmin && <Form.Item name="autoStart" valuePropName="checked" label={t('instance.autoStart')}><Switch /></Form.Item>}
            {isAdmin && <Form.Item name="autoRestart" valuePropName="checked" label={t('restart.auto')}><Switch /></Form.Item>}
            {isAdmin && <Form.Item name="restartDelay" label={t('restart.delay')}><InputNumber min={1} max={3600} /></Form.Item>}
            <Form.Item name="outputEncoding" label={t('encoding.label')} extra={t('encoding.help')}>
              <Select style={{ width: 140 }} options={[
                { label: 'utf-8', value: 'utf-8' },
                { label: 'gbk', value: 'gbk' },
                { label: 'gb18030', value: 'gb18030' },
                { label: 'big5', value: 'big5' },
              ]} />
            </Form.Item>
          </Space>
          {isAdmin && editType === 'minecraft' && (
            <Space>
              <Form.Item name="minecraftHost" label="MC Host"><Input placeholder="127.0.0.1" /></Form.Item>
              <Form.Item name="minecraftPort" label="MC Port"><InputNumber min={1} max={65535} placeholder="25565" /></Form.Item>
            </Space>
          )}
          {!isAdmin && info?.config.type === 'docker' && (
            <Form.Item name="containerPort" label={t('docker.containerPort')} extra={t('docker.containerPortHelp')}>
              <InputNumber min={1} max={65535} placeholder={t('docker.containerPortPh')} style={{ width: '100%' }} />
            </Form.Item>
          )}
          {isAdmin && editType === 'docker' && (
            <>
              <Form.Item name="dockerEnvText" label={t('docker.env')}><Input.TextArea rows={3} placeholder='EULA=TRUE&#10;MEMORY=2G' /></Form.Item>
              <Form.Item name="dockerVolumesText" label={t('docker.volumes')}><Input.TextArea rows={2} placeholder='./mc-data:/data' /></Form.Item>
                  <Form.Item name="hostPort" label={t('docker.port')} extra={t('docker.portEditHelp', { min: portMin, max: portMax })}>
                    <InputNumber min={portMin} max={portMax} placeholder={t('docker.portAutoPh')} style={{ width: '100%' }} />
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
                    <Form.Item name="dockerDiskSize" label={t('docker.disk')} rules={[{ required: true, message: t('docker.required') }]} style={{ flex: 1 }}
                      extra={info?.config.managedVolume ? t('docker.diskGrowOnly') : t('docker.diskNoVol')}>
                      <Input placeholder="10g" disabled={!info?.config.managedVolume} />
                    </Form.Item>
                  </Space.Compact>
                  <Form.Item label={t('hib.enabled')} extra={t('hib.enabledHelp')}>
                    <Form.Item name="hibernationMode" noStyle>
                      <Radio.Group>
                        <Radio.Button value="default">{t('hib.useDefault')}</Radio.Button>
                        <Radio.Button value="on">{t('hib.forceOn')}</Radio.Button>
                        <Radio.Button value="off">{t('hib.forceOff')}</Radio.Button>
                      </Radio.Group>
                    </Form.Item>
                  </Form.Item>
                  <Form.Item name="hibernationIdleMinutes" label={t('hib.idleMinutes')} extra={t('hib.idleMinutesHelp')}>
                    <InputNumber min={1} max={1440} placeholder={t('hib.idleMinutesPh')} style={{ width: 200 }} />
                  </Form.Item>
                </>
          )}
        </Form>
      </Modal>
      {info && (
        <SetupWizard
          open={setupOpen}
          onClose={() => setSetupOpen(false)}
          daemonId={did}
          info={info}
          onDone={reload}
        />
      )}
      <ServerDeployModal
        open={srvDeployOpen}
        daemonId={did}
        uuid={uuid}
        onClose={() => setSrvDeployOpen(false)}
        onDone={() => { setSrvDeployOpen(false); reload() }}
      />
    </>
  )
}

function fmtBytes(n: number): string {
  if (n < 1024) return n + ' B'
  if (n < 1024 ** 2) return (n / 1024).toFixed(1) + ' KB'
  if (n < 1024 ** 3) return (n / 1024 ** 2).toFixed(1) + ' MB'
  return (n / 1024 ** 3).toFixed(2) + ' GB'
}

interface ResourceBarProps {
  label: string
  used?: number
  total?: number
  fmt?: (n: number) => string
  placeholder?: string
  loading?: boolean
}
function ResourceBar({ label, used, total, fmt, placeholder, loading }: ResourceBarProps) {
  if (loading) {
    return (
      <div>
        <div style={{ display: 'flex', justifyContent: 'space-between', fontSize: 13, marginBottom: 6, alignItems: 'center' }}>
          <span>{label}</span>
          <Skeleton.Input active size="small" style={{ width: 110, height: 14, minWidth: 60 }} />
        </div>
        <Progress percent={100} status="active" showInfo={false} />
      </div>
    )
  }
  if (placeholder !== undefined || used === undefined || !total) {
    return (
      <div>
        <div style={{ display: 'flex', justifyContent: 'space-between', fontSize: 13, marginBottom: 6 }}>
          <span>{label}</span>
          <span style={{ color: 'var(--taps-text-muted)' }}>{placeholder ?? '—'}</span>
        </div>
        <Progress percent={0} showInfo={false} />
      </div>
    )
  }
  const pct = Math.min(100, Math.round((used / total) * 100))
  const f = fmt ?? ((n: number) => String(n))
  return (
    <div>
      <div style={{ display: 'flex', justifyContent: 'space-between', fontSize: 13, marginBottom: 6 }}>
        <span>{label}</span>
        <span className="taps-mono" style={{ color: 'var(--taps-text-muted)' }}>{f(used)} / {f(total)}</span>
      </div>
      <Progress
        percent={pct}
        format={(p) => (p ?? 0) + '%'}
        strokeColor={pct >= 90 ? '#ef4444' : pct >= 70 ? '#f59e0b' : '#007BFC'}
      />
    </div>
  )
}
