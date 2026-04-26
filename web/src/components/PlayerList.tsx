import { useEffect, useState } from 'react'
import { Card, Tag, Empty, Space, Button, Statistic, Row, Col, Avatar } from 'antd'
import { ReloadOutlined, UserOutlined } from '@ant-design/icons'
import { useTranslation } from 'react-i18next'
import { mcApi, type McPlayersResp } from '@/api/mc'
import { formatApiError } from '@/api/errors'

interface Props { daemonId: number; uuid: string }

export default function PlayerList({ daemonId, uuid }: Props) {
  const { t } = useTranslation()
  const [data, setData] = useState<McPlayersResp | null>(null)
  const [loading, setLoading] = useState(false)

  const load = async () => {
    setLoading(true)
    try { setData(await mcApi.players(daemonId, uuid)) }
    catch (e: any) { setData({ online: false, error: formatApiError(e, ''), max: 0, count: 0, players: [] }) }
    finally { setLoading(false) }
  }
  useEffect(() => { load(); const t = setInterval(load, 10000); return () => clearInterval(t) }, [daemonId, uuid])

  if (!data) return <Empty />

  return (
    <Card
      loading={loading && !data}
      title={data.online
        ? <Space><Tag color="green">{t('mc.online')}</Tag>{data.version ?? ''}</Space>
        : <Space><Tag color="red">{t('mc.offline')}</Tag></Space>}
      extra={<Button size="small" icon={<ReloadOutlined />} onClick={load}>{t('common.refresh')}</Button>}
    >
      {!data.online ? (
        <Empty description={data.error || t('mc.notReady')} />
      ) : (
        <>
          <Row gutter={16} style={{ marginBottom: 16 }}>
            <Col span={8}><Statistic title={t('mc.players')} value={data.count} suffix={`/ ${data.max}`} /></Col>
            <Col span={16}>
              <div style={{ color: '#888', fontSize: 12, whiteSpace: 'pre-wrap' }}>{data.description}</div>
            </Col>
          </Row>
          {data.players.length === 0 ? (
            <Empty description={t('mc.noPlayers')} image={Empty.PRESENTED_IMAGE_SIMPLE} />
          ) : (
            <Space wrap>
              {data.players.map(p => (
                <Card.Grid key={p.name} hoverable={false} style={{ padding: 12, width: 'auto' }}>
                  <Space>
                    <Avatar icon={<UserOutlined />} size="small" />
                    <code>{p.name}</code>
                  </Space>
                </Card.Grid>
              ))}
            </Space>
          )}
        </>
      )}
    </Card>
  )
}
