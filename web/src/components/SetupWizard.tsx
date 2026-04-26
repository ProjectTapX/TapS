import { useEffect, useMemo, useRef, useState } from 'react'
import { Modal, Steps, Form, Input, Select, InputNumber, App, Button, Alert } from 'antd'
import { useTranslation } from 'react-i18next'
import { Link } from 'react-router-dom'
import { instancesApi, type InstanceInfo } from '@/api/resources'
import { dockerApi, type DockerImage } from '@/api/docker'
import { formatApiError } from '@/api/errors'

interface Props {
  open: boolean
  onClose: () => void
  daemonId: number
  info: InstanceInfo
  onDone: () => void
}

// Detect whether an instance still needs the setup wizard. We treat a
// docker instance as "incomplete" when it lacks an image (Command), the
// launch CMD (Args), the stop directive, or any docker port mapping.
// Non-docker instances always have a Command at create time, so they are
// never flagged.
export function needsSetup(info: InstanceInfo): boolean {
  const c = info.config
  if (c.type !== 'docker') return false
  if (!c.command) return true
  if (!c.args || c.args.length === 0) return true
  if (!c.stopCmd) return true
  if (!c.dockerPorts || c.dockerPorts.length === 0) return true
  return false
}

// Walk the existing dockerPorts spec and return [hostPort, containerPort].
function parsePorts(specs: string[] | undefined): { host?: number; container?: number } {
  if (!specs || specs.length === 0) return {}
  const body = specs[0].split('/')[0]
  const parts = body.split(':')
  let h: string, c: string
  if (parts.length === 3) { h = parts[1]; c = parts[2] }
  else if (parts.length === 2) { h = parts[0]; c = parts[1] }
  else { h = parts[0]; c = parts[0] }
  const hn = Number(h), cn = Number(c)
  return {
    host: Number.isFinite(hn) && hn > 0 ? hn : undefined,
    container: Number.isFinite(cn) && cn > 0 ? cn : undefined,
  }
}

// Shell-ish split that respects double quotes — same routine the create
// form uses to turn the textarea into a real argv.
function splitArgs(line: string): string[] {
  const out: string[] = []
  let cur = '', inQ = false
  for (const ch of line) {
    if (ch === '"') { inQ = !inQ; continue }
    if (!inQ && (ch === ' ' || ch === '\t')) { if (cur) { out.push(cur); cur = '' }; continue }
    cur += ch
  }
  if (cur) out.push(cur)
  return out
}

