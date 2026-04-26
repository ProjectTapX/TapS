import { useEffect, useState } from 'react'
import { Button, Table, Space, Modal, Form, Input, Select, Switch, Popconfirm, Tag, App } from 'antd'
import { useTranslation } from 'react-i18next'
import { tasksApi, type ScheduledTask } from '@/api/tasks'
import { formatApiError } from '@/api/errors'

interface Props { daemonId: number; uuid: string }

export default function TaskList({ daemonId, uuid }: Props) {
  const { t } = useTranslation()
  const { message } = App.useApp()
  const tasks = tasksApi(daemonId, uuid)
  const [data, setData] = useState<ScheduledTask[]>([])
  const [loading, setLoading] = useState(false)
  const [open, setOpen] = useState(false)
  const [editing, setEditing] = useState<ScheduledTask | null>(null)
  const [form] = Form.useForm()

  const ACTIONS = [
    { label: t('task.actions.command'), value: 'command' },
    { label: t('task.actions.start'), value: 'start' },
    { label: t('task.actions.stop'), value: 'stop' },
    { label: t('task.actions.restart'), value: 'restart' },
    { label: t('task.actions.backup'), value: 'backup' },
  ]

  const load = async () => {
    setLoading(true)
    try { setData(await tasks.list()) } finally { setLoading(false) }
  }
  useEffect(() => { load() }, [daemonId, uuid])

  const onSubmit = async () => {
    const v = await form.validateFields()
    try {
      if (editing) await tasks.update(editing.id, v)
      else await tasks.create(v)
      message.success(t('common.success')); setOpen(false); setEditing(null); form.resetFields(); load()
    } catch (e: any) { message.error(formatApiError(e, 'common.error')) }
  }

  return (
    <>
      <Space style={{ marginBottom: 12 }}>
        <Button type="primary" onClick={() => { setEditing(null); form.resetFields(); form.setFieldsValue({ enabled: true, action: 'command' }); setOpen(true) }}>{t('task.new')}</Button>
      </Space>
      <Table<ScheduledTask>
        rowKey="id"
        loading={loading}
        dataSource={data}
        size="middle"
        pagination={false}
        columns={[
          { title: t('common.id'), dataIndex: 'id', width: 60 },
          { title: t('task.name'), dataIndex: 'name' },
          { title: t('task.cron'), dataIndex: 'cron', render: (v) => <code>{v}</code> },
          { title: t('task.action'), dataIndex: 'action', render: (v: string) => <Tag>{t(`task.actions.${v}`)}</Tag> },
          { title: t('task.data'), dataIndex: 'data', ellipsis: true },
          { title: t('common.enabled'), dataIndex: 'enabled', render: (v: boolean) => v ? <Tag color="green">{t('common.yes')}</Tag> : <Tag>{t('common.no')}</Tag> },
          { title: t('task.lastRun'), dataIndex: 'lastRun', render: (s: string) => s && new Date(s).getFullYear() > 2000 ? new Date(s).toLocaleString() : '-' },
          {
            title: t('common.actions'), width: 160,
            render: (_, r) => (
              <Space>
                <Button size="small" onClick={() => { setEditing(r); form.setFieldsValue(r); setOpen(true) }}>{t('common.edit')}</Button>
                <Popconfirm title={t('task.confirmDelete')} onConfirm={async () => { await tasks.remove(r.id); load() }}>
                  <Button size="small" danger>{t('common.delete')}</Button>
                </Popconfirm>
              </Space>
            ),
          },
        ]}
      />
      <Modal title={editing ? t('task.edit') : t('task.new')} open={open} onCancel={() => { setOpen(false); setEditing(null) }} onOk={onSubmit} destroyOnClose>
        <Form form={form} layout="vertical">
          <Form.Item name="name" label={t('task.name')}><Input /></Form.Item>
          <Form.Item name="cron" label={t('task.cron')} rules={[{ required: true }]} extra={t('task.cronHelp')}>
            <Input placeholder="*/5 * * * *" />
          </Form.Item>
          <Form.Item name="action" label={t('task.action')} rules={[{ required: true }]}>
            <Select options={ACTIONS} />
          </Form.Item>
          <Form.Item name="data" label={t('task.data')}>
            <Input placeholder="say hello" />
          </Form.Item>
          <Form.Item name="enabled" label={t('common.enabled')} valuePropName="checked">
            <Switch />
          </Form.Item>
        </Form>
      </Modal>
    </>
  )
}
