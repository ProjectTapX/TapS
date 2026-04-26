import { useEffect, useState } from 'react'
import { Card, Table, Button, Modal, Form, Input, Select, Space, Tag, Popconfirm, App } from 'antd'
import { PlusOutlined, EditOutlined, DeleteOutlined, ReloadOutlined } from '@ant-design/icons'
import { useTranslation } from 'react-i18next'
import { groupsApi, type NodeGroup } from '@/api/groups'
import { daemonsApi, type DaemonView } from '@/api/resources'
import PageHeader from '@/components/PageHeader'
import { formatApiError } from '@/api/errors'

export default function GroupsPage() {
  const { t } = useTranslation()
  const { message } = App.useApp()
  const [groups, setGroups] = useState<NodeGroup[]>([])
  const [daemons, setDaemons] = useState<DaemonView[]>([])
  const [loading, setLoading] = useState(false)
  const [open, setOpen] = useState(false)
  const [editing, setEditing] = useState<NodeGroup | null>(null)
  const [form] = Form.useForm<{ name: string; daemonIds: number[] }>()

  const load = async () => {
    setLoading(true)
    try {
      const [g, d] = await Promise.all([groupsApi.list(), daemonsApi.list()])
      setGroups(g); setDaemons(d)
    } catch (e: any) {
      message.error(formatApiError(e, 'common.error'))
    } finally { setLoading(false) }
  }
  useEffect(() => { load() }, [])

  const onCreate = () => {
    setEditing(null)
    form.resetFields()
    setOpen(true)
  }
  const onEdit = (g: NodeGroup) => {
    setEditing(g)
    form.setFieldsValue({ name: g.name, daemonIds: g.daemonIds ?? [] })
    setOpen(true)
  }
  const onSave = async () => {
    const v = await form.validateFields()
    try {
      if (editing) await groupsApi.update(editing.id, v.name, v.daemonIds ?? [])
      else await groupsApi.create(v.name, v.daemonIds ?? [])
      message.success(t('common.success'))
      setOpen(false); load()
    } catch (e: any) {
      message.error(formatApiError(e, 'common.error'))
    }
  }

  return (
    <>
      <PageHeader
        title={t('menu.groups')}
        subtitle={t('groups.pageSubtitle')}
        extra={
          <>
            <Button icon={<ReloadOutlined />} onClick={load}>{t('common.refresh')}</Button>
            <Button type="primary" icon={<PlusOutlined />} onClick={onCreate}>{t('groups.new')}</Button>
          </>
        }
      />
      <Card bodyStyle={{ padding: 0 }}>
        <Table<NodeGroup>
          rowKey="id"
          loading={loading}
          dataSource={groups}
          pagination={{ pageSize: 10, hideOnSinglePage: true }}
          columns={[
            { title: t('groups.name'), dataIndex: 'name' },
            {
              title: t('groups.members'),
              render: (_, g) => g.daemonIds?.length
                ? <Space wrap size={4}>{g.daemonIds.map(id => {
                    const d = daemons.find(x => x.id === id)
                    return <Tag key={id} bordered={false}>{d?.name ?? `#${id}`}</Tag>
                  })}</Space>
                : <span style={{ color: 'var(--taps-text-muted)' }}>{t('groups.empty')}</span>,
            },
            {
              title: t('common.actions'), width: 160, align: 'right',
              render: (_, g) => (
                <Space size={4}>
                  <Button size="small" icon={<EditOutlined />} onClick={() => onEdit(g)}>{t('common.edit')}</Button>
                  <Popconfirm title={t('common.confirmDelete')} onConfirm={async () => {
                    try { await groupsApi.remove(g.id); load() } catch (e: any) { message.error(formatApiError(e, 'common.error')) }
                  }}>
                    <Button size="small" danger icon={<DeleteOutlined />} />
                  </Popconfirm>
                </Space>
              ),
            },
          ]}
        />
      </Card>

      <Modal title={editing ? t('groups.edit') : t('groups.new')} open={open}
        onCancel={() => setOpen(false)} onOk={onSave} destroyOnClose>
        <Form form={form} layout="vertical">
          <Form.Item name="name" label={t('groups.name')} rules={[{ required: true, max: 64 }]}>
            <Input placeholder="mc-survival" />
          </Form.Item>
          <Form.Item name="daemonIds" label={t('groups.members')} extra={t('groups.membersHelp')}>
            <Select mode="multiple" allowClear placeholder={t('groups.pickNodes')}
              options={daemons.map(d => ({ label: d.name, value: d.id }))} />
          </Form.Item>
        </Form>
      </Modal>
    </>
  )
}
