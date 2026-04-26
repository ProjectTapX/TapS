import { useEffect, useRef, useState } from 'react'
import { Modal, Progress, Space, Tag, Typography, Button, App } from 'antd'
import { CheckCircleFilled, LoadingOutlined } from '@ant-design/icons'
import { useTranslation } from 'react-i18next'
import { dockerApi, type PullSseEvent } from '@/api/docker'

interface Props {
  daemonId: number
  image: string
  open: boolean
  onClose: () => void
  onSuccess: () => void
}

type LayerState = 'queued' | 'downloading' | 'verifying' | 'extracting' | 'done'

interface Layer {
  id: string
  state: LayerState
  rawStatus: string
}

// State → progress mapping (no per-layer byte progress in non-tty mode)
const STATE_PCT: Record<LayerState, number> = {
  queued: 5, downloading: 35, verifying: 55, extracting: 75, done: 100,
}

function classify(rest: string): LayerState | null {
  const s = rest.toLowerCase()
  if (s.includes('pull complete') || s.includes('already exists')) return 'done'
  if (s.includes('extracting')) return 'extracting'
  if (s.includes('verifying') || s.includes('download complete')) return 'verifying'
  if (s.includes('downloading') || s.includes('pulling fs layer') || s.startsWith('waiting')) {
    return s.includes('pulling fs layer') ? 'queued' : 'downloading'
  }
  return null
}

const RE_LAYER = /^([a-f0-9]{6,}):\s+(.+)$/

