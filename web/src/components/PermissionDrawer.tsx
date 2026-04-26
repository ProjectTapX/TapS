import { useEffect, useState } from 'react'
import { Button, Drawer, Table, Space, Select, Popconfirm, App, Tag, Alert, Typography } from 'antd'
import { useTranslation } from 'react-i18next'
import { permsApi, type PermissionRow } from '@/api/tasks'
import { api } from '@/api/client'
import { formatApiError } from '@/api/errors'

interface Props {
  open: boolean
  onClose: () => void
  daemonId: number
  uuid: string
  instanceName: string
}

interface UserRow { id: number; username: string; role: string }

export default function PermissionDrawer({ open, onClose, daemonId, uuid, instanceName }: Props) {
  const { t } = useTranslation()
  const { message } = App.useApp()
  const [perms, setPerms] = useState<PermissionRow[]>([])
  const [users, setUsers] = useState<UserRow[]>([])
  const [picked, setPicked] = useState<number | null>(null)

  const load = async () => {
    if (!open) return
    const [p, us] = await Promise.all([
      permsApi.list({ daemonId, uuid }),
      api.get<UserRow[]>('/users').then(r => r.data).catch(() => []),
    ])
    setPerms(p); setUsers(us)
  }
  useEffect(() => { load() }, [open, daemonId, uuid])

  const remaining = users.filter(u => u.role !== 'admin' && !perms.some(p => p.userId === u.id))

  const onGrant = async () => {
    if (!picked) return
    try {
      await permsApi.grant({ userId: picked, daemonId, uuid })
      setPicked(null); load(); message.success(t('permission.granted'))
    } catch (e: any) { message.error(formatApiError(e, 'common.error')) }
  }

  return (
    <Drawer
      open={open}
      onClose={onClose}
      title={`${t('permission.drawerTitle')} · ${instanceName}`}
      width={520}
      destroyOnClose
    >
      <Alert type="info" showIcon style={{ marginBottom: 16 }} message={t('permission.note')} />

      <Space.Compact style={{ width: '100%', marginBottom: 16 }}>
        <Select
          style={{ flex: 1 }}
          value={picked ?? undefined}
          onChange={setPicked}
          placeholder={t('permission.pickUser')}
          options={remaining.map(u => ({ label: `${u.username} (${u.role})`, value: u.id }))}
        />
        <Button type="primary" disabled={!picked} onClick={onGrant}>{t('permission.grant')}</Button>
      </Space.Compact>

      <Typography.Title level={5}>{t('permission.granted')}</Typography.Title>
      <Table<PermissionRow>
        rowKey={(r) => `${r.userId}`}
        size="small"
        dataSource={perms}
        pagination={false}
        columns={[
          { title: t('user.username'), render: (_, r) => <Tag color="blue">{r.username}</Tag> },
          {
            title: t('common.actions'), width: 100,
            render: (_, r) => (
              <Popconfirm title={t('permission.confirmRevoke')} onConfirm={async () => {
                await permsApi.revoke({ userId: r.userId, daemonId: r.daemonId, uuid: r.uuid })
                load()
              }}>
                <Button size="small" danger>{t('permission.revoke')}</Button>
              </Popconfirm>
            ),
          },
        ]}
      />
    </Drawer>
  )
}
