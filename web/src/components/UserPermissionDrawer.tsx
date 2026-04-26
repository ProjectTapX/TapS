import { useEffect, useMemo, useState } from 'react'
import { Drawer, Space, App, Alert, Empty, Spin, Switch, Tag, Input, Select, Pagination, Tooltip } from 'antd'
import { SearchOutlined } from '@ant-design/icons'
import { useTranslation } from 'react-i18next'
import { instancesApi, daemonsApi, type AggregateRow, type DaemonView } from '@/api/resources'
import { permsApi } from '@/api/tasks'
import { copyToClipboard } from '@/utils/clipboard'
import { formatApiError } from '@/api/errors'

interface Props {
  open: boolean
  onClose: () => void
  userId: number
  username: string
  role: string
}

const PAGE_SIZE = 8

export default function UserPermissionDrawer({ open, onClose, userId, username, role }: Props) {
  const { t } = useTranslation()
  const { message } = App.useApp()
  const [loading, setLoading] = useState(false)
  const [rows, setRows] = useState<AggregateRow[]>([])
  const [daemons, setDaemons] = useState<DaemonView[]>([])
  const [granted, setGranted] = useState<Set<string>>(new Set())
  const [busyKey, setBusyKey] = useState<string | null>(null)
  const [query, setQuery] = useState('')
  const [daemonFilter, setDaemonFilter] = useState<number | 'all'>('all')
  const [page, setPage] = useState(1)

  const key = (daemonId: number, uuid: string) => `${daemonId}|${uuid}`

  const load = async () => {
    setLoading(true)
    try {
      const [r, ds, ps] = await Promise.all([
        instancesApi.aggregate(),
        daemonsApi.list(),
        permsApi.list({ userId }),
      ])
      setRows(r)
      setDaemons(ds)
      setGranted(new Set(ps.map(p => key(p.daemonId, p.uuid))))
    } catch (e: any) {
      message.error(formatApiError(e, 'common.error'))
    } finally {
      setLoading(false)
    }
  }
  useEffect(() => { if (open) { load(); setPage(1); setQuery(''); setDaemonFilter('all') } }, [open, userId])

  const toggle = async (row: AggregateRow, value: boolean) => {
    const k = key(row.daemonId, row.info.config.uuid)
    setBusyKey(k)
    try {
      if (value) {
        await permsApi.grant({ userId, daemonId: row.daemonId, uuid: row.info.config.uuid })
        setGranted(s => new Set([...s, k]))
      } else {
        await permsApi.revoke({ userId, daemonId: row.daemonId, uuid: row.info.config.uuid })
        setGranted(s => { const ns = new Set(s); ns.delete(k); return ns })
      }
    } catch (e: any) {
      message.error(formatApiError(e, 'common.error'))
    } finally { setBusyKey(null) }
  }

  // Apply node + text filters before paginating. Search matches instance
  // name, type, or first 8 of UUID — case-insensitive.
  const filtered = useMemo(() => {
    const q = query.trim().toLowerCase()
    return rows.filter(r => {
      if (daemonFilter !== 'all' && r.daemonId !== daemonFilter) return false
      if (!q) return true
      const cfg = r.info.config
      return cfg.name.toLowerCase().includes(q)
        || (cfg.type ?? '').toLowerCase().includes(q)
        || cfg.uuid.toLowerCase().startsWith(q)
    })
  }, [rows, query, daemonFilter])

  // Reset to page 1 whenever filters change so users don't land on an
  // empty page after narrowing the result set.
  useEffect(() => { setPage(1) }, [query, daemonFilter])

  const pageRows = filtered.slice((page - 1) * PAGE_SIZE, page * PAGE_SIZE)

  return (
    <Drawer
      open={open}
      onClose={onClose}
      width={560}
      title={t('userPerm.title', { username })}
      destroyOnClose
    >
      {role === 'admin' ? (
        <Alert type="info" showIcon message={t('userPerm.adminNote')} />
      ) : (
        <>
          <Alert type="info" showIcon style={{ marginBottom: 16 }}
            message={t('userPerm.note')} />

          <Space.Compact style={{ width: '100%', marginBottom: 12 }}>
            <Input
              prefix={<SearchOutlined />}
              placeholder={t('userPerm.searchPh')}
              allowClear
              value={query}
              onChange={(e) => setQuery(e.target.value)}
              style={{ flex: 1 }}
            />
            <Select
              value={daemonFilter}
              onChange={(v) => setDaemonFilter(v)}
              style={{ width: 200 }}
              options={[
                { value: 'all', label: t('userPerm.allNodes') },
                ...daemons.map(d => ({ value: d.id, label: d.name })),
              ]}
            />
          </Space.Compact>

          {loading ? (
            <div style={{ textAlign: 'center', padding: 32 }}><Spin /></div>
          ) : filtered.length === 0 ? (
            <Empty description={query || daemonFilter !== 'all' ? t('userPerm.noMatches') : t('userPerm.noInstances')} />
          ) : (
            <>
              {pageRows.map(r => {
                const k = key(r.daemonId, r.info.config.uuid)
                const isGranted = granted.has(k)
                const d = daemons.find(x => x.id === r.daemonId)
                return (
                  <div key={k} style={{
                    display: 'flex', alignItems: 'center', justifyContent: 'space-between',
                    padding: '10px 12px', marginBottom: 6,
                    border: '1px solid var(--taps-border)', borderRadius: 10,
                    background: isGranted ? 'var(--taps-primary-soft)' : 'transparent',
                  }}>
                    <Space direction="vertical" size={0} style={{ minWidth: 0, flex: 1 }}>
                      <span style={{ fontWeight: 500 }}>{r.info.config.name}</span>
                      <Tooltip title={r.info.config.uuid}>
                        <span className="taps-mono" style={{ fontSize: 11, color: 'var(--taps-text-muted)', cursor: 'pointer', wordBreak: 'break-all' }}
                          onClick={async () => { if (await copyToClipboard(r.info.config.uuid)) message.success(t('common.copied')) }}>
                          {d?.name ?? `#${r.daemonId}`} · {r.info.config.type} · {r.info.config.uuid}
                        </span>
                      </Tooltip>
                    </Space>
                    <Switch
                      checked={isGranted}
                      loading={busyKey === k}
                      onChange={(v) => toggle(r, v)}
                    />
                  </div>
                )
              })}
              {filtered.length > PAGE_SIZE && (
                <div style={{ textAlign: 'center', marginTop: 12 }}>
                  <Pagination
                    size="small"
                    current={page}
                    pageSize={PAGE_SIZE}
                    total={filtered.length}
                    showSizeChanger={false}
                    onChange={setPage}
                  />
                </div>
              )}
            </>
          )}
          <div style={{ marginTop: 16, color: 'var(--taps-text-muted)', fontSize: 12 }}>
            {t('userPerm.granted', { n: granted.size })} · <Tag>{role}</Tag>
          </div>
        </>
      )}
    </Drawer>
  )
}
