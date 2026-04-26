import { useEffect, useState } from 'react'
import { Button, Table, Space, Modal, Form, Input, Popconfirm, App, Tag, Alert, Typography, Card, DatePicker, Radio } from 'antd'
import { CopyOutlined, PlusOutlined, ReloadOutlined, StopOutlined } from '@ant-design/icons'
import { useTranslation } from 'react-i18next'
import dayjs, { type Dayjs } from 'dayjs'
import { apiKeysApi, type ApiKeyRow } from '@/api/tasks'
import { copyToClipboard } from '@/utils/clipboard'
import PageHeader from '@/components/PageHeader'
import { formatApiError } from '@/api/errors'

// One of the quick "expire in N days" radio choices, plus "custom"
// (date picker) and "never". The shortcut buttons keep the common
// case (90-day rotation) one click away.
type ExpiryChoice = 'never' | 'd30' | 'd90' | 'd365' | 'custom'

export default function ApiKeysPage() {
  const { t } = useTranslation()
  const { message, notification, modal } = App.useApp()
  const [data, setData] = useState<ApiKeyRow[]>([])
  const [loading, setLoading] = useState(false)
  const [open, setOpen] = useState(false)
  const [created, setCreated] = useState<string | null>(null)
  const [form] = Form.useForm()
  const [expiryChoice, setExpiryChoice] = useState<ExpiryChoice>('never')
  const [customDate, setCustomDate] = useState<Dayjs | null>(null)

  const load = async () => {
    setLoading(true)
    try { setData(await apiKeysApi.list()) } finally { setLoading(false) }
  }
  useEffect(() => { load() }, [])

  const resolveExpiresAt = (): string | undefined => {
    switch (expiryChoice) {
      case 'never':  return undefined
      case 'd30':    return dayjs().add(30, 'day').toISOString()
      case 'd90':    return dayjs().add(90, 'day').toISOString()
      case 'd365':   return dayjs().add(365, 'day').toISOString()
      case 'custom': return customDate ? customDate.toISOString() : undefined
    }
  }

  const onSubmit = async () => {
    const v = await form.validateFields()
    const expiresAt = resolveExpiresAt()
    if (expiryChoice === 'custom' && !expiresAt) {
      message.warning(t('apikey.expiryPickRequired'))
      return
    }
    try {
      const r = await apiKeysApi.create({ ...v, expiresAt })
      setCreated(r.key); setOpen(false); form.resetFields()
      setExpiryChoice('never'); setCustomDate(null)
      load()
    } catch (e: any) { message.error(formatApiError(e, 'common.error')) }
  }

  const onRevokeAll = () => {
    modal.confirm({
      title: t('apikey.confirmRevokeAll'),
      content: t('apikey.confirmRevokeAllHint'),
      okType: 'danger',
      okText: t('apikey.revokeAll'),
      cancelText: t('common.cancel'),
      onOk: async () => {
        try {
          const r = await apiKeysApi.revokeAll()
          message.success(t('apikey.revokeAllOk', { n: r.revoked }))
          load()
        } catch (e: any) { message.error(formatApiError(e, 'common.error')) }
      },
    })
  }

  // status label per row: revoked > expired > active
  const statusOf = (r: ApiKeyRow): { color: string; label: string } => {
    if (r.revokedAt) return { color: 'red', label: t('apikey.statusRevoked') }
    if (r.expiresAt && dayjs(r.expiresAt).isBefore(dayjs())) return { color: 'orange', label: t('apikey.statusExpired') }
    return { color: 'green', label: t('apikey.statusActive') }
  }

  return (
    <>
      <PageHeader
        title={t('menu.apikeys')}
        subtitle={t('apikey.pageSubtitle')}
        extra={
          <>
            <Button icon={<ReloadOutlined />} onClick={load}>{t('common.refresh')}</Button>
            <Button danger icon={<StopOutlined />} onClick={onRevokeAll}>{t('apikey.revokeAll')}</Button>
            <Button type="primary" icon={<PlusOutlined />} onClick={() => { form.resetFields(); setExpiryChoice('never'); setCustomDate(null); setOpen(true) }}>{t('apikey.issue')}</Button>
          </>
        }
      />
      <Card bodyStyle={{ padding: 0 }}>
        <Table<ApiKeyRow>
          rowKey="id"
          loading={loading}
          dataSource={data}
          pagination={false}
          columns={[
            { title: t('common.id'), dataIndex: 'id', width: 60 },
            { title: t('common.name'), dataIndex: 'name', render: (v) => <span style={{ fontWeight: 500 }}>{v}</span> },
            { title: t('apikey.prefix'), dataIndex: 'prefix', render: (v) => <code className="taps-mono">{v}…</code> },
            { title: t('apikey.user'), dataIndex: 'userId', width: 80 },
            {
              title: t('apikey.status'), width: 90,
              render: (_, r) => {
                const s = statusOf(r)
                return <Tag color={s.color}>{s.label}</Tag>
              },
            },
            {
              title: t('apikey.expiresAt'), width: 160, dataIndex: 'expiresAt',
              render: (s?: string) => s ? dayjs(s).format('YYYY-MM-DD HH:mm') : <Typography.Text type="secondary">{t('apikey.never')}</Typography.Text>,
            },
            { title: t('apikey.lastUsed'), dataIndex: 'lastUsed', render: (s: string) => s && new Date(s).getFullYear() > 2000 ? new Date(s).toLocaleString() : '—' },
            { title: t('common.createdAt'), dataIndex: 'createdAt', render: (s: string) => new Date(s).toLocaleString() },
            {
              title: t('common.actions'), width: 200, align: 'right',
              render: (_, r) => (
                <Space size={4}>
                  {!r.revokedAt && (
                    <Popconfirm title={t('apikey.confirmRevoke')} onConfirm={async () => {
                      try { await apiKeysApi.revoke(r.id); message.success(t('common.success')); load() }
                      catch (e: any) { message.error(formatApiError(e, 'common.error')) }
                    }}>
                      <Button size="small">{t('apikey.revoke')}</Button>
                    </Popconfirm>
                  )}
                  <Popconfirm title={t('apikey.confirmDelete')} onConfirm={async () => {
                    try { await apiKeysApi.remove(r.id); message.success(t('common.success')); load() }
                    catch (e: any) { message.error(formatApiError(e, 'common.error')) }
                  }}>
                    <Button size="small" danger>{t('apikey.delete')}</Button>
                  </Popconfirm>
                </Space>
              ),
            },
          ]}
        />
      </Card>

      <Modal title={t('apikey.issueTitle')} open={open} onCancel={() => setOpen(false)} onOk={onSubmit} destroyOnClose>
        <Form form={form} layout="vertical">
          <Form.Item name="name" label={t('apikey.name')} rules={[{ required: true }]}>
            <Input placeholder={t('apikey.namePh')} />
          </Form.Item>
          <Form.Item name="ipWhitelist" label={t('apikey.ipWhitelist')} extra={t('apikey.ipWhitelistHelp')}>
            <Input placeholder="10.0.0.0/8, 192.168.1.42" />
          </Form.Item>
          <Form.Item name="scopes" label={t('apikey.scopes')} extra={t('apikey.scopesHelp')}>
            <Input placeholder="instance.read, instance.control" />
          </Form.Item>
          <Form.Item label={t('apikey.expiresAt')} extra={t('apikey.expiresAtHelp')}>
            <Space direction="vertical" style={{ width: '100%' }}>
              <Radio.Group value={expiryChoice} onChange={(e) => setExpiryChoice(e.target.value)}>
                <Radio value="never">{t('apikey.never')}</Radio>
                <Radio value="d30">30 {t('apikey.days')}</Radio>
                <Radio value="d90">90 {t('apikey.days')}</Radio>
                <Radio value="d365">365 {t('apikey.days')}</Radio>
                <Radio value="custom">{t('apikey.expiryCustom')}</Radio>
              </Radio.Group>
              {expiryChoice === 'custom' && (
                <DatePicker showTime value={customDate} onChange={setCustomDate} disabledDate={(d) => d && d.isBefore(dayjs(), 'minute')} style={{ width: '100%' }} />
              )}
            </Space>
          </Form.Item>
        </Form>
      </Modal>

      <Modal
        title={t('apikey.showOnce')}
        open={!!created}
        onCancel={() => setCreated(null)}
        onOk={() => setCreated(null)}
        cancelButtonProps={{ style: { display: 'none' } }}
        okText={t('apikey.saved')}
      >
        <Alert type="warning" showIcon style={{ marginBottom: 12 }} message={t('apikey.showOnceWarn')} />
        <Space.Compact style={{ width: '100%' }}>
          <Input value={created ?? ''} readOnly />
          <Button icon={<CopyOutlined />} type="primary" onClick={async () => {
            const ok = await copyToClipboard(created ?? '')
            if (ok) {
              notification.success({
                message: t('apikey.copied'),
                description: t('apikey.copiedHint'),
                placement: 'topRight',
                duration: 3,
              })
            } else {
              notification.error({
                message: t('common.error'),
                description: t('apikey.copyManual'),
                placement: 'topRight',
              })
            }
          }}>
            {t('apikey.copy')}
          </Button>
        </Space.Compact>
        <Typography.Paragraph type="secondary" style={{ marginTop: 12 }}>
          {t('apikey.usage')} <Tag><code>Authorization: Bearer &lt;key&gt;</code></Tag>
        </Typography.Paragraph>
      </Modal>
    </>
  )
}
