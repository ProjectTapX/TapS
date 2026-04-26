import { useEffect, useState } from 'react'
import { Modal, Form, Select, Input, App, Steps, Card, Space, Tag, Alert } from 'antd'
import { useTranslation } from 'react-i18next'
import { deployApi, type Template } from '@/api/mc'
import { daemonsApi, type DaemonView } from '@/api/resources'
import { formatApiError } from '@/api/errors'

interface Props {
  open: boolean
  onClose: () => void
  onDeployed?: () => void
}

export default function QuickDeploy({ open, onClose, onDeployed }: Props) {
  const { t } = useTranslation()
  const { message } = App.useApp()
  const [step, setStep] = useState(0)
  const [templates, setTemplates] = useState<Template[]>([])
  const [picked, setPicked] = useState<Template | null>(null)
  const [versions, setVersions] = useState<string[]>([])
  const [versionFallback, setVersionFallback] = useState(false)
  const [daemons, setDaemons] = useState<DaemonView[]>([])
  const [submitting, setSubmitting] = useState(false)
  const [form] = Form.useForm()

  useEffect(() => {
    if (!open) return
    setStep(0); setPicked(null); form.resetFields()
    deployApi.templates().then(setTemplates)
    daemonsApi.list().then(setDaemons)
  }, [open])

  const onPick = async (tpl: Template) => {
    setPicked(tpl); setStep(1)
    try {
      const r = await deployApi.paperVersions()
      setVersions(r.versions); setVersionFallback(r.fallback)
    } catch { setVersions([]) }
  }

  const onSubmit = async () => {
    const v = await form.validateFields()
    setSubmitting(true)
    const hide = message.loading(t('deploy.deploying'), 0)
    try {
      await deployApi.deploy(v.daemonId, {
        template: picked!.id,
        version: v.version,
        instanceName: v.instanceName,
        maxMemory: v.maxMemory,
        hostPort: v.hostPort,
      })
      hide(); message.success(t('deploy.done'))
      onClose(); onDeployed?.()
    } catch (e: any) {
      hide(); message.error(formatApiError(e, 'common.error'))
    } finally { setSubmitting(false) }
  }

  return (
    <Modal
      open={open}
      onCancel={onClose}
      footer={null}
      title={t('deploy.title')}
      width={680}
      destroyOnClose
    >
      <Steps current={step} size="small" style={{ marginBottom: 24 }}
        items={[
          { title: t('deploy.s1') },
          { title: t('deploy.s2') },
        ]}
      />

      {step === 0 && (
        <Space direction="vertical" style={{ width: '100%' }}>
          {templates.map(tpl => (
            <Card key={tpl.id} hoverable onClick={() => onPick(tpl)}>
              <Space direction="vertical" size={2}>
                <Space><strong>{tpl.name}</strong><Tag>{tpl.type}</Tag></Space>
                <span style={{ color: '#aaa', fontSize: 12 }}>{tpl.description}</span>
              </Space>
            </Card>
          ))}
        </Space>
      )}

      {step === 1 && picked && (
        <Form form={form} layout="vertical" onFinish={onSubmit}
          initialValues={{ maxMemory: '2G', hostPort: 25565, daemonId: daemons.find(d => d.connected)?.id }}>
          <Alert type="info" showIcon style={{ marginBottom: 16 }}
            message={t('deploy.dockerInfo')}
            description={t('deploy.dockerInfoDesc')} />
          <Form.Item label={t('deploy.template')}>
            <Tag color="blue">{picked.name}</Tag>
          </Form.Item>
          <Form.Item name="daemonId" label={t('instance.node')} rules={[{ required: true }]}>
            <Select options={daemons.filter(d => d.connected).map(d => ({
              label: d.name + (d.dockerReady === false ? ' · ⚠ no docker' : ''),
              value: d.id,
              disabled: d.dockerReady === false,
            }))} />
          </Form.Item>
          <Form.Item name="instanceName" label={t('instance.name')} rules={[{ required: true }]}>
            <Input placeholder="my-paper-server" />
          </Form.Item>
          <Form.Item name="version" label={t('deploy.version')} rules={[{ required: true }]}>
            <Select showSearch options={versions.map(v => ({ label: v, value: v }))} />
          </Form.Item>
          <Form.Item name="hostPort" label={t('deploy.hostPort')} extra={t('deploy.hostPortHelp')}>
            <Input type="number" />
          </Form.Item>
          <Form.Item name="maxMemory" label={t('deploy.maxMem')} extra="e.g. 2G, 4G">
            <Input />
          </Form.Item>
          <Space>
            <button type="submit" disabled={submitting}
              style={{ padding: '6px 16px', background: '#7c5cff', color: '#fff', border: 'none', borderRadius: 6, cursor: 'pointer' }}>
              {submitting ? t('common.loading') : t('deploy.submit')}
            </button>
          </Space>
        </Form>
      )}
    </Modal>
  )
}
