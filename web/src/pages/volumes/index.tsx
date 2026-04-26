import { useEffect, useState } from 'react'
import { Button, Table, Space, Modal, Form, Input, InputNumber, Select, Popconfirm, App, Alert, Tag, Empty, Card, Tooltip } from 'antd'
import { PlusOutlined, ReloadOutlined, CopyOutlined } from '@ant-design/icons'
import { useTranslation } from 'react-i18next'
import { daemonsApi, type DaemonView } from '@/api/resources'
import { volumesApi, type Volume } from '@/api/volumes'
import { copyToClipboard } from '@/utils/clipboard'
import PageHeader from '@/components/PageHeader'
import StatusBadge from '@/components/StatusBadge'
import { formatApiError } from '@/api/errors'

function fmtSize(n: number) {
  if (!n) return '-'
  const u = ['B', 'KB', 'MB', 'GB', 'TB']
  let i = 0
  let v = n
  while (v >= 1024 && i < u.length - 1) { v /= 1024; i++ }
  return `${v.toFixed(1)} ${u[i]}`
}

export default function VolumesPage() {
  const { t } = useTranslation()
  const { message } = App.useApp()
  const [daemons, setDaemons] = useState<DaemonView[]>([])
  const [daemonId, setDaemonId] = useState<number | null>(null)
  const [available, setAvailable] = useState(true)
  const [errMsg, setErrMsg] = useState('')
  const [data, setData] = useState<Volume[]>([])
  const [loading, setLoading] = useState(false)
  const [open, setOpen] = useState(false)
  const [creating, setCreating] = useState(false)
  const [form] = Form.useForm()

  const loadDaemons = async () => {
    const ds = await daemonsApi.list()
    setDaemons(ds)
    if (!daemonId && ds.length > 0) setDaemonId(ds[0].id)
  }
  useEffect(() => { loadDaemons() }, [])

  const load = async () => {
    if (!daemonId) return
    setLoading(true)
    try {
      const r = await volumesApi(daemonId).list()
      setAvailable(r.available); setErrMsg(r.error ?? ''); setData(r.volumes ?? [])
    } catch (e: any) {
      setAvailable(false); setErrMsg(formatApiError(e, ''))
    } finally { setLoading(false) }
  }
  useEffect(() => { load() }, [daemonId])

  const onCreate = async () => {
    const v = await form.validateFields()
    setCreating(true)
    try {
      await volumesApi(daemonId!).create({
        name: v.name,
        sizeBytes: Math.round(v.sizeGB * 1024 * 1024 * 1024),
        fsType: v.fsType ?? 'ext4',
      })
      message.success(t('common.success'))
      setOpen(false); form.resetFields(); load()
    } catch (e: any) {
      message.error(formatApiError(e, 'common.error'))
    } finally { setCreating(false) }
  }

  const copyMount = async (v: Volume) => {
    const text = `${v.mountPath}:/data`
    if (await copyToClipboard(text)) message.success(t('volumes.copied'))
    else message.warning(text)
  }

  return (
    <>
      <PageHeader
        title={t('menu.volumes')}
        subtitle={t('volumes.subtitle')}
        extra={
          <>
            <Select
              value={daemonId ?? undefined}
              onChange={setDaemonId}
              options={daemons.map(d => ({ label: d.name, value: d.id, disabled: !d.connected }))}
              style={{ width: 200 }}
              placeholder={t('instance.node')}
            />
            <Button icon={<ReloadOutlined />} onClick={load} disabled={!daemonId}>{t('common.refresh')}</Button>
            <Button type="primary" icon={<PlusOutlined />} onClick={() => setOpen(true)} disabled={!daemonId || !available || creating}>
              {t('volumes.create')}
            </Button>
          </>
        }
      />

      {!available && (
        <Alert type="warning" showIcon style={{ marginBottom: 16 }}
          message={t('volumes.unavailable')} description={errMsg || t('volumes.unavailableDesc')} />
      )}

      <Card bodyStyle={{ padding: 0 }}>
        <Table<Volume>
          rowKey="name"
          loading={loading}
          dataSource={data}
          locale={{ emptyText: <Empty description={t('volumes.empty')} /> }}
          pagination={false}
          columns={[
            {
              title: t('common.name'), dataIndex: 'name',
              render: (v: string, r) => (
                <Space direction="vertical" size={0}>
                  <span style={{ fontWeight: 500 }}>{v}</span>
                  <span className="taps-mono" style={{ color: 'var(--taps-text-muted)', fontSize: 11 }}>{r.mountPath}</span>
                </Space>
              ),
            },
            { title: t('volumes.size'), dataIndex: 'sizeBytes', width: 120, render: fmtSize },
            { title: t('volumes.used'), dataIndex: 'usedBytes', width: 160,
              render: (n: number, r) => n ? <span>{fmtSize(n)} <span style={{ color: 'var(--taps-text-muted)' }}>({Math.round(n / r.sizeBytes * 100)}%)</span></span> : '-' },
            { title: t('volumes.fs'), dataIndex: 'fsType', width: 90, render: (v) => <Tag bordered={false}>{v}</Tag> },
            {
              title: t('instance.status_'), dataIndex: 'mounted', width: 110,
              render: (v: boolean) => v ? <StatusBadge variant="success">{t('volumes.mounted')}</StatusBadge> : <StatusBadge variant="danger">{t('volumes.unmounted')}</StatusBadge>,
            },
            { title: t('common.createdAt'), dataIndex: 'createdAt', width: 180, render: (s: number) => new Date(s * 1000).toLocaleString() },
            {
              title: t('common.actions'), width: 200, align: 'right',
              render: (_: any, r) => (
                <Space size={4}>
                  <Tooltip title={t('volumes.copyMountHint')}>
                    <Button size="small" icon={<CopyOutlined />} onClick={() => copyMount(r)}>{t('volumes.copyMount')}</Button>
                  </Tooltip>
                  <Popconfirm title={t('volumes.confirmRemove', { name: r.name })} onConfirm={async () => { await volumesApi(daemonId!).remove(r.name); load() }}>
                    <Button size="small" danger>{t('common.delete')}</Button>
                  </Popconfirm>
                </Space>
              ),
            },
          ]}
        />
      </Card>

      <Modal
        title={t('volumes.createTitle')}
        open={open}
        onOk={onCreate}
        onCancel={() => setOpen(false)}
        confirmLoading={creating}
        destroyOnClose
        width={520}
      >
        <Form form={form} layout="vertical" initialValues={{ sizeGB: 5, fsType: 'ext4' }}>
          <Form.Item name="name" label={t('common.name')} rules={[{ required: true, pattern: /^[A-Za-z0-9_-]+$/, message: t('volumes.nameRule') }]}>
            <Input placeholder="mc-data" className="taps-mono" />
          </Form.Item>
          <Form.Item name="sizeGB" label={t('volumes.sizeGB')} rules={[{ required: true }]} extra={t('volumes.sizeHelp')}>
            <InputNumber min={1} max={2048} style={{ width: '100%' }} />
          </Form.Item>
          <Form.Item name="fsType" label={t('volumes.fs')}>
            <Select options={[{ label: 'ext4 (推荐)', value: 'ext4' }, { label: 'xfs', value: 'xfs' }]} />
          </Form.Item>
        </Form>
      </Modal>
    </>
  )
}
