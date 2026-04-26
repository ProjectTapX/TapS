import { useEffect, useRef, useState } from 'react'
import { Modal, Select, Form, Checkbox, Alert, Progress, Space, Tag, App } from 'antd'
import { useTranslation } from 'react-i18next'
import { serverDeployApi, type ServerProvider, type DeployStatus } from '@/api/deploy'
import { formatApiError } from '@/api/errors'

// ServerDeployModal walks the user through picking a server type +
// version + build, then kicks off the install on the daemon and
// streams progress via 1-second polling. Stays open through the whole
// deploy so the user can watch the bar fill.
export default function ServerDeployModal({
  open, daemonId, uuid, onClose, onDone,
}: {
  open: boolean
  daemonId: number
  uuid: string
  onClose: () => void
  onDone?: () => void
}) {
  const { t } = useTranslation()
  const { message } = App.useApp()

  const [providers, setProviders] = useState<ServerProvider[]>([])
  const [type, setType] = useState<string | undefined>()
  const [versions, setVersions] = useState<string[]>([])
  const [version, setVersion] = useState<string | undefined>()
  const [builds, setBuilds] = useState<string[]>([])
  const [build, setBuild] = useState<string | undefined>()
  const [eula, setEula] = useState(true)
  const [loadingV, setLoadingV] = useState(false)
  const [loadingB, setLoadingB] = useState(false)

  // Phase: 'pick' shows the form; 'progress' shows the progress bar.
  const [phase, setPhase] = useState<'pick' | 'progress'>('pick')
  const [status, setStatus] = useState<DeployStatus | null>(null)
  const pollRef = useRef<number | null>(null)

  useEffect(() => {
    if (!open) return
    serverDeployApi.types().then(setProviders).catch(() => { /* ignore */ })
    // Only auto-resume if a deploy is *currently in progress*. A
    // finished deploy (success or error) should let the user start a
    // fresh one — re-opening the modal goes back to the picker.
    serverDeployApi.status(daemonId, uuid).then(s => {
      if (s.active) {
        setStatus(s)
        setPhase('progress')
      } else {
        setPhase('pick')
        setStatus(null)
      }
    }).catch(() => {
      setPhase('pick')
      setStatus(null)
    })
  }, [open, daemonId, uuid])

  // Reload version list whenever the provider changes.
  useEffect(() => {
    if (!type) { setVersions([]); setVersion(undefined); return }
    setLoadingV(true)
    serverDeployApi.versions(type).then(vs => {
      setVersions(vs)
      setVersion(vs[0])
    }).catch(e => message.error(formatApiError(e, 'common.error')))
      .finally(() => setLoadingV(false))
    setBuilds([]); setBuild(undefined)
  }, [type])

  // Reload build list when version changes (only for providers that
  // actually have builds — Vanilla/Fabric skip this round-trip).
  useEffect(() => {
    const p = providers.find(x => x.id === type)
    if (!p?.hasBuilds || !type || !version) { setBuilds([]); setBuild(undefined); return }
    setLoadingB(true)
    serverDeployApi.builds(type, version).then(bs => {
      setBuilds(bs)
      setBuild(bs[0])
    }).catch(e => message.error(formatApiError(e, 'common.error')))
      .finally(() => setLoadingB(false))
  }, [type, version, providers])

  // Poll while in progress phase. Stops when the deploy isn't active
  // anymore (done or error).
  useEffect(() => {
    if (phase !== 'progress') return
    const tick = async () => {
      try {
        const s = await serverDeployApi.status(daemonId, uuid)
        setStatus(s)
        if (!s.active) {
          if (pollRef.current) window.clearInterval(pollRef.current)
          if (s.stage === 'done') {
            message.success(t('serverDeploy.successMsg'))
            onDone?.()
          } else if (s.stage === 'error') {
            message.error(s.error || t('common.error'))
          }
        }
      } catch { /* ignore — daemon might be momentarily unreachable */ }
    }
    tick()
    const id = window.setInterval(tick, 1000)
    pollRef.current = id
    return () => { if (pollRef.current) window.clearInterval(pollRef.current); pollRef.current = null }
  }, [phase, daemonId, uuid])

  const provider = providers.find(p => p.id === type)

  const onStart = async () => {
    if (!type || !version) { message.error(t('serverDeploy.pickTypeVersion')); return }
    if (!eula) { message.error(t('serverDeploy.eulaRequired')); return }
    try {
      await serverDeployApi.start(daemonId, uuid, { type, version, build, acceptEula: true })
      setPhase('progress')
    } catch (e: any) {
      message.error(formatApiError(e, 'common.error'))
    }
  }

  const renderProgressBody = () => {
    const s = status
    const stage = s?.stage ?? 'queued'
    // Always show a progress bar — for stages that lack a real percent
    // (validating/clearing/installing/configuring) we render an
    // indeterminate "active" bar so the user sees the deploy is still
    // moving, similar to the docker pull experience.
    const bytesKnown = stage === 'downloading' && s && s.bytesTotal > 0
    const indeterminate = !bytesKnown && (stage as string) !== 'done' && (stage as string) !== 'error'
    return (
      <Space direction="vertical" size="middle" style={{ width: '100%' }}>
        <Space wrap>
          {(['validating','clearing','downloading','installing','configuring','done'] as const).map(st => {
            const reached = stageOrder(stage) >= stageOrder(st)
            const current = stage === st && (stage as string) !== 'done' && (stage as string) !== 'error'
            return (
              <Tag key={st}
                color={(stage as string) === 'error' ? (reached ? 'red' : undefined)
                  : current ? 'processing'
                  : reached ? 'success' : undefined}
                bordered={false}
              >{t(`serverDeploy.stage.${st}`)}</Tag>
            )
          })}
        </Space>
        {s?.message && <div style={{ color: 'var(--taps-text-muted)' }}>{
          s.messageKey ? t(`serverDeploy.${s.messageKey}`, { defaultValue: s.message }) : s.message
        }</div>}
        <Progress
          percent={(stage as string) === 'done' ? 100 : (indeterminate ? 100 : (s?.percent ?? 0))}
          status={(stage as string) === 'error' ? 'exception'
            : (stage as string) === 'done' ? 'success'
            : 'active'}
          format={() => bytesKnown
            ? `${fmtMB(s!.bytesDone)} / ${fmtMB(s!.bytesTotal)} (${s!.percent}%)`
            : indeterminate ? t('serverDeploy.working')
            : `${s?.percent ?? 0}%`}
          strokeColor={(stage as string) === 'error' ? '#ef4444' : '#007BFC'}
        />
        {(stage as string) === 'done' && (
          <Alert type="success" showIcon message={t('serverDeploy.successMsg')} />
        )}
        {(stage as string) === 'error' && (
          <Alert type="error" showIcon
            message={t('serverDeploy.errorTitle')}
            description={s?.error || t('common.error')} />
        )}
      </Space>
    )
  }

  return (
    <Modal
      title={t('serverDeploy.title')}
      open={open}
      onCancel={onClose}
      onOk={phase === 'pick' ? onStart : onClose}
      okText={phase === 'pick' ? t('serverDeploy.start') : t('common.close')}
      okButtonProps={{ disabled: phase === 'progress' && status?.active === true }}
      cancelText={t('common.cancel')}
      cancelButtonProps={{ style: { display: phase === 'progress' ? 'none' : undefined } }}
      destroyOnClose
      width={560}
      maskClosable={false}
    >
      {phase === 'pick' ? (
        <>
          <Alert type="warning" showIcon style={{ marginBottom: 12 }}
            message={t('serverDeploy.clearWarning')}
            description={t('serverDeploy.clearWarningDesc')} />
          <Form layout="vertical">
            <Form.Item label={t('serverDeploy.type')}>
              <Select value={type} onChange={setType}
                placeholder={t('serverDeploy.typePh')}
                options={providers.map(p => ({ label: p.displayName, value: p.id }))} />
              {provider?.needsImage && (
                <div style={{ fontSize: 12, color: 'var(--taps-text-muted)', marginTop: 4 }}>
                  ⚠ {t('serverDeploy.installerImageNote')}
                </div>
              )}
            </Form.Item>
            <Form.Item label={t('serverDeploy.version')}>
              <Select value={version} onChange={setVersion} loading={loadingV} disabled={!type}
                showSearch placeholder={t('serverDeploy.versionPh')}
                notFoundContent={loadingV ? t('serverDeploy.loading') : (type ? t('serverDeploy.empty') : t('serverDeploy.pickTypeFirst'))}
                options={versions.map(v => ({ label: v, value: v }))} />
            </Form.Item>
            {provider?.hasBuilds && (
              <Form.Item label={t('serverDeploy.build')}>
                <Select value={build} onChange={setBuild} loading={loadingB} disabled={!version}
                  showSearch placeholder={t('serverDeploy.buildPh')}
                  notFoundContent={loadingB ? t('serverDeploy.loading') : (version ? t('serverDeploy.empty') : t('serverDeploy.pickVersionFirst'))}
                  options={builds.map(b => ({ label: b, value: b }))} />
              </Form.Item>
            )}
            <Form.Item>
              <Checkbox checked={eula} onChange={(e) => setEula(e.target.checked)}>
                {t('serverDeploy.eula')}
              </Checkbox>
              <div style={{ fontSize: 12, color: 'var(--taps-text-muted)', marginTop: 4 }}>
                {t('serverDeploy.eulaHelp')}
              </div>
            </Form.Item>
          </Form>
        </>
      ) : renderProgressBody()}
    </Modal>
  )
}

function stageOrder(s: string): number {
  const order: Record<string, number> = {
    queued: 0, validating: 1, clearing: 2, downloading: 3,
    installing: 4, configuring: 5, done: 6, error: 6,
  }
  return order[s] ?? 0
}

function fmtMB(n: number): string {
  if (n < 1024 * 1024) return `${(n / 1024).toFixed(0)} KB`
  return `${(n / 1024 / 1024).toFixed(1)} MB`
}