export default function PullProgressModal({ daemonId, image, open, onClose, onSuccess }: Props) {
  const { t } = useTranslation()
  const { message } = App.useApp()
  const [layers, setLayers] = useState<Map<string, Layer>>(new Map())
  const [tail, setTail] = useState<string[]>([])
  const [phase, setPhase] = useState<'idle' | 'running' | 'done' | 'error'>('idle')
  const [errorText, setErrorText] = useState('')
  const [upToDate, setUpToDate] = useState(false)
  const abortRef = useRef<AbortController | null>(null)

  useEffect(() => {
    if (!open) return
    setLayers(new Map()); setTail([]); setPhase('running'); setErrorText(''); setUpToDate(false)
    const ctl = new AbortController()
    abortRef.current = ctl

    ;(async () => {
      try {
        for await (const ev of dockerApi(daemonId).pullStream(image, ctl.signal)) {
          handleEvent(ev)
        }
      } catch (e: any) {
        if (e?.name !== 'AbortError') {
          setPhase('error'); setErrorText(String(e?.message ?? e))
        }
      }
    })()

    return () => { ctl.abort() }
  }, [open, daemonId, image])

  const handleEvent = (ev: PullSseEvent) => {
    if (ev.type === 'line') ingestLine(ev.line)
    else if (ev.type === 'done') {
      if (ev.error) {
        setPhase('error'); setErrorText(ev.error)
      } else {
        // The pull as a whole succeeded — force every known layer to "done".
        // Docker sometimes never emits a final "Pull complete" line for layers
        // that were resolved from local cache or coalesced in batched output,
        // which would otherwise leave them stuck at "verifying".
        setLayers((prev) => {
          const next = new Map(prev)
          for (const [id, l] of next) next.set(id, { ...l, state: 'done' })
          return next
        })
        setPhase('done')
        message.success(t('images.pulled', { image }))
        onSuccess()
      }
    }
  }

  const ingestLine = (line: string) => {
    // detect "image is up to date" case (no layer events come)
    if (/Image is up to date/i.test(line)) {
      setUpToDate(true)
    }
    const m = line.match(RE_LAYER)
    if (!m) {
      setTail((tl) => [...tl.slice(-9), line])
      return
    }
    const id = m[1]
    const rest = m[2]
    const newState = classify(rest)
    setLayers((prev) => {
      const next = new Map(prev)
      const cur = next.get(id) ?? { id, state: 'queued' as LayerState, rawStatus: rest }
      const merged: Layer = { ...cur, rawStatus: rest }
      if (newState) {
        // never go backwards (e.g. "Downloading" line after "Pull complete")
        if (STATE_PCT[newState] >= STATE_PCT[merged.state]) merged.state = newState
      }
      next.set(id, merged)
      return next
    })
  }

  const layerList = [...layers.values()]
  const completeCount = layerList.filter(l => l.state === 'done').length

  // overall: average per-layer state progress; or 100% if up-to-date / no layers but done
  let overall = 0
  if (phase === 'done' && (upToDate || layerList.length === 0)) {
    overall = 100
  } else if (layerList.length > 0) {
    overall = Math.round(layerList.reduce((s, l) => s + STATE_PCT[l.state], 0) / layerList.length)
  }

  const cancel = () => {
    abortRef.current?.abort()
    onClose()
  }

  return (
    <Modal
      open={open}
      title={
        <Space>
          {phase === 'running' && <LoadingOutlined spin />}
          {phase === 'done' && <CheckCircleFilled style={{ color: '#10b981' }} />}
          {t('images.pulling', { image })}
        </Space>
      }
      width={680}
      onCancel={phase === 'running' ? undefined : onClose}
      closable={phase !== 'running'}
      maskClosable={phase !== 'running'}
      footer={
        phase === 'running' ? (
          <Button danger onClick={cancel}>{t('common.cancel')}</Button>
        ) : (
          <Button type="primary" onClick={onClose}>{t('common.ok')}</Button>
        )
      }
    >
      <div style={{ marginBottom: 16 }}>
        <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'baseline' }}>
          <Typography.Text type="secondary">
            {phase === 'done' && (upToDate || layerList.length === 0)
              ? t('images.pullUpToDate')
              : t('images.pullOverall', { done: completeCount, total: layerList.length || '?' })}
          </Typography.Text>
          <Typography.Text strong>{overall}%</Typography.Text>
        </div>
        <Progress percent={overall} status={phase === 'error' ? 'exception' : phase === 'done' ? 'success' : 'active'} showInfo={false} />
      </div>

      <div style={{
        maxHeight: 320, overflowY: 'auto',
        background: 'var(--taps-bg)', border: '1px solid var(--taps-border)', borderRadius: 8,
        padding: 8,
      }}>
        {layerList.length === 0 && phase === 'running' && (
          <div style={{ padding: 16, textAlign: 'center', color: 'var(--taps-text-muted)', fontSize: 12 }}>
            {t('images.pullStarting')}
          </div>
        )}
        {layerList.map(l => {
          const pct = STATE_PCT[l.state]
          const stateLabelKey = `images.pullState.${l.state}`
          return (
            <div key={l.id} style={{
              display: 'flex', alignItems: 'center', gap: 10,
              padding: '6px 8px', fontSize: 12,
            }}>
              <code className="taps-mono" style={{ width: 90, color: 'var(--taps-text-muted)', flexShrink: 0 }}>{l.id.slice(0, 12)}</code>
              <div style={{ flex: 1, minWidth: 0 }}>
                <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'baseline', marginBottom: 2 }}>
                  <span>
                    {l.state === 'done' && <CheckCircleFilled style={{ color: '#10b981', marginRight: 4 }} />}
                    {t(stateLabelKey)}
                  </span>
                  <span style={{ color: 'var(--taps-text-muted)', fontSize: 11, marginLeft: 8 }}>{pct}%</span>
                </div>
                <Progress percent={pct} showInfo={false} size="small" style={{ margin: 0 }}
                  strokeColor={l.state === 'done' ? '#10b981' : '#007BFC'} />
              </div>
            </div>
          )
        })}
      </div>

      {tail.length > 0 && (
        <div style={{ marginTop: 12, padding: 10, background: 'var(--taps-bg)', borderRadius: 8, fontSize: 11 }}>
          {tail.map((l, i) => (
            <div key={i} className="taps-mono" style={{ color: 'var(--taps-text-muted)' }}>{l}</div>
          ))}
        </div>
      )}

      {phase === 'error' && (
        <div style={{ marginTop: 12 }}>
          <Tag color="error">{t('common.error')}</Tag>
          <span className="taps-mono" style={{ marginLeft: 8 }}>{errorText}</span>
        </div>
      )}
    </Modal>
  )
}
