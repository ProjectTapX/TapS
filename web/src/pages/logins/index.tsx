import { useEffect, useState } from 'react'
import { Table, Input, Space, App, Button, Card, Tag, Tooltip, Select } from 'antd'
import { ReloadOutlined } from '@ant-design/icons'
import { useTranslation } from 'react-i18next'
import { api } from '@/api/client'
import PageHeader from '@/components/PageHeader'
import { formatApiError } from '@/api/errors'

interface LoginRow {
  id: number
  time: string
  username: string
  userId: number
  success: boolean
  reason?: string
  ip: string
  userAgent: string
}

// Cheap browser/OS heuristic — keeps a UA string skimmable in the table
// without pulling in a real ua-parser dep.
function summarizeUA(ua: string): string {
  if (!ua) return '—'
  const browser = /Edg\/(\S+)/.exec(ua) ? 'Edge'
    : /Chrome\/(\S+)/.exec(ua) ? 'Chrome'
    : /Firefox\/(\S+)/.exec(ua) ? 'Firefox'
    : /Safari\/(\S+)/.exec(ua) ? 'Safari'
    : ua.split(' ')[0]
  const os = /Windows NT 10/.test(ua) ? 'Windows'
    : /Mac OS X/.test(ua) ? 'macOS'
    : /Android/.test(ua) ? 'Android'
    : /iPhone|iPad/.test(ua) ? 'iOS'
    : /Linux/.test(ua) ? 'Linux'
    : ''
  return os ? `${browser} · ${os}` : browser
}

// Map the canonical reason strings written by the backend to localized
// labels. Unknown reasons are passed through untranslated.
function localizeReason(r: string, t: (k: string) => string): string {
  switch (r) {
    case 'no such user': return t('logins.reasonNoUser')
    case 'wrong password': return t('logins.reasonWrongPwd')
    case 'issue token failed': return t('logins.reasonTokenFail')
    default: return r
  }
}

export default function LoginsPage() {
  const { t } = useTranslation()
  const { message } = App.useApp()
  const [data, setData] = useState<LoginRow[]>([])
  const [total, setTotal] = useState(0)
  const [loading, setLoading] = useState(false)
  const [username, setUsername] = useState('')
  const [success, setSuccess] = useState<'all' | 'true' | 'false'>('all')
  const [reason, setReason] = useState<string>('')
  const [page, setPage] = useState(1)
  const pageSize = 50

  // The reason values are written by the panel login handler; keep this
  // list in sync with auth.go. Selecting any reason implicitly filters
  // success=false on the server too.
  const REASONS = [
    { value: 'no such user', labelKey: 'logins.reasonNoUser' },
    { value: 'wrong password', labelKey: 'logins.reasonWrongPwd' },
    { value: 'issue token failed', labelKey: 'logins.reasonTokenFail' },
  ]

  const load = async () => {
    setLoading(true)
    try {
      const params: Record<string, any> = { limit: pageSize, offset: (page - 1) * pageSize }
      if (username) params.username = username
      if (success !== 'all') params.success = success
      if (reason) params.reason = reason
      const r = await api.get('/logins', { params })
      setData(r.data.items ?? [])
      setTotal(r.data.total ?? 0)
    } catch (e: any) {
      message.error(formatApiError(e, 'common.error'))
    } finally { setLoading(false) }
  }
  useEffect(() => { load() }, [page, username, success, reason])

  return (
    <>
      <PageHeader
        title={t('menu.logins')}
        subtitle={t('logins.pageSubtitle')}
        extra={
          <Space.Compact>
            <Input
              placeholder={t('audit.user')} allowClear style={{ width: 160 }}
              value={username}
              onChange={(e) => { setPage(1); setUsername(e.target.value) }}
            />
            <Select
              value={success}
              onChange={(v) => { setPage(1); setSuccess(v); if (v === 'true') setReason('') }}
              style={{ width: 120 }}
              options={[
                { value: 'all', label: t('logins.allResults') },
                { value: 'true', label: t('user.loginOk') },
                { value: 'false', label: t('user.loginFail') },
              ]}
            />
            <Select
              value={reason || 'all'}
              onChange={(v) => { setPage(1); setReason(v === 'all' ? '' : v); if (v !== 'all') setSuccess('false') }}
              style={{ width: 160 }}
              options={[
                { value: 'all', label: t('logins.allReasons') },
                ...REASONS.map(r => ({ value: r.value, label: t(r.labelKey) })),
              ]}
            />
            <Button icon={<ReloadOutlined />} onClick={load} />
          </Space.Compact>
        }
      />
      <Card bodyStyle={{ padding: 0 }}>
        <Table<LoginRow>
          rowKey="id"
          loading={loading}
          dataSource={data}
          size="small"
          pagination={{ current: page, pageSize, total, onChange: setPage, showSizeChanger: false }}
          columns={[
            {
              title: t('common.status'), dataIndex: 'success', width: 70,
              render: (s: boolean) => s
                ? <Tag color="success">{t('user.loginOk')}</Tag>
                : <Tag color="error">{t('user.loginFail')}</Tag>,
            },
            { title: t('common.time'), dataIndex: 'time', width: 180, render: (s: string) => <span className="taps-mono" style={{ fontSize: 12 }}>{new Date(s).toLocaleString()}</span> },
            { title: t('audit.user'), dataIndex: 'username', width: 140, render: (v) => v || '—' },
            { title: 'IP', dataIndex: 'ip', width: 140, render: (v) => <span className="taps-mono" style={{ fontSize: 12 }}>{v}</span> },
            {
              title: t('user.userAgent'), dataIndex: 'userAgent', ellipsis: true,
              render: (v: string) => <Tooltip title={v}><span style={{ fontSize: 12, color: 'var(--taps-text-muted)' }}>{summarizeUA(v)}</span></Tooltip>,
            },
            { title: t('user.failReason'), dataIndex: 'reason', width: 160,
              render: (v: string) => v ? <span style={{ color: 'var(--taps-text-muted)', fontSize: 12 }}>{localizeReason(v, t)}</span> : '—',
            },
          ]}
        />
      </Card>
    </>
  )
}
