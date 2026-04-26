import { useEffect, useState } from 'react'
import { Table, Input, Space, App, Button, Card } from 'antd'
import { ReloadOutlined } from '@ant-design/icons'
import { useTranslation } from 'react-i18next'
import { api } from '@/api/client'
import PageHeader from '@/components/PageHeader'
import StatusBadge from '@/components/StatusBadge'
import { formatApiError } from '@/api/errors'

interface AuditRow {
  id: number
  time: string
  userId: number
  username: string
  method: string
  path: string
  status: number
  ip: string
  durationMs: number
}

const METHOD_VARIANT: Record<string, 'info' | 'warning' | 'danger' | 'neutral'> = {
  POST: 'info', PUT: 'warning', DELETE: 'danger', GET: 'neutral',
}

export default function AuditPage() {
  const { t } = useTranslation()
  const { message } = App.useApp()
  const [data, setData] = useState<AuditRow[]>([])
  const [total, setTotal] = useState(0)
  const [loading, setLoading] = useState(false)
  const [filters, setFilters] = useState({ username: '', path: '' })
  const [page, setPage] = useState(1)
  const pageSize = 50

  const load = async () => {
    setLoading(true)
    try {
      const r = await api.get('/audit', { params: { ...filters, limit: pageSize, offset: (page - 1) * pageSize } })
      setData(r.data.items ?? [])
      setTotal(r.data.total ?? 0)
    } catch (e: any) {
      message.error(formatApiError(e, 'common.error'))
    } finally { setLoading(false) }
  }
  useEffect(() => { load() }, [page, filters.username, filters.path])

  return (
    <>
      <PageHeader
        title={t('menu.audit')}
        subtitle={t('audit.pageSubtitle')}
        extra={
          <Space.Compact>
            <Input placeholder={t('audit.user')} allowClear style={{ width: 160 }}
              value={filters.username} onChange={(e) => { setPage(1); setFilters(f => ({ ...f, username: e.target.value })) }} />
            <Input placeholder={t('audit.path')} allowClear style={{ width: 240 }}
              value={filters.path} onChange={(e) => { setPage(1); setFilters(f => ({ ...f, path: e.target.value })) }} />
            <Button icon={<ReloadOutlined />} onClick={load} />
          </Space.Compact>
        }
      />
      <Card bodyStyle={{ padding: 0 }}>
        <Table<AuditRow>
          rowKey="id"
          loading={loading}
          dataSource={data}
          size="small"
          pagination={{ current: page, pageSize, total, onChange: setPage, showSizeChanger: false }}
          columns={[
            { title: t('common.id'), dataIndex: 'id', width: 70 },
            { title: t('audit.time'), dataIndex: 'time', width: 180, render: (s: string) => <span className="taps-mono" style={{ fontSize: 12 }}>{new Date(s).toLocaleString()}</span> },
            { title: t('audit.user'), dataIndex: 'username', width: 120, render: (v) => v || '—' },
            { title: t('audit.method'), dataIndex: 'method', width: 90, render: (v: string) => <StatusBadge variant={METHOD_VARIANT[v] ?? 'neutral'}>{v}</StatusBadge> },
            { title: t('audit.path'), dataIndex: 'path', render: (v) => <span className="taps-mono">{v}</span> },
            { title: t('audit.status'), dataIndex: 'status', width: 80, render: (s: number) => <StatusBadge variant={s >= 400 ? 'danger' : 'success'}>{s}</StatusBadge> },
            { title: t('audit.ip'), dataIndex: 'ip', width: 140, render: (v) => <span className="taps-mono" style={{ fontSize: 12 }}>{v}</span> },
            { title: 'ms', dataIndex: 'durationMs', width: 70, align: 'right' },
          ]}
        />
      </Card>
    </>
  )
}
