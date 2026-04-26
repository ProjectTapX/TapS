import { useEffect, useState } from 'react'
import { Button, Table, Space, Modal, Form, Input, InputNumber, Popconfirm, App, Card, Tag, Alert, Typography } from 'antd'
import { ReloadOutlined, PlusOutlined, DeleteOutlined, SafetyCertificateOutlined } from '@ant-design/icons'
import { useTranslation } from 'react-i18next'
import { daemonsApi, type DaemonView } from '@/api/resources'
import PageHeader from '@/components/PageHeader'
import StatusBadge from '@/components/StatusBadge'
import { formatApiError } from '@/api/errors'

export default function NodesPage() {
  const { t } = useTranslation()
  const { message } = App.useApp()
  const [data, setData] = useState<DaemonView[]>([])
  const [loading, setLoading] = useState(false)
  const [open, setOpen] = useState(false)
  const [editing, setEditing] = useState<DaemonView | null>(null)
  const [form] = Form.useForm()
  // Fingerprint that the operator has confirmed for this form session.
  // Cleared when the modal opens; populated by Probe / Refetch buttons
  // and by the existing daemon's stored value when editing.
  const [pinnedFp, setPinnedFp] = useState<string>('')
  // Result of the last probe — shown in an Alert so the operator can
  // visually confirm before clicking Save.
  const [probedFp, setProbedFp] = useState<string>('')
  const [probing, setProbing] = useState(false)

  const load = async () => {
    setLoading(true)
    try { setData(await daemonsApi.list()) } finally { setLoading(false) }
  }
  useEffect(() => { load(); const t = setInterval(load, 5000); return () => clearInterval(t) }, [])

  const openAdd = () => {
    setEditing(null)
    form.resetFields()
    setPinnedFp('')
    setProbedFp('')
    setOpen(true)
  }

  const openEdit = (r: DaemonView) => {
    setEditing(r)
    form.setFieldsValue({
      name: r.name, address: r.address, displayHost: r.displayHost,
      portMin: r.portMin || undefined, portMax: r.portMax || undefined,
    })
    setPinnedFp(r.certFingerprint || '')
    setProbedFp('')
    setOpen(true)
  }

  const probe = async () => {
    const addr = form.getFieldValue('address')
    if (!addr) {
      message.warning(t('node.probeNeedAddress'))
      return
    }
    setProbing(true)
    try {
      const r = await daemonsApi.probeFingerprint(addr)
      setProbedFp(r.fingerprint)
    } catch (e: any) {
      message.error(formatApiError(e, 'common.error'))
    } finally { setProbing(false) }
  }

  const acceptProbed = () => {
    if (probedFp) setPinnedFp(probedFp)
  }

  const onSubmit = async () => {
    const v = await form.validateFields()
    if (!pinnedFp) {
      message.error(t('node.fpRequired'))
      return
    }
    try {
      const body = { ...v, certFingerprint: pinnedFp }
      if (editing) await daemonsApi.update(editing.id, body)
      else await daemonsApi.create(body)
      message.success(t('common.success'))
      setOpen(false); setEditing(null); form.resetFields(); setPinnedFp(''); setProbedFp(''); load()
    } catch (e: any) {
      message.error(formatApiError(e, 'common.error'))
    }
  }

  return (
    <>
      <PageHeader
        title={t('menu.nodes')}
        subtitle={t('node.pageSubtitle')}
        extra={
          <>
            <Button icon={<ReloadOutlined />} onClick={load}>{t('common.refresh')}</Button>
            <Button type="primary" icon={<PlusOutlined />} onClick={openAdd}>
              {t('node.add')}
            </Button>
          </>
        }
      />
      <Card bodyStyle={{ padding: 0 }}>
        <Table<DaemonView>
          rowKey="id"
          loading={loading}
          dataSource={data}
          pagination={false}
          columns={[
            {
              title: t('common.name'),
              render: (_, r) => (
                <Space direction="vertical" size={0}>
                  <span style={{ fontWeight: 500 }}>{r.name}</span>
                  <span className="taps-mono" style={{ color: 'var(--taps-text-muted)', fontSize: 11 }}>{r.address}</span>
                </Space>
              ),
            },
            {
              title: t('instance.status_'), width: 130,
              render: (_, r) => r.connected
                ? <StatusBadge variant="success">{t('node.connected')}</StatusBadge>
                : <StatusBadge variant="danger">{t('node.offline')}</StatusBadge>,
            },
            { title: t('node.os'), width: 140, render: (_, r) => r.connected ? <Tag bordered={false}>{r.os}/{r.arch}</Tag> : '—' },
            { title: t('node.version'), width: 110, dataIndex: 'daemonVersion', render: (v) => v ? <span className="taps-mono">{v}</span> : '—' },
            {
              title: t('common.actions'), width: 200, align: 'right',
              render: (_, r) => (
                <Space size={4}>
                  <Button size="small" onClick={() => openEdit(r)}>{t('common.edit')}</Button>
                  <Popconfirm title={t('node.confirmRemove')} onConfirm={async () => { await daemonsApi.remove(r.id); message.success(t('common.success')); load() }}>
                    <Button size="small" danger icon={<DeleteOutlined />} />
                  </Popconfirm>
                </Space>
              ),
            },
          ]}
        />
      </Card>
      <Modal title={editing ? t('node.edit') : t('node.add')} open={open} onCancel={() => { setOpen(false); setEditing(null) }} onOk={onSubmit} destroyOnClose width={600}>
        <Form form={form} layout="vertical">
          <Form.Item name="name" label={t('common.name')} rules={[{ required: true }]}><Input /></Form.Item>
          <Form.Item name="address" label={`${t('node.address')} (host:port)`} rules={[{ required: true }]} extra={t('node.addressHelp')}><Input placeholder="127.0.0.1:24445" /></Form.Item>
          <Form.Item name="displayHost" label={t('node.displayHost')} extra={t('node.displayHostHelp')}>
            <Input placeholder="play.example.com" />
          </Form.Item>
          <Space.Compact style={{ width: '100%' }}>
            <Form.Item name="portMin" label={t('node.portMin')} extra={t('node.portRangeHelp')} style={{ flex: 1, marginRight: 8 }}>
              <InputNumber min={1024} max={65535} placeholder="25565" style={{ width: '100%' }} />
            </Form.Item>
            <Form.Item name="portMax" label={t('node.portMax')} style={{ flex: 1 }}>
              <InputNumber min={1024} max={65535} placeholder="25600" style={{ width: '100%' }} />
            </Form.Item>
          </Space.Compact>
          <Form.Item name="token" label={editing ? `${t('node.token')}（${t('node.tokenKeepHelp')}）` : t('node.token')} rules={[{ required: !editing }]}><Input.Password /></Form.Item>

          {/* TLS fingerprint pin — TOFU. The single Probe button always
              uses the address currently in the form, so admins editing
              an address can see the new daemon's fingerprint without
              saving first. Operator clicks "Accept" to commit. */}
          <Card size="small" type="inner" title={<Space><SafetyCertificateOutlined />{t('node.fpTitle')}</Space>} style={{ marginBottom: 12 }}>
            <Alert type="info" showIcon style={{ marginBottom: 12 }} message={t('node.fpDesc')} />
            <Space style={{ marginBottom: 8 }}>
              <Button onClick={probe} loading={probing}>{t('node.fpProbe')}</Button>
            </Space>
            {probedFp && (
              <Alert
                type={probedFp === pinnedFp ? 'success' : 'warning'}
                showIcon
                style={{ marginBottom: 8 }}
                message={
                  <Space direction="vertical" size={2}>
                    <span>{probedFp === pinnedFp ? t('node.fpMatchesPinned') : t('node.fpDiffersFromPinned')}</span>
                    <Typography.Text code copyable style={{ fontSize: 11, wordBreak: 'break-all' }}>{probedFp}</Typography.Text>
                  </Space>
                }
                action={probedFp !== pinnedFp ? <Button size="small" type="primary" onClick={acceptProbed}>{t('node.fpAccept')}</Button> : undefined}
              />
            )}
            <div>
              <Typography.Text type="secondary" style={{ fontSize: 12 }}>{t('node.fpPinned')}：</Typography.Text>
              {pinnedFp
                ? <Typography.Text code copyable style={{ fontSize: 11, wordBreak: 'break-all' }}>{pinnedFp}</Typography.Text>
                : <Typography.Text type="danger" style={{ fontSize: 12 }}>{t('node.fpNone')}</Typography.Text>}
            </div>
          </Card>
        </Form>
      </Modal>
    </>
  )
}