export default function SetupWizard({ open, onClose, daemonId, info, onDone }: Props) {
  const { t } = useTranslation()
  const { message } = App.useApp()
  const [step, setStep] = useState(0)
  const [images, setImages] = useState<DockerImage[]>([])
  const [form] = Form.useForm()
  const [submitting, setSubmitting] = useState(false)

  const initial = useMemo(() => {
    const ports = parsePorts(info.config.dockerPorts)
    return {
      name: info.config.name || `inst-${Math.random().toString(16).slice(2, 10)}`,
      command: info.config.command || undefined,
      argsText: (info.config.args ?? []).join(' '),
      stopCmd: info.config.stopCmd || 'stop',
      containerPort: ports.container ?? 25565,
      hostPort: ports.host ?? 25565,
    }
  }, [info])

  // We only seed the form *once* per open session. The parent polls the
  // instance every couple seconds; without this guard each poll returns
  // a fresh `info` reference, recomputes `initial`, re-runs the effect,
  // and clobbers whatever the user typed/picked back to the on-disk
  // (still-empty) config.
  const seeded = useRef(false)
  useEffect(() => {
    if (!open) { seeded.current = false; return }
    if (seeded.current) return
    setStep(0)
    form.setFieldsValue(initial)
    seeded.current = true
    dockerApi(daemonId).images().then(r => setImages(r.images ?? [])).catch(() => { /* ignore — non-admin still gets a useful 200 from the read endpoint */ })
  }, [open, daemonId, initial])

  const imageOptions = useMemo(() => images
    .filter(im => im.repository && im.repository !== '<none>' && im.tag && im.tag !== '<none>')
    .map(im => {
      const ref = `${im.repository}:${im.tag}`
      return { label: im.displayName || ref, value: ref }
    }), [images])

  const stepFields: (keyof typeof initial)[][] = [
    ['name'],
    ['command'],
    ['argsText'],
    ['stopCmd'],
    ['containerPort'],
  ]

  const next = async () => {
    try {
      await form.validateFields(stepFields[step])
      setStep(s => s + 1)
    } catch { /* validation message shown by antd */ }
  }
  const prev = () => setStep(s => Math.max(0, s - 1))

  const finish = async () => {
    let v: any
    try { v = await form.validateFields() } catch { return }
    setSubmitting(true)
    try {
      const hostPort = initial.hostPort
      const containerPort = v.containerPort || hostPort
      await instancesApi.update(daemonId, info.config.uuid, {
        name: (v.name || '').trim() || info.config.name,
        command: v.command,
        args: v.argsText ? splitArgs(v.argsText) : undefined,
        stopCmd: v.stopCmd || 'stop',
        dockerPorts: [`${hostPort}:${containerPort}`],
      })
      message.success(t('setup.done'))
      onDone()
      onClose()
    } catch (e: any) {
      message.error(formatApiError(e, 'common.error'))
    } finally { setSubmitting(false) }
  }

  const stepDefs = [
    { title: t('instance.name'), key: 'name' },
    { title: t('instance.runtime'), key: 'runtime' },
    { title: t('instance.dockerCmd'), key: 'cmd' },
    { title: t('instance.stopCmd'), key: 'stop' },
    { title: t('docker.containerPort'), key: 'port' },
  ]

  return (
    <Modal
      title={t('setup.title', { name: info.config.name })}
      open={open}
      onCancel={onClose}
      width={680}
      destroyOnClose
      footer={
        <div style={{ display: 'flex', justifyContent: 'space-between' }}>
          <Button onClick={prev} disabled={step === 0}>{t('setup.prev')}</Button>
          {step < stepDefs.length - 1
            ? <Button type="primary" onClick={next}>{t('setup.next')}</Button>
            : <Button type="primary" loading={submitting} onClick={finish}>{t('setup.finish')}</Button>}
        </div>
      }
    >
      <Steps current={step} size="small" items={stepDefs.map(s => ({ title: s.title }))} style={{ marginBottom: 24 }} />

      <Alert type="info" showIcon style={{ marginBottom: 16 }} message={t(`setup.hint.${stepDefs[step].key}`)} />

      <Form form={form} layout="vertical">
        {/* All fields stay mounted so Form.values stays intact across steps;
            we just hide the ones not relevant to the current step. */}
        <div style={{ display: step === 0 ? 'block' : 'none' }}>
          <Form.Item name="name" label={t('instance.name')}
            rules={[{ required: true, message: t('setup.required') }, { whitespace: true, message: t('setup.required') }]}
            extra={t('setup.nameHelp')}
          >
            <Input placeholder="my-server" />
          </Form.Item>
        </div>
        <div style={{ display: step === 1 ? 'block' : 'none' }}>
          <Form.Item name="command" label={t('instance.runtime')} rules={[{ required: true, message: t('setup.required') }]}
            extra={imageOptions.length === 0 ? <span>{t('instance.imageEmpty')} <Link to="/images">{t('instance.imageGoPull')}</Link></span> : null}
          >
            <Select showSearch options={imageOptions} placeholder={imageOptions.length === 0 ? t('instance.imageEmptyPh') : t('instance.imagePickPh')} />
          </Form.Item>
        </div>
        <div style={{ display: step === 2 ? 'block' : 'none' }}>
          <Form.Item name="argsText" label={t('instance.dockerCmd')} rules={[{ required: true, message: t('setup.required') }]} extra={t('instance.dockerCmdHelp')}>
            <Input className="taps-mono" placeholder='java -Xmx2G -jar server.jar nogui' />
          </Form.Item>
        </div>
        <div style={{ display: step === 3 ? 'block' : 'none' }}>
          <Form.Item name="stopCmd" label={t('instance.stopCmd')} extra={t('instance.stopCmdHelp')}>
            <Input placeholder="stop" />
          </Form.Item>
        </div>
        <div style={{ display: step === 4 ? 'block' : 'none' }}>
          <Form.Item name="containerPort" label={t('docker.containerPort')} extra={t('docker.containerPortHelp')}>
            <InputNumber min={1} max={65535} style={{ width: '100%' }} />
          </Form.Item>
        </div>
      </Form>
    </Modal>
  )
}
