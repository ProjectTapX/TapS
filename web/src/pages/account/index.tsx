import { useEffect, useState } from 'react'
import { Card, Button, Table, Space, Tag, App, Popconfirm, Typography, Empty, Alert } from 'antd'
import { LinkOutlined, DisconnectOutlined, ReloadOutlined } from '@ant-design/icons'
import { useTranslation } from 'react-i18next'
import { ssoApi, authConfigApi, type MyIdentity, type SSOProviderPublic, type LoginMethod } from '@/api/resources'
import { useAuthStore } from '@/stores/auth'
import { formatApiError } from '@/api/errors'

// Account self-service page. Today its only job is letting users
// bind / unbind SSO identities. We hide the bind UI in password-only
// mode (no SSO buttons would even work) and warn before the last
// unlink in oidc-only mode (server also enforces this).
export default function AccountPage() {
  const { t } = useTranslation()
  const { message } = App.useApp()
  const user = useAuthStore((s) => s.user)
  const [loading, setLoading] = useState(false)
  const [bindings, setBindings] = useState<MyIdentity[]>([])
  const [providers, setProviders] = useState<SSOProviderPublic[]>([])
  const [method, setMethod] = useState<LoginMethod>('password-only')

  const reload = () => {
    setLoading(true)
    Promise.all([
      ssoApi.myIdentities().catch(() => [] as MyIdentity[]),
      ssoApi.publicProviders().catch(() => [] as SSOProviderPublic[]),
      authConfigApi.getMethod().catch(() => 'password-only' as LoginMethod),
    ]).then(([b, p, m]) => {
      setBindings(b); setProviders(p); setMethod(m)
    }).finally(() => setLoading(false))
  }

  useEffect(() => { reload() }, [])

  const ssoEnabled = method !== 'password-only'
  const boundNames = new Set(bindings.map(b => b.providerName))
  const unboundProviders = providers.filter(p => !boundNames.has(p.name))

  const onBind = (name: string) => {
    // Same as the login page: full navigation so IdP cookies are set.
    window.location.assign(`/api/oauth/start/${encodeURIComponent(name)}`)
  }

  const onUnlink = async (id: number) => {
    try {
      await ssoApi.unlinkMine(id)
      message.success(t('account.unlinkOk'))
      reload()
    } catch (e: any) {
      message.error(formatApiError(e, 'account.unlinkFailed'))
    }
  }

  return (
    <div style={{ display: 'flex', flexDirection: 'column', gap: 16 }}>
      <Card
        title={t('account.ssoTitle')}
        extra={<Button icon={<ReloadOutlined />} onClick={reload} loading={loading}>{t('common.refresh')}</Button>}
      >
        <Typography.Paragraph type="secondary" style={{ marginBottom: 16 }}>
          {t('account.ssoDesc', { username: user?.username ?? '' })}
        </Typography.Paragraph>

        {!ssoEnabled && (
          <Alert
            type="info"
            showIcon
            style={{ marginBottom: 16 }}
            message={t('account.ssoDisabledNotice')}
          />
        )}

        {method === 'oidc-only' && bindings.length === 1 && (
          <Alert
            type="warning"
            showIcon
            style={{ marginBottom: 16 }}
            message={t('account.lastBindingWarn')}
          />
        )}

        <Table<MyIdentity>
          rowKey="id"
          loading={loading}
          dataSource={bindings}
          pagination={false}
          locale={{ emptyText: <Empty description={t('account.noBindings')} /> }}
          columns={[
            {
              title: t('account.colProvider'),
              dataIndex: 'providerDisplayName',
              render: (v: string, r) => <Space><Tag color="blue">{r.providerName}</Tag>{v}</Space>,
            },
            { title: t('account.colEmail'), dataIndex: 'email' },
            {
              title: t('account.colLinkedAt'),
              dataIndex: 'linkedAt',
              render: (v: string) => v ? new Date(v).toLocaleString() : '-',
            },
            {
              title: t('account.colLastUsedAt'),
              dataIndex: 'lastUsedAt',
              render: (v: string) => v ? new Date(v).toLocaleString() : '-',
            },
            {
              title: t('common.actions'),
              key: 'action',
              width: 120,
              render: (_: any, r) => (
                <Popconfirm
                  title={t('account.confirmUnlink', { name: r.providerDisplayName })}
                  okText={t('common.confirm')}
                  cancelText={t('common.cancel')}
                  onConfirm={() => onUnlink(r.id)}
                >
                  <Button danger size="small" icon={<DisconnectOutlined />}>
                    {t('account.unlink')}
                  </Button>
                </Popconfirm>
              ),
            },
          ]}
        />

        {ssoEnabled && unboundProviders.length > 0 && (
          <div style={{ marginTop: 24 }}>
            <Typography.Title level={5} style={{ marginBottom: 12 }}>{t('account.bindMore')}</Typography.Title>
            <Space wrap>
              {unboundProviders.map(p => (
                <Button key={p.name} icon={<LinkOutlined />} onClick={() => onBind(p.name)}>
                  {t('account.bindWith', { name: p.displayName })}
                </Button>
              ))}
            </Space>
          </div>
        )}
      </Card>
    </div>
  )
}
