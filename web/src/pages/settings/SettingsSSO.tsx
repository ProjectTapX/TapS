// SSO / OIDC settings card. Renders inside the main Settings page.
// Three sections, all admin-only:
//
//   1. Public URL — required so OIDC callbacks can be reached from
//      the browser. We block "Add provider" when this is empty.
//   2. Login method — radio: password-only / oidc+password / oidc-only.
//      Switching to oidc-only enforces the Q1+ guard server-side.
//   3. Provider list + create/edit modal — 5 templates plus custom.
//
// Templates only pre-fill the form; the actual provider config still
// goes through the same POST /api/admin/sso/providers endpoint.
import { useEffect, useState } from 'react'
import {
  Card, Form, Input, Radio, Button, Alert, Space, Table, Modal, Select,
  Switch, App, Tag, Typography, Dropdown, Popconfirm,
} from 'antd'
import { PlusOutlined, ReloadOutlined } from '@ant-design/icons'
import { useTranslation } from 'react-i18next'
import {
  ssoApi, authConfigApi,
  type SSOProviderAdmin, type SSOProviderInput, type LoginMethod,
} from '@/api/resources'
import { formatApiError } from '@/api/errors'

interface ProviderTemplate {
  id:           string  // unique key
  templateNameKey: string  // shown on dropdown menu
  // Pre-filled form values
  defaults: Partial<SSOProviderInput> & { issuerHintKey?: string }
}

const TEMPLATES: ProviderTemplate[] = [
  {
    id: 'logto',
    templateNameKey: 'sso.tplLogto',
    defaults: {
      // Leave name (slug) blank so the callback URL banner doesn't
      // pop in with a placeholder before the operator has settled on
      // a real slug. displayName is similarly user-driven; it just
      // happens to default to the brand because most people keep it.
      name: '', displayName: 'Logto',
      issuer: 'https://<your-logto>/oidc',
      issuerHintKey: 'sso.tplLogtoHint',
      scopes: 'openid profile email',
      defaultRole: 'user', autoCreate: true,
    },
  },
  {
    id: 'custom',
    templateNameKey: 'sso.tplCustom',
    defaults: {
      name: '', displayName: '',
      issuer: '',
      scopes: 'openid profile email',
      defaultRole: 'user', autoCreate: true,
    },
  },
]

export type SSOSection = 'publicUrl' | 'loginMethod' | 'providers'

