import { useEffect, useState } from 'react'
import { Button, Table, Space, Modal, Form, Input, Select, Popconfirm, App, Alert, Tag, Empty, Card, Tabs, Tooltip, Typography } from 'antd'
import { DownloadOutlined, ReloadOutlined, EditOutlined } from '@ant-design/icons'
import { useTranslation } from 'react-i18next'
import { daemonsApi, type DaemonView } from '@/api/resources'
import { dockerApi, type DockerImage } from '@/api/docker'
import PageHeader from '@/components/PageHeader'
import PullProgressModal from '@/components/PullProgressModal'
import { PRESET_GROUPS, type ImagePreset } from '@/data/imagePresets'
import { formatApiError } from '@/api/errors'

function fmtBytes(n: number) {
  if (!n) return '-'
  const u = ['B', 'KB', 'MB', 'GB', 'TB']; let i = 0
  while (n >= 1024 && i < u.length - 1) { n /= 1024; i++ }
  return `${n.toFixed(1)} ${u[i]}`
}

function shortRepo(repo: string) {
  const i = repo.lastIndexOf('/')
  return i >= 0 ? repo.slice(i + 1) : repo
}

export default function ImagesPage() {
  const { t } = useTranslation()
  const { message } = App.useApp()
  const [daemons, setDaemons] = useState<DaemonView[]>([])
  const [daemonId, setDaemonId] = useState<number | null>(null)
  const [available, setAvailable] = useState<boolean>(true)
  const [errMsg, setErrMsg] = useState<string>('')
  const [data, setData] = useState<DockerImage[]>([])
  const [loading, setLoading] = useState(false)
  const [pulling, setPulling] = useState<string | null>(null)
  const [open, setOpen] = useState(false)
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
      const r = await dockerApi(daemonId).images()
      setAvailable(r.available); setErrMsg(r.error ?? ''); setData(r.images ?? [])
    } catch (e: any) {
      setAvailable(false); setErrMsg(formatApiError(e, ''))
    } finally { setLoading(false) }
  }
  useEffect(() => { load() }, [daemonId])

  const pulledRefs = new Set(data.map(d => `${d.repository}:${d.tag}`))

  const startPull = (image: string) => {
    setOpen(false)
    setPulling(image)
  }

  const onPullCustom = async () => {
    const v = await form.validateFields()
    startPull(v.image)
  }

  const pickPreset = (p: ImagePreset) => {
    form.setFieldValue('image', p.ref)
  }

  return (
    <>
      <PageHeader
        title={t('menu.images')}
        subtitle={t('images.pageSubtitle')}
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
            <Button type="primary" icon={<DownloadOutlined />} onClick={() => setOpen(true)} disabled={!daemonId || !available || !!pulling}>
              {t('images.pull')}
            </Button>
          </>
        }
      />

      {!available && (
        <Alert type="warning" showIcon style={{ marginBottom: 16 }}
          message={t('images.notAvailable')}
          description={errMsg || t('images.notAvailableDesc')} />
      )}

      <Card bodyStyle={{ padding: 0 }}>
        <Table<DockerImage>
          rowKey="id"
          loading={loading}
          dataSource={data}
          locale={{ emptyText: <Empty description={t('images.empty')} /> }}
          pagination={false}
          columns={[
            { title: t('images.displayName'), dataIndex: 'displayName',
              render: (v: string, r: DockerImage) => {
                const name = v || shortRepo(r.repository)
                const desc = r.description
                const ref = `${r.repository}:${r.tag}`
                return (
                  <Space size={4}>
                    {desc
                      ? <Tooltip title={desc}><span style={{ fontWeight: 500 }}>{name}</span></Tooltip>
                      : <span style={{ fontWeight: 500 }}>{name}</span>
                    }
                    <Typography.Link
                      style={{ fontSize: 11 }}
                      onClick={async () => {
                        const { value } = await new Promise<{ value: string }>((resolve) => {
                          let val = v || ''
                          Modal.confirm({
                            title: t('images.editDisplayName'),
                            content: <Input defaultValue={val} onChange={(e) => { val = e.target.value }} placeholder={shortRepo(r.repository)} />,
                            okText: t('common.save'),
                            cancelText: t('common.cancel'),
                            onOk: () => resolve({ value: val }),
                            onCancel: () => resolve({ value: v || '' }),
                          })
                        })
                        if (value !== (v || '')) {
                          try {
                            await dockerApi(daemonId!).setAlias(ref, value.trim())
                            message.success(t('common.success'))
                            load()
                          } catch (e: any) { message.error(formatApiError(e, 'common.error')) }
                        }
                      }}
                    >
                      <EditOutlined />
                    </Typography.Link>
                  </Space>
                )
              },
            },
            { title: t('images.repo'), dataIndex: 'repository', render: (v: string) => <code className="taps-mono">{v}</code> },
            { title: t('images.tag'), dataIndex: 'tag', width: 120, render: (v) => <Tag bordered={false}>{v}</Tag> },
            { title: t('common.id'), dataIndex: 'id', ellipsis: true, render: (v: string) => <span className="taps-mono" style={{ fontSize: 11 }}>{v.slice(0, 19)}</span> },
            { title: t('images.size'), dataIndex: 'size', render: fmtBytes, width: 120 },
            { title: t('images.created'), dataIndex: 'created', render: (s: number) => s ? new Date(s * 1000).toLocaleString() : '—' },
            {
              title: t('common.delete'), width: 120, align: 'right',
              render: (_, r) => (
                <Popconfirm title={t('images.confirmRemove', { ref: `${r.repository}:${r.tag}` })} onConfirm={async () => {
                  await dockerApi(daemonId!).remove(r.id); load()
                }}>
                  <Button size="small" danger>{t('common.delete')}</Button>
                </Popconfirm>
              ),
            },
          ]}
        />
      </Card>

      <Modal
        title={t('images.pull')}
        open={open}
        onOk={onPullCustom}
        okText={t('images.pull')}
        confirmLoading={!!pulling}
        onCancel={() => setOpen(false)}
        destroyOnClose
        width={720}
      >
        <Tabs
          defaultActiveKey={PRESET_GROUPS[0].id}
          items={PRESET_GROUPS.map(g => ({
            key: g.id,
            label: t(g.titleKey),
            children: (
              <>
                {g.hintKey && <div style={{ color: 'var(--taps-text-muted)', fontSize: 12, marginBottom: 12 }}>{t(g.hintKey)}</div>}
                <Space wrap size={[8, 8]}>
                  {g.items.map(p => {
                    const already = pulledRefs.has(p.ref)
                    return (
                      <Tooltip
                        key={p.ref}
                        title={
                          <div>
                            <div><code style={{ color: '#7dd3fc' }}>{p.ref}</code></div>
                            {p.descKey && <div style={{ marginTop: 4 }}>{t(p.descKey)}</div>}
                            {p.size && <div style={{ marginTop: 4, opacity: 0.7 }}>~{p.size}</div>}
                          </div>
                        }
                      >
                        <button
                          type="button"
                          onClick={() => pickPreset(p)}
                          style={{
                            cursor: 'pointer',
                            padding: '8px 14px',
                            borderRadius: 10,
                            border: '1px solid var(--taps-border)',
                            background: already ? 'rgba(16, 185, 129, 0.08)' : 'transparent',
                            color: 'inherit',
                            display: 'flex',
                            alignItems: 'center',
                            gap: 8,
                            fontFamily: 'inherit',
                            fontSize: 13,
                            transition: 'all .12s',
                          }}
                          onMouseEnter={(e) => { e.currentTarget.style.borderColor = 'var(--taps-primary)'; e.currentTarget.style.background = 'var(--taps-primary-soft)' }}
                          onMouseLeave={(e) => { e.currentTarget.style.borderColor = 'var(--taps-border)'; e.currentTarget.style.background = already ? 'rgba(16, 185, 129, 0.08)' : 'transparent' }}
                        >
                          <span style={{ fontWeight: 600 }}>{t(p.labelKey)}</span>
                          {p.size && <span style={{ color: 'var(--taps-text-muted)', fontSize: 11 }}>{p.size}</span>}
                          {already && <span style={{ color: '#10b981', fontSize: 11 }}>✓</span>}
                        </button>
                      </Tooltip>
                    )
                  })}
                </Space>
              </>
            ),
          }))}
        />

        <Form form={form} layout="vertical" style={{ marginTop: 8 }}>
          <Form.Item
            name="image"
            label={t('images.imageField')}
            extra={t('images.imageHelp2')}
            rules={[{ required: true }]}
          >
            <Input placeholder={t('images.imagePh')} className="taps-mono" />
          </Form.Item>
        </Form>
      </Modal>

      {pulling && (
        <PullProgressModal
          daemonId={daemonId!}
          image={pulling}
          open={!!pulling}
          onClose={() => { setPulling(null); load() }}
          onSuccess={() => { /* keep modal open until user dismisses */ }}
        />
      )}
    </>
  )
}
