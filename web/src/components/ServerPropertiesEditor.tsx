import { useEffect, useMemo, useState } from 'react'
import { Card, Form, Input, Button, Space, App, Spin, Alert, InputNumber, Switch, Tooltip } from 'antd'
import { SaveOutlined, ReloadOutlined } from '@ant-design/icons'
import { useTranslation } from 'react-i18next'
import { fsApi } from '@/api/fs'
import { formatApiError } from '@/api/errors'

// A subset of well-known server.properties keys we render with friendly inputs.
// Anything not in this list still appears as a plain string row.
const KNOWN: Record<string, { type: 'bool' | 'int' | 'string'; help?: string }> = {
  'server-port': { type: 'int' },
  'max-players': { type: 'int' },
  'gamemode': { type: 'string', help: 'survival | creative | adventure | spectator' },
  'difficulty': { type: 'string', help: 'peaceful | easy | normal | hard' },
  'level-name': { type: 'string' },
  'level-seed': { type: 'string' },
  'motd': { type: 'string' },
  'pvp': { type: 'bool' },
  'online-mode': { type: 'bool' },
  'white-list': { type: 'bool' },
  'enforce-whitelist': { type: 'bool' },
  'allow-flight': { type: 'bool' },
  'allow-nether': { type: 'bool' },
  'spawn-monsters': { type: 'bool' },
  'spawn-animals': { type: 'bool' },
  'spawn-npcs': { type: 'bool' },
  'view-distance': { type: 'int' },
  'simulation-distance': { type: 'int' },
  'enable-command-block': { type: 'bool' },
  'hardcore': { type: 'bool' },
  'force-gamemode': { type: 'bool' },
  'spawn-protection': { type: 'int' },
}

interface Props {
  daemonId: number
  workingDir: string // relative to daemon root
}

interface Row { key: string; value: string }

function parse(text: string): Row[] {
  const rows: Row[] = []
  for (const line of text.split(/\r?\n/)) {
    const t = line.trim()
    if (!t || t.startsWith('#')) continue
    const eq = t.indexOf('=')
    if (eq < 0) continue
    rows.push({ key: t.slice(0, eq).trim(), value: t.slice(eq + 1) })
  }
  return rows
}

function serialize(rows: Row[]): string {
  return rows.map(r => `${r.key}=${r.value}`).join('\n') + '\n'
}

function joinPath(dir: string, name: string) {
  const d = (dir || '').replace(/\\/g, '/').replace(/\/+$/, '')
  return (d.startsWith('/') ? d : '/' + d) + '/' + name
}

export default function ServerPropertiesEditor({ daemonId, workingDir }: Props) {
  const { t } = useTranslation()
  const { message } = App.useApp()
  const fs = useMemo(() => fsApi(daemonId), [daemonId])
  const [rows, setRows] = useState<Row[] | null>(null)
  const [loading, setLoading] = useState(false)
  const [missing, setMissing] = useState(false)

  const filePath = joinPath(workingDir, 'server.properties')

  const load = async () => {
    setLoading(true); setMissing(false)
    try {
      const r = await fs.read(filePath)
      setRows(parse(r.content))
    } catch (e: any) {
      setRows([])
      setMissing(true)
    } finally { setLoading(false) }
  }
  useEffect(() => { load() }, [daemonId, workingDir])

  if (loading || rows === null) return <Spin />

  const update = (i: number, val: string) => {
    setRows(rows.map((r, idx) => idx === i ? { ...r, value: val } : r))
  }

  const onSave = async () => {
    try {
      await fs.write(filePath, serialize(rows))
      message.success(t('common.success'))
    } catch (e: any) {
      message.error(formatApiError(e, 'common.error'))
    }
  }

  return (
    <Card
      title={<code>{filePath}</code>}
      extra={
        <Space>
          <Button icon={<ReloadOutlined />} onClick={load}>{t('common.refresh')}</Button>
          <Button type="primary" icon={<SaveOutlined />} onClick={onSave}>{t('common.save')}</Button>
        </Space>
      }
    >
      {missing && (
        <Alert type="info" showIcon style={{ marginBottom: 16 }}
          message={t('mc.propsMissing')} description={t('mc.propsMissingDesc')} />
      )}
      <Form layout="vertical" colon={false}>
        {rows.length === 0 && <Alert type="warning" message={t('mc.propsEmpty')} />}
        {rows.map((r, i) => {
          const meta = KNOWN[r.key]
          if (meta?.type === 'bool') {
            return (
              <Form.Item key={r.key} label={<code>{r.key}</code>} style={{ marginBottom: 8 }}>
                <Switch checked={r.value === 'true'} onChange={(v) => update(i, v ? 'true' : 'false')} />
              </Form.Item>
            )
          }
          if (meta?.type === 'int') {
            return (
              <Form.Item key={r.key} label={<code>{r.key}</code>} style={{ marginBottom: 8 }}>
                <InputNumber value={Number(r.value) || 0} onChange={(v) => update(i, String(v ?? ''))} />
              </Form.Item>
            )
          }
          return (
            <Form.Item key={r.key} label={
              <Space><code>{r.key}</code>{meta?.help && <Tooltip title={meta.help}><span style={{ color: '#888' }}>?</span></Tooltip>}</Space>
            } style={{ marginBottom: 8 }}>
              <Input value={r.value} onChange={(e) => update(i, e.target.value)} />
            </Form.Item>
          )
        })}
      </Form>
    </Card>
  )
}