export default function SettingsSSO({ section, onPublicUrlSaved }: { section?: SSOSection; onPublicUrlSaved?: (url: string) => void } = {}) {
  const { t } = useTranslation()
  const { message, modal } = App.useApp()

  // ---- public URL ----
  const [publicUrl, setPublicUrl] = useState('')
  const [publicUrlSaving, setPublicUrlSaving] = useState(false)
  const savePublicUrl = async () => {
    setPublicUrlSaving(true)
    try {
      const trimmed = publicUrl.trim()
      await authConfigApi.setPublicUrl(trimmed)
      message.success(t('common.success'))
      // Notify the parent settings page so its top-of-page
      // "Panel 公开地址 未配置" banner clears immediately on save —
      // without this the banner would only refresh on next page nav.
      onPublicUrlSaved?.(trimmed)
      load()
    } catch (e: any) {
      message.error(formatApiError(e, 'common.error'))
    } finally { setPublicUrlSaving(false) }
  }

  // ---- login method ----
  const [method, setMethod] = useState<LoginMethod>('password-only')
  const [methodSaving, setMethodSaving] = useState(false)
  const saveMethod = async (m: LoginMethod) => {
    setMethodSaving(true)
    try {
      await authConfigApi.setMethod(m)
      setMethod(m)
      message.success(t('common.success'))
    } catch (e: any) {
      message.error(formatApiError(e, 'common.error'))
    } finally { setMethodSaving(false) }
  }

  // ---- providers ----
  const [providers, setProviders] = useState<SSOProviderAdmin[]>([])
  const [loading, setLoading] = useState(false)
  const [editing, setEditing] = useState<SSOProviderAdmin | null>(null)
  const [modalOpen, setModalOpen] = useState(false)
  const [form] = Form.useForm<SSOProviderInput>()
  const [issuerHint, setIssuerHint] = useState<string>('')
  const [testing, setTesting] = useState(false)
  // Track which issuer the operator last successfully tested. If they
  // edit the issuer field after the test passes (B15) and try to save
  // without re-testing, we warn — easy to typo a tenant ID and the
  // first real user finding out via "discovery failed" is a bad UX.
  const [testedIssuer, setTestedIssuer] = useState<string | null>(null)

  const load = async () => {
    setLoading(true)
    try {
      const [pu, m, list] = await Promise.all([
        authConfigApi.getPublicUrl(),
        authConfigApi.getMethod(),
        ssoApi.list(),
      ])
      setPublicUrl(pu ?? '')
      setMethod(m)
      setProviders(list)
    } catch (e: any) {
      message.error(formatApiError(e, 'common.error'))
    } finally { setLoading(false) }
  }
  useEffect(() => { load() /* eslint-disable-next-line */ }, [])

  const openCreate = (tpl?: ProviderTemplate) => {
    setEditing(null)
    form.resetFields()
    setTestedIssuer(null)
    if (tpl) {
      const { issuerHintKey: hintKey, ...rest } = tpl.defaults
      form.setFieldsValue(rest)
      setIssuerHint(hintKey ? t(hintKey) : '')
    } else {
      setIssuerHint('')
    }
    setModalOpen(true)
  }

  const openEdit = (row: SSOProviderAdmin) => {
    setEditing(row)
    // Edit flow: trust the server-stored issuer as already tested. The
    // banner will only re-warn if the operator changes the field.
    setTestedIssuer(row.issuer)
    form.resetFields()
    form.setFieldsValue({
      name: row.name, displayName: row.displayName, enabled: row.enabled,
      issuer: row.issuer, clientId: row.clientId, clientSecret: '',
      scopes: row.scopes, autoCreate: row.autoCreate, defaultRole: row.defaultRole,
      emailDomains: row.emailDomains ?? '', trustUnverifiedEmail: row.trustUnverifiedEmail,
    })
    setIssuerHint('')
    setModalOpen(true)
  }

  const onSubmit = async () => {
    const v = await form.validateFields()
    // B15: warn if the issuer field doesn't match the last successfully
    // tested value. Skip on edit when the issuer wasn't touched (its
    // value already matches what's on the server).
    const currentIssuer = form.getFieldValue('issuer')
    if (currentIssuer && currentIssuer !== testedIssuer) {
      const ok = await new Promise<boolean>((resolve) => {
        modal.confirm({
          title: t('sso.untestedIssuerTitle'),
          content: t('sso.untestedIssuerContent'),
          okText: t('common.save'),
          cancelText: t('common.cancel'),
          onOk: () => resolve(true),
          onCancel: () => resolve(false),
        })
      })
      if (!ok) return
    }
    try {
      if (editing) {
        // PUT: clientSecret '' = keep existing
        await ssoApi.update(editing.id, v)
        message.success(t('common.success'))
      } else {
        await ssoApi.create(v)
        message.success(t('common.success'))
      }
      setModalOpen(false)
      load()
    } catch (e: any) {
      message.error(formatApiError(e, 'common.error'))
    }
  }

  const onDelete = async (row: SSOProviderAdmin) => {
    try {
      await ssoApi.remove(row.id)
      message.success(t('common.success'))
      load()
    } catch (e: any) {
      message.error(formatApiError(e, 'common.error'))
    }
  }

  const onTestIssuer = async () => {
    const issuer = form.getFieldValue('issuer')
    if (!issuer) { message.warning(t('sso.issuerRequired')); return }
    setTesting(true)
    try {
      const r = await ssoApi.test(issuer)
      if (r.ok) {
        setTestedIssuer(issuer)
        modal.success({
          title: t('sso.testOkTitle'),
          content: (
            <Space direction="vertical" size={4} style={{ width: '100%' }}>
              <div><b>auth_endpoint:</b> <Typography.Text code copyable>{r.authUrl}</Typography.Text></div>
              <div><b>token_endpoint:</b> <Typography.Text code copyable>{r.tokenUrl}</Typography.Text></div>
            </Space>
          ),
        })
      } else {
        modal.error({ title: t('sso.testFailTitle'), content: r.error })
      }
    } catch (e: any) {
      modal.error({ title: t('sso.testFailTitle'), content: formatApiError(e) })
    } finally { setTesting(false) }
  }

  const publicUrlReady = !!publicUrl.trim()

  // Section gating lets the parent settings page interleave the
  // three SSO cards (publicUrl / loginMethod / providers) with
  // unrelated cards in arbitrary order. Omitting the prop keeps
  // the legacy behaviour of rendering all three back-to-back.
  const showPublic = !section || section === 'publicUrl'
  const showMethod = !section || section === 'loginMethod'
  const showProviders = !section || section === 'providers'

  return (
    <>
      {showPublic && (
      <>
      {/* ---- Public URL ---- */}
      <Card id="sso-public-url" title={t('sso.publicUrlTitle')} style={{ maxWidth: 720, marginBottom: 16 }}>
        <Alert type="info" showIcon style={{ marginBottom: 12 }} message={t('sso.publicUrlDesc')} />
        <Form layout="vertical">
          <Form.Item label={t('sso.publicUrlLabel')} extra={t('sso.publicUrlHelp')}>
            <Input value={publicUrl} onChange={(e) => setPublicUrl(e.target.value)}
              placeholder="https://taps.example.com" />
          </Form.Item>
          <Button type="primary" loading={publicUrlSaving} onClick={savePublicUrl}>{t('common.save')}</Button>
        </Form>
      </Card>
      </>
      )}

      {showMethod && (
      <>
      {/* ---- Login method ---- */}
      <Card title={t('sso.loginMethodTitle')} style={{ maxWidth: 720, marginBottom: 16 }}>
        <Alert type="info" showIcon style={{ marginBottom: 12 }} message={t('sso.loginMethodDesc')} />
        <Radio.Group value={method} onChange={(e) => saveMethod(e.target.value)} disabled={methodSaving}>
          <Space direction="vertical">
            <Radio value="password-only">{t('sso.methodPasswordOnly')}</Radio>
            <Radio value="oidc+password">{t('sso.methodOIDCPassword')}</Radio>
            <Radio value="oidc-only">{t('sso.methodOIDCOnly')}</Radio>
          </Space>
        </Radio.Group>
        {method === 'oidc-only' && (
          <Alert type="warning" showIcon style={{ marginTop: 12 }} message={t('sso.methodOIDCOnlyWarn')} />
        )}
      </Card>
      </>
      )}

      {showProviders && (
      <>
      {/* ---- Providers ---- */}
      <Card
        title={t('sso.providersTitle')}
        style={{ maxWidth: 720, marginBottom: 16 }}
        extra={
          <Space>
            <Button icon={<ReloadOutlined />} onClick={load} loading={loading}>{t('common.refresh')}</Button>
            <Dropdown
              disabled={!publicUrlReady}
              menu={{
                items: TEMPLATES.map(tpl => ({
                  key: tpl.id,
                  label: t(tpl.templateNameKey),
                  onClick: () => openCreate(tpl),
                })),
              }}
            >
              <Button type="primary" icon={<PlusOutlined />}>
                {t('sso.addProvider')}
              </Button>
            </Dropdown>
          </Space>
        }
      >
        {!publicUrlReady && (
          <Alert type="warning" showIcon style={{ marginBottom: 12 }} message={t('sso.publicUrlRequired')} />
        )}
        <Table<SSOProviderAdmin>
          rowKey="id"
          dataSource={providers}
          loading={loading}
          pagination={false}
          columns={[
            { title: t('sso.colName'), dataIndex: 'displayName',
              render: (_v, r) => <Space direction="vertical" size={0}>
                <span style={{ fontWeight: 500 }}>{r.displayName}</span>
                <Typography.Text type="secondary" style={{ fontSize: 11 }}>{r.name}</Typography.Text>
              </Space>,
            },
            { title: 'Issuer', dataIndex: 'issuer', ellipsis: true,
              render: (v: string) => <Typography.Text code style={{ fontSize: 11 }}>{v}</Typography.Text>,
            },
            { title: t('sso.colEnabled'), dataIndex: 'enabled', width: 80,
              render: (v: boolean) => v ? <Tag color="green">{t('common.yes')}</Tag> : <Tag>{t('common.no')}</Tag>,
            },
            { title: t('sso.colDefaultRole'), dataIndex: 'defaultRole', width: 100,
              render: (v: string) => <Tag color={v === 'admin' ? 'red' : 'blue'}>{v}</Tag>,
            },
            { title: t('sso.colAutoCreate'), dataIndex: 'autoCreate', width: 100,
              render: (v: boolean) => v ? <Tag color="orange">{t('common.yes')}</Tag> : <Tag>{t('common.no')}</Tag>,
            },
            { title: t('common.actions'), width: 160, align: 'right',
              render: (_v, r) => (
                <Space size={4}>
                  <Button size="small" onClick={() => openEdit(r)}>{t('common.edit')}</Button>
                  <Popconfirm title={t('sso.confirmDelete')} onConfirm={() => onDelete(r)}>
                    <Button size="small" danger>{t('common.delete')}</Button>
                  </Popconfirm>
                </Space>
              ),
            },
          ]}
        />
      </Card>
      </>
      )}

      {/* ---- Create/Edit modal ---- */}
      <Modal
        title={editing ? t('sso.editProvider') : t('sso.addProvider')}
        open={modalOpen}
        onCancel={() => setModalOpen(false)}
        onOk={onSubmit}
        destroyOnClose
        width={680}
      >
        <CallbackUrlBanner editing={editing} publicUrl={publicUrl} form={form} />
        <Form form={form} layout="vertical">
          {!editing && (
            <Form.Item name="name" label={t('sso.fieldName')} extra={t('sso.fieldNameHelp')} rules={[{ required: true, pattern: /^[a-z0-9_-]{1,64}$/, message: t('sso.fieldNameInvalid') }]}>
              <Input placeholder="logto" />
            </Form.Item>
          )}
          <Form.Item name="displayName" label={t('sso.fieldDisplayName')} rules={[{ required: true }]}>
            <Input placeholder="Logto" />
          </Form.Item>
          <Form.Item name="issuer" label="Issuer URL" extra={issuerHint || t('sso.fieldIssuerHelp')}
            rules={[{ required: true, pattern: /^https?:\/\//, message: 'http(s)://...' }]}>
            <Input placeholder="https://your-logto.example.com/oidc" />
          </Form.Item>
          <Form.Item>
            <Button onClick={onTestIssuer} loading={testing}>{t('sso.testIssuer')}</Button>
          </Form.Item>
          <Form.Item name="clientId" label="Client ID" rules={[{ required: true }]}>
            <Input />
          </Form.Item>
          <Form.Item name="clientSecret" label="Client Secret"
            extra={editing ? t('sso.clientSecretEditHelp') : ''}
            rules={[{ required: !editing, message: t('sso.clientSecretRequired') }]}>
            <Input.Password placeholder={editing ? t('sso.clientSecretEditPlaceholder') : ''} />
          </Form.Item>
          <Form.Item name="scopes" label="Scopes" extra={t('sso.scopesHelp')}>
            <Input placeholder="openid profile email" />
          </Form.Item>
          <Form.Item name="emailDomains" label={t('sso.fieldEmailDomains')} extra={t('sso.emailDomainsHelp')}>
            <Input placeholder="yingxi.me, sub.yingxi.me" />
          </Form.Item>
          <Form.Item name="defaultRole" label={t('sso.fieldDefaultRole')} extra={t('sso.defaultRoleHelp')}>
            <Select options={[{ value: 'user', label: 'user' }, { value: 'admin', label: 'admin' }]} />
          </Form.Item>
          <Form.Item name="autoCreate" label={t('sso.fieldAutoCreate')} valuePropName="checked" extra={t('sso.autoCreateHelp')}>
            <Switch />
          </Form.Item>
          <Form.Item name="trustUnverifiedEmail" label={t('sso.fieldTrustUnverifiedEmail')} valuePropName="checked" extra={t('sso.trustUnverifiedEmailHelp')}>
            <Switch />
          </Form.Item>
          {editing && (
            <Form.Item name="enabled" label={t('common.enabled')} valuePropName="checked">
              <Switch />
            </Form.Item>
          )}
        </Form>
      </Modal>
    </>
  )
}

// CallbackUrlBanner sits above the form so the operator can copy the
// redirect URI into their IdP *before* filling out the rest. For new
// providers we compute it live from publicUrl + the slug being typed;
// for edits we trust the server-rendered value (handles edge cases
// like trailing slashes in publicUrl).
function CallbackUrlBanner({
  editing, publicUrl, form,
}: {
  editing: SSOProviderAdmin | null
  publicUrl: string
  form: ReturnType<typeof Form.useForm<SSOProviderInput>>[0]
}) {
  const { t } = useTranslation()
  const watchedName = Form.useWatch('name', form)

  // Edit flow trusts the server-rendered callbackUrl (handles trailing
  // slashes etc.). Skip all client-side validation in that case.
  if (editing) {
    return (
      <Alert
        type="info"
        showIcon
        style={{ marginBottom: 16 }}
        message={
          <Space direction="vertical" size={2} style={{ width: '100%' }}>
            <span>{t('sso.callbackUrlHint')}</span>
            <Typography.Text code copyable style={{ fontSize: 12, wordBreak: 'break-all' }}>{editing.callbackUrl}</Typography.Text>
          </Space>
        }
      />
    )
  }

  // Create flow: refuse to render a callback URL until publicUrl is a
  // well-formed origin. Without this check the banner would happily
  // splice "example.com/api/oauth/callback/foo" or "https://taps.com/path/api/..."
  // and the operator would copy that into the IdP's redirect_uri box,
  // only to find the actual callback (server-side built from a
  // different normalisation) doesn't match.
  if (!publicUrl) return null
  let parsed: URL | null = null
  try {
    parsed = new URL(publicUrl)
  } catch {
    parsed = null
  }
  const validScheme = parsed?.protocol === 'http:' || parsed?.protocol === 'https:'
  const validPath = parsed && (parsed.pathname === '' || parsed.pathname === '/')
  const validHost = !!parsed?.host
  if (!parsed || !validScheme || !validPath || !validHost) {
    return (
      <Alert
        type="error"
        showIcon
        style={{ marginBottom: 16 }}
        message={t('sso.publicUrlMalformed')}
      />
    )
  }
  if (!watchedName) return null
  const base = publicUrl.replace(/\/+$/, '')
  const url = `${base}/api/oauth/callback/${watchedName}`
  return (
    <Alert
      type="info"
      showIcon
      style={{ marginBottom: 16 }}
      message={
        <Space direction="vertical" size={2} style={{ width: '100%' }}>
          <span>{t('sso.callbackUrlHint')}</span>
          <Typography.Text code copyable style={{ fontSize: 12, wordBreak: 'break-all' }}>{url}</Typography.Text>
        </Space>
      }
    />
  )
}
