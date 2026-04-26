import { useEffect, useState } from 'react'
import { Button, Table, Modal, Form, Input, Select, Popconfirm, Space, App, Card, Tooltip, Drawer, Tag } from 'antd'
import { PlusOutlined, ReloadOutlined, DeleteOutlined, SafetyOutlined, HistoryOutlined } from '@ant-design/icons'
import { useTranslation } from 'react-i18next'
import { api } from '@/api/client'
import PageHeader from '@/components/PageHeader'
import StatusBadge from '@/components/StatusBadge'
import UserPermissionDrawer from '@/components/UserPermissionDrawer'
import { formatApiError } from '@/api/errors'

interface User {
  id: number
  username: string
  email?: string
  role: 'admin' | 'user' | 'guest'
  createdAt: string
}

export default function UsersPage() {
  const { t } = useTranslation()
  const { message } = App.useApp()
  const [data, setData] = useState<User[]>([])
  const [loading, setLoading] = useState(false)
  const [open, setOpen] = useState(false)
  const [editing, setEditing] = useState<User | null>(null)
  const [permTarget, setPermTarget] = useState<User | null>(null)
  const [loginsTarget, setLoginsTarget] = useState<User | null>(null)
  const [form] = Form.useForm()

  const load = async () => {
    setLoading(true)
    try {
      const r = await api.get<User[]>('/users')
      setData(r.data)
    } finally {
      setLoading(false)
    }
  }
  useEffect(() => { load() }, [])

  const onSubmit = async () => {
    const v = await form.validateFields()
    try {
      if (editing) {
        await api.put(`/users/${editing.id}`, v)
        message.success(t('common.success'))
      } else {
        await api.post('/users', v)
        message.success(t('common.success'))
      }
      setOpen(false); setEditing(null); form.resetFields(); load()
    } catch (e: any) {
      message.error(formatApiError(e, 'common.error'))
    }
  }

  const onDelete = async (id: number) => {
    try {
      await api.delete(`/users/${id}`)
      message.success(t('common.success')); load()
    } catch (e: any) {
      message.error(formatApiError(e, 'common.error'))
    }
  }

  return (
    <>
      <PageHeader
        title={t('menu.users')}
        extra={
          <>
            <Button icon={<ReloadOutlined />} onClick={load}>{t('common.refresh')}</Button>
            <Button type="primary" icon={<PlusOutlined />} onClick={() => { setEditing(null); form.resetFields(); setOpen(true) }}>
              {t('user.new')}
            </Button>
          </>
        }
      />
      <Card bodyStyle={{ padding: 0 }}>
        <Table<User>
          rowKey="id"
          loading={loading}
          dataSource={data}
          pagination={false}
          columns={[
            { title: t('common.id'), dataIndex: 'id', width: 80 },
            { title: t('user.username'), dataIndex: 'username', render: (v) => <span style={{ fontWeight: 500 }}>{v}</span> },
            { title: t('user.email'), dataIndex: 'email', render: (v: string) => v ? v : <span style={{ color: 'var(--taps-text-muted)' }}>—</span> },
            {
              title: t('user.role'), width: 120, dataIndex: 'role',
              render: (r: string) => <StatusBadge variant={r === 'admin' ? 'danger' : r === 'user' ? 'info' : 'neutral'}>{r}</StatusBadge>,
            },
            { title: t('common.createdAt'), dataIndex: 'createdAt', render: (s: string) => new Date(s).toLocaleString() },
            {
              title: t('common.actions'), width: 240, align: 'right',
              render: (_: any, r) => (
                <Space size={4}>
                  <Tooltip title={t('userPerm.button')}>
                    <Button size="small" icon={<SafetyOutlined />} onClick={() => setPermTarget(r)} disabled={r.role === 'admin'}>
                      {t('userPerm.button')}
                    </Button>
                  </Tooltip>
                  <Tooltip title={t('user.loginsTitle', { username: r.username })}>
                    <Button size="small" icon={<HistoryOutlined />} onClick={() => setLoginsTarget(r)}>
                      {t('user.logins')}
                    </Button>
                  </Tooltip>
                  <Button size="small" onClick={() => { setEditing(r); form.setFieldsValue({ username: r.username, email: r.email ?? '', role: r.role }); setOpen(true) }}>{t('common.edit')}</Button>
                  <Popconfirm title={t('common.confirmDelete')} onConfirm={() => onDelete(r.id)}>
                    <Button size="small" danger icon={<DeleteOutlined />} />
                  </Popconfirm>
                </Space>
              ),
            },
          ]}
        />
      </Card>
      <Modal
        title={editing ? t('user.edit') : t('user.new')}
        open={open}
        onCancel={() => { setOpen(false); setEditing(null) }}
        onOk={onSubmit}
        destroyOnClose
      >
        <Form form={form} layout="vertical">
          <Form.Item name="username" label={t('user.username')} rules={[{ required: !editing }]}>
            <Input disabled={!!editing} />
          </Form.Item>
          <Form.Item
            name="email"
            label={t('user.email')}
            rules={[{ type: 'email', message: t('user.emailInvalid') }]}
            extra={t('user.emailHelp')}
          >
            <Input placeholder="user@example.com" allowClear />
          </Form.Item>
          <Form.Item name="password" label={editing ? t('user.newPassword') : t('user.password')} rules={[{ required: !editing }]}>
            <Input.Password />
          </Form.Item>
          <Form.Item name="role" label={t('user.role')} initialValue="user">
            <Select options={[
              { label: 'admin', value: 'admin' },
              { label: 'user', value: 'user' },
              { label: 'guest', value: 'guest' },
            ]} />
          </Form.Item>
        </Form>
      </Modal>
      {permTarget && (
        <UserPermissionDrawer
          open={!!permTarget}
          onClose={() => setPermTarget(null)}
          userId={permTarget.id}
          username={permTarget.username}
          role={permTarget.role}
        />
      )}
      {loginsTarget && (
        <LoginHistoryDrawer
          open={!!loginsTarget}
          onClose={() => setLoginsTarget(null)}
          userId={loginsTarget.id}
          username={loginsTarget.username}
        />
      )}
    </>
  )
}

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

