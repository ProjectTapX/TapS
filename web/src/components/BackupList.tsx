import { useEffect, useState } from 'react'
import { Button, Table, Space, Modal, Input, Popconfirm, App, Tag, Empty } from 'antd'
import { ReloadOutlined, SaveOutlined, RollbackOutlined, DownloadOutlined } from '@ant-design/icons'
import { useTranslation } from 'react-i18next'
import { backupApi, type BackupEntry } from '@/api/fs'
import { useAuthStore } from '@/stores/auth'
import { formatApiError } from '@/api/errors'

interface Props { daemonId: number; uuid: string }

function fmtSize(n: number) {
  if (n < 1024) return n + ' B'
  if (n < 1024 ** 2) return (n / 1024).toFixed(1) + ' KB'
  if (n < 1024 ** 3) return (n / 1024 ** 2).toFixed(1) + ' MB'
  return (n / 1024 ** 3).toFixed(2) + ' GB'
}

export default function BackupList({ daemonId, uuid }: Props) {
  const { t } = useTranslation()
  const { message } = App.useApp()
  const api = backupApi(daemonId, uuid)
  const [data, setData] = useState<BackupEntry[]>([])
  const [loading, setLoading] = useState(false)
  const [open, setOpen] = useState(false)
  const [note, setNote] = useState('')
  const [creating, setCreating] = useState(false)

  const load = async () => {
    setLoading(true)
    try { setData(await api.list()) } finally { setLoading(false) }
  }
  useEffect(() => { load() }, [daemonId, uuid])

  const onCreate = async () => {
    setCreating(true)
    const hide = message.loading(t('backup.creating'), 0)
    try {
      await api.create(note)
      hide(); message.success(t('common.success'))
      setOpen(false); setNote(''); load()
    } catch (e: any) {
      hide(); message.error(formatApiError(e, 'common.error'))
    } finally { setCreating(false) }
  }

  const onRestore = (e: BackupEntry) => {
    Modal.confirm({
      title: t('backup.confirmRestore', { name: e.name }),
      content: t('backup.restoreNote'),
      okButtonProps: { danger: true },
      onOk: async () => {
        const hide = message.loading(t('backup.restoring'), 0)
        try { await api.restore(e.name); hide(); message.success(t('common.success')) }
        catch (err: any) { hide(); message.error(formatApiError(err, 'common.error')) }
      },
    })
  }

  return (
    <>
      <Space style={{ marginBottom: 12 }}>
        <Button type="primary" icon={<SaveOutlined />} onClick={() => setOpen(true)}>{t('backup.new')}</Button>
        <Button icon={<ReloadOutlined />} onClick={load}>{t('common.refresh')}</Button>
      </Space>
      <Table<BackupEntry>
        rowKey="name"
        loading={loading}
        dataSource={data}
        size="middle"
        pagination={false}
        locale={{ emptyText: <Empty description={t('backup.empty')} /> }}
        columns={[
          { title: t('backup.name'), dataIndex: 'name', render: (v) => <code>{v}</code> },
          { title: t('files.size'), dataIndex: 'size', render: (v: number) => fmtSize(v), width: 120 },
          { title: t('backup.created'), dataIndex: 'created', render: (s: number) => new Date(s * 1000).toLocaleString(), width: 200 },
          {
            title: t('common.actions'), width: 280,
            render: (_, r) => {
              const token = useAuthStore.getState().token ?? ''
              return (
                <Space>
                  <Button size="small" icon={<DownloadOutlined />} href={api.downloadUrl(r.name, token)}>
                    {t('files.download')}
                  </Button>
                  <Button size="small" icon={<RollbackOutlined />} onClick={() => onRestore(r)}>{t('backup.restore')}</Button>
                  <Popconfirm title={t('backup.confirmDelete')} onConfirm={async () => { await api.remove(r.name); load() }}>
                    <Button size="small" danger>{t('common.delete')}</Button>
                  </Popconfirm>
                </Space>
              )
            },
          },
        ]}
      />
      <Modal
        open={open}
        title={t('backup.new')}
        onCancel={() => setOpen(false)}
        onOk={onCreate}
        confirmLoading={creating}
        destroyOnClose
      >
        <p style={{ color: '#888' }}>{t('backup.note')}</p>
        <Input value={note} onChange={(e) => setNote(e.target.value)} placeholder="pre-update" />
        <div style={{ marginTop: 12 }}>
          <Tag>{t('backup.zipsTo')}: <code>backups/&lt;uuid&gt;/timestamp[-note].zip</code></Tag>
        </div>
      </Modal>
    </>
  )
}