function LoginHistoryDrawer({ open, onClose, userId, username }: { open: boolean; onClose: () => void; userId: number; username: string }) {
  const { t } = useTranslation()
  const [rows, setRows] = useState<LoginRow[]>([])
  const [total, setTotal] = useState(0)
  const [page, setPage] = useState(1)
  const [loading, setLoading] = useState(false)
  const pageSize = 20

  // Server-side pagination — no upper limit on what the user can scroll
  // through. Each page is one round-trip; the daemon's login_logs table
  // is indexed on user_id so OFFSET stays cheap until the table grows
  // very large.
  const load = async () => {
    setLoading(true)
    try {
      const r = await api.get<{ items: LoginRow[]; total: number }>('/logins', {
        params: { userId, limit: pageSize, offset: (page - 1) * pageSize },
      })
      setRows(r.data.items ?? [])
      setTotal(r.data.total ?? 0)
    } finally { setLoading(false) }
  }
  useEffect(() => { if (open) load() }, [open, userId, page])
  // Reset page when the drawer reopens for a different user.
  useEffect(() => { if (open) setPage(1) }, [open, userId])
  return (
    <Drawer
      open={open}
      onClose={onClose}
      width={780}
      title={t('user.loginsTitle', { username })}
      extra={<Button size="small" icon={<ReloadOutlined />} onClick={load}>{t('common.refresh')}</Button>}
      destroyOnClose
    >
      <Table<LoginRow>
        rowKey="id"
        loading={loading}
        dataSource={rows}
        size="small"
        pagination={{ current: page, pageSize, total, onChange: setPage, showSizeChanger: false, hideOnSinglePage: true }}
        columns={[
          {
            title: t('common.status'), dataIndex: 'success', width: 70,
            render: (s: boolean) => s
              ? <Tag color="success">{t('user.loginOk')}</Tag>
              : <Tag color="error">{t('user.loginFail')}</Tag>,
          },
          { title: t('common.time'), dataIndex: 'time', width: 170, render: (s: string) => new Date(s).toLocaleString() },
          { title: 'IP', dataIndex: 'ip', width: 130, render: (v) => <code className="taps-mono">{v}</code> },
          { title: t('user.userAgent'), dataIndex: 'userAgent', ellipsis: true,
            render: (v: string) => <Tooltip title={v}><span style={{ fontSize: 12, color: 'var(--taps-text-muted)' }}>{summarizeUA(v)}</span></Tooltip>,
          },
          { title: t('user.failReason'), dataIndex: 'reason', width: 130, render: (v: string) => v ? <span style={{ color: 'var(--taps-text-muted)' }}>{v}</span> : '—' },
        ]}
      />
    </Drawer>
  )
}

// Pull a friendly browser/OS hint out of a User-Agent string. Cheap
// heuristic — we don't want a UA-parser dep just for a tooltip summary.
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
