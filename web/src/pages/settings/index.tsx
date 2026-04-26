import { useEffect, useState } from 'react'
import { Card, Form, Input, InputNumber, Switch, Button, App, Space, Alert, Upload, Image, Radio, Tooltip } from 'antd'
import { UploadOutlined, DeleteOutlined } from '@ant-design/icons'
import { useTranslation } from 'react-i18next'
import { api } from '@/api/client'
import PageHeader from '@/components/PageHeader'
import { useBrandStore } from '@/stores/brand'
import SettingsSSO from './SettingsSSO'
import { formatApiError } from '@/api/errors'
import { waitFor } from '@/utils/waitFor'

interface HibSettings {
  defaultEnabled: boolean
  defaultMinutes: number
  warmupMinutes: number
  motd: string
  kickMessage: string
  hasIcon: boolean
}

interface CaptchaSettings {
  provider: 'none' | 'recaptcha' | 'turnstile'
  siteKey: string
  // hasSecret reports whether the panel already has a stored secret;
  // GET never returns the secret itself (encrypted at rest, audit N3).
  // PUT may send `secret` to install / rotate; empty string keeps the
  // existing one — same pattern as SSO clientSecret.
  hasSecret?: boolean
  secret: string
  scoreThreshold: number
}

interface LogLimits {
  auditMaxRows: number
  loginMaxRows: number
}

interface RateLimit {
  rateLimitPerMin: number
  banDurationMinutes: number
  oauthStartCount: number
  oauthStartWindowSec: number
  pkceStoreMaxEntries: number
  terminalReadDeadlineSec: number
  terminalInputRatePerSec: number
  terminalInputBurst: number
  iconCacheMaxAgeSec: number
  iconRatePerMin: number
}

interface RequestLimits {
  maxJsonBodyBytes: number
  maxWsFrameBytes: number
  maxRequestBodyBytes: number
}

interface AuthTimings {
  jwtTtlMinutes: number
  wsHeartbeatMinutes: number
}

interface PanelPort {
  port: number
}

interface HttpTimeouts {
  readHeaderTimeoutSec: number
  readTimeoutSec: number
  writeTimeoutSec: number
  idleTimeoutSec: number
}

interface TrustedProxies {
  proxies: string
}

export default function SettingsPage() {
  const { t } = useTranslation()
  const { message, modal } = App.useApp()
  const [url, setUrl] = useState('')
  // Audit-2026-04-24-v3 H1: webhook URL is validated against private/
  // loopback hosts on save unless this override is on. The override
  // and the URL travel together in a single PUT (`SetWebhook` saves
  // allowPrivate first so the same call's URL validation sees the
  // new value).
  const [webhookAllowPrivate, setWebhookAllowPrivate] = useState(false)
  // Mirror of the panel's system.publicUrl. Used by the M3 onboarding
  // banner above; empty value means SSO/terminal-Origin defenses can't
  // engage and admin needs to fix it. `publicUrlLoaded` gates the
  // banner so it doesn't flash for the ~half-second between mount
  // (publicUrl='' default) and the GET response landing — without it,
  // every refresh briefly shows "未配置" even when a value is already
  // saved.
  const [publicUrl, setPublicUrl] = useState('')
  const [publicUrlLoaded, setPublicUrlLoaded] = useState(false)
  const [loading, setLoading] = useState(false)
  const [hib, setHib] = useState<HibSettings>({ defaultEnabled: true, defaultMinutes: 60, warmupMinutes: 5, motd: '', kickMessage: '', hasIcon: false })
  const [hibSaving, setHibSaving] = useState(false)
  const [iconKey, setIconKey] = useState(0)
  const [deploySource, setDeploySource] = useState<'fastmirror' | 'official'>('fastmirror')
  const [deploySaving, setDeploySaving] = useState(false)
  const [captcha, setCaptcha] = useState<CaptchaSettings>({ provider: 'none', siteKey: '', secret: '', scoreThreshold: 0.5 })
  const [captchaSaving, setCaptchaSaving] = useState(false)
  // audit-2026-04-25 H1: track the provider as last loaded from the
  // server. When the operator changes the Select to a different
  // provider, the previously-stored secret is meaningless under the
  // new provider — we hide the "leave blank to keep" placeholder and
  // require a fresh secret. Mirrors the server-side gate that
  // refuses the PUT with settings.captcha_secret_required_on_provider_change.
  const [captchaInitialProvider, setCaptchaInitialProvider] = useState<'none' | 'recaptcha' | 'turnstile'>('none')
  const captchaProviderChanged = captcha.provider !== 'none' && captcha.provider !== captchaInitialProvider
  // Save is gated on a successful "Test connectivity" for the *current*
  // values. We snapshot the config at test-pass time as a JSON string;
  // any subsequent edit invalidates the pass (snapshot != current) and
  // re-disables Save until the admin tests again. provider=none never
  // needs to test.
  const [captchaPassedFor, setCaptchaPassedFor] = useState<string>('')
  const captchaSnapshot = JSON.stringify(captcha)
  const captchaSaveAllowed = captcha.provider === 'none' || captchaPassedFor === captchaSnapshot

  const reloadBrand = useBrandStore((s) => s.load)
  const brandSiteName = useBrandStore((s) => s.siteName)
  const brandHasFavicon = useBrandStore((s) => s.hasFavicon)
  const [siteNameDraft, setSiteNameDraft] = useState('')
  const [siteNameSaving, setSiteNameSaving] = useState(false)
  const [faviconKey, setFaviconKey] = useState(0)
  useEffect(() => { setSiteNameDraft(brandSiteName) }, [brandSiteName])

  const [logLimits, setLogLimits] = useState<LogLimits>({ auditMaxRows: 1_000_000, loginMaxRows: 1_000_000 })
  const [logLimitsSaving, setLogLimitsSaving] = useState(false)
  const saveLogLimits = async () => {
    setLogLimitsSaving(true)
    try {
      await api.put('/settings/log-limits', logLimits)
      message.success(t('common.success'))
    } catch (e: any) {
      message.error(formatApiError(e, 'common.error'))
    } finally { setLogLimitsSaving(false) }
  }

  const [rateLimit, setRateLimit] = useState<RateLimit>({
    rateLimitPerMin: 5, banDurationMinutes: 5,
    oauthStartCount: 30, oauthStartWindowSec: 300,
    pkceStoreMaxEntries: 10000,
    terminalReadDeadlineSec: 60, terminalInputRatePerSec: 200, terminalInputBurst: 50,
    iconCacheMaxAgeSec: 300, iconRatePerMin: 10,
  })
  const [rateLimitSaving, setRateLimitSaving] = useState(false)
  const saveRateLimit = async () => {
    setRateLimitSaving(true)
    try {
      await api.put('/settings/rate-limit', rateLimit)
      message.success(t('common.success'))
    } catch (e: any) {
      message.error(formatApiError(e, 'common.error'))
    } finally { setRateLimitSaving(false) }
  }

  const MIB = 1024 * 1024
  const KIB = 1024
  const [reqLimits, setReqLimits] = useState<RequestLimits>({ maxJsonBodyBytes: 16 * MIB, maxWsFrameBytes: 16 * MIB, maxRequestBodyBytes: 128 * KIB })
  const [reqLimitsSaving, setReqLimitsSaving] = useState(false)
  const saveReqLimits = async () => {
    setReqLimitsSaving(true)
    try {
      await api.put('/settings/limits', reqLimits)
      message.success(t('common.success'))
    } catch (e: any) {
      message.error(formatApiError(e, 'common.error'))
    } finally { setReqLimitsSaving(false) }
  }

  const [authTimings, setAuthTimings] = useState<AuthTimings>({ jwtTtlMinutes: 60, wsHeartbeatMinutes: 5 })
  const [authTimingsSaving, setAuthTimingsSaving] = useState(false)
  const saveAuthTimings = async () => {
    setAuthTimingsSaving(true)
    try {
      await api.put('/settings/auth-timings', authTimings)
      message.success(t('common.success'))
    } catch (e: any) {
      message.error(formatApiError(e, 'common.error'))
    } finally { setAuthTimingsSaving(false) }
  }

  // Panel listen port — DB > env > 24444. Saving here only writes the
  // DB row; the running process can't rebind, so we show a "restart
  // required" notice and the new port takes effect on next start.
  const [panelPort, setPanelPort] = useState<number>(24444)
  const [panelPortSaving, setPanelPortSaving] = useState(false)
  const savePanelPort = async () => {
    setPanelPortSaving(true)
    try {
      await api.put('/settings/panel-port', { port: panelPort })
      modal.warning({
        title: t('settings.panelPortSavedTitle'),
        content: t('settings.panelPortSavedHint', { port: panelPort }),
        okText: t('common.ok'),
      })
    } catch (e: any) {
      message.error(formatApiError(e, 'common.error'))
    } finally { setPanelPortSaving(false) }
  }

  // audit-2026-04-25 MED5: HTTP slow-loris timeouts. Same restart-
  // required UX as panelPort — http.Server's timeout fields are
  // immutable once Listen is called.
  const [httpTimeouts, setHttpTimeouts] = useState<HttpTimeouts>({
    readHeaderTimeoutSec: 10,
    readTimeoutSec: 60,
    writeTimeoutSec: 120,
    idleTimeoutSec: 120,
  })
  const [httpTimeoutsSaving, setHttpTimeoutsSaving] = useState(false)
  const saveHttpTimeouts = async () => {
    setHttpTimeoutsSaving(true)
    try {
      await api.put('/settings/http-timeouts', httpTimeouts)
      modal.warning({
        title: t('settings.httpTimeoutsSavedTitle'),
        content: t('settings.httpTimeoutsSavedHint'),
        okText: t('common.ok'),
      })
    } catch (e: any) {
      message.error(formatApiError(e, 'common.error'))
    } finally { setHttpTimeoutsSaving(false) }
  }

  // Trusted reverse-proxy IPs — see comment in cmd/panel/main.go.
  // Lives in DB so admins can add their nginx host without redeploy.
  // Takes effect on next process start.
  const [trustedProxies, setTrustedProxies] = useState<string>('127.0.0.1,::1')
  const [trustedProxiesSaving, setTrustedProxiesSaving] = useState(false)
  const saveTrustedProxies = async () => {
    setTrustedProxiesSaving(true)
    try {
      await api.put('/settings/trusted-proxies', { proxies: trustedProxies })
      modal.warning({
        title: t('settings.trustedProxiesSavedTitle'),
        content: t('settings.trustedProxiesSavedHint'),
        okText: t('common.ok'),
      })
    } catch (e: any) {
      message.error(formatApiError(e, 'common.error'))
    } finally { setTrustedProxiesSaving(false) }
  }

  // CORS allowed origins (B23). Empty = panel falls back to its own
  // PublicURL only (the SPA on the same origin always passes).
  const [corsOrigins, setCorsOrigins] = useState<string>('')
  const [corsOriginsSaving, setCorsOriginsSaving] = useState(false)
  const saveCorsOrigins = async () => {
    setCorsOriginsSaving(true)
    try {
      await api.put('/settings/cors-origins', { origins: corsOrigins })
      message.success(t('common.success'))
    } catch (e: any) {
      message.error(formatApiError(e, 'common.error'))
    } finally { setCorsOriginsSaving(false) }
  }

  // CSP domain allowlists (audit-2026-04-25 VULN-001)
  const [cspScriptSrc, setCspScriptSrc] = useState('https://challenges.cloudflare.com,https://www.recaptcha.net')
  const [cspFrameSrc, setCspFrameSrc] = useState('https://challenges.cloudflare.com,https://www.google.com,https://www.recaptcha.net')
  const [cspSaving, setCspSaving] = useState(false)
  const saveCSP = async () => {
    setCspSaving(true)
    try {
      await api.put('/settings/csp', { scriptSrcExtra: cspScriptSrc, frameSrcExtra: cspFrameSrc })
      message.success(t('common.success'))
    } catch (e: any) {
      message.error(formatApiError(e, 'common.error'))
    } finally { setCspSaving(false) }
  }

  const saveSiteName = async () => {
    const name = siteNameDraft.trim()
    if (!siteNameValid(name)) {
      message.error(t('settings.brandSiteNameInvalid'))
      return
    }
    if (siteNameWeight(name) > 16) {
      message.error(t('settings.brandSiteNameTooLong'))
      return
    }
    setSiteNameSaving(true)
    try {
      await api.put('/settings/brand/site-name', { siteName: name })
      message.success(t('common.success'))
      await reloadBrand()
    } catch (e: any) {
      message.error(formatApiError(e, 'common.error'))
    } finally { setSiteNameSaving(false) }
  }

  const load = async () => {
    try {
      const r = await api.get<{ url: string; allowPrivate: boolean }>('/settings/webhook')
      setUrl(r.data.url ?? '')
      setWebhookAllowPrivate(!!r.data.allowPrivate)
    } catch { /* ignore */ }
    try { setHib((await api.get<HibSettings>('/settings/hibernation')).data) } catch { /* ignore */ }
    try { setDeploySource((await api.get<{ source: 'fastmirror' | 'official' }>('/settings/deploy-source')).data.source) } catch { /* ignore */ }
    try {
      // GET returns { provider, siteKey, hasSecret, scoreThreshold } —
      // server never echoes back the captcha secret (encrypted at
      // rest, audit N3). Set our local `secret` to '' so the input
      // renders as the "leave blank to keep" placeholder; only when
      // the operator types something does it ride along on PUT.
      const r = await api.get<CaptchaSettings>('/settings/captcha')
      setCaptcha({ ...r.data, secret: '' })
      setCaptchaInitialProvider(r.data.provider)
    } catch { /* ignore */ }
    try { setLogLimits((await api.get<LogLimits>('/settings/log-limits')).data) } catch { /* ignore */ }
    try { setRateLimit((await api.get<RateLimit>('/settings/rate-limit')).data) } catch { /* ignore */ }
    try { setReqLimits((await api.get<RequestLimits>('/settings/limits')).data) } catch { /* ignore */ }
    try { setAuthTimings((await api.get<AuthTimings>('/settings/auth-timings')).data) } catch { /* ignore */ }
    try {
      const r = await api.get<PanelPort>('/settings/panel-port')
      if (r.data?.port && r.data.port > 0) setPanelPort(r.data.port)
    } catch { /* ignore */ }
    try {
      const r = await api.get<HttpTimeouts>('/settings/http-timeouts')
      if (r.data) setHttpTimeouts(r.data)
    } catch { /* ignore */ }
    try {
      const r = await api.get<TrustedProxies>('/settings/trusted-proxies')
      if (r.data?.proxies) setTrustedProxies(r.data.proxies)
    } catch { /* ignore */ }
    try {
      const r = await api.get<{ origins: string }>('/settings/cors-origins')
      setCorsOrigins(r.data?.origins ?? '')
    } catch { /* ignore */ }
    try {
      const r = await api.get<{ scriptSrcExtra: string; frameSrcExtra: string }>('/settings/csp')
      if (r.data) {
        setCspScriptSrc(r.data.scriptSrcExtra ?? '')
        setCspFrameSrc(r.data.frameSrcExtra ?? '')
      }
    } catch { /* ignore */ }
    try {
      const r = await api.get<{ url: string }>('/settings/panel-public-url')
      setPublicUrl(r.data?.url ?? '')
    } catch { /* ignore */ }
    finally { setPublicUrlLoaded(true) }
  }
  useEffect(() => { load() }, [])

  const save = async () => {
    setLoading(true)
    try { await api.put('/settings/webhook', { url, allowPrivate: webhookAllowPrivate }); message.success(t('common.success')) }
    catch (e: any) { message.error(formatApiError(e, 'common.error')) }
    finally { setLoading(false) }
  }

  const test = async () => {
    try { await api.post('/settings/webhook/test'); message.success(t('settings.webhookTestSent')) }
    catch (e: any) { message.error(formatApiError(e, 'common.error')) }
  }

  const saveDeploySource = async (v: 'fastmirror' | 'official') => {
    setDeploySource(v)
    setDeploySaving(true)
    try {
      await api.put('/settings/deploy-source', { source: v })
      message.success(t('common.success'))
    } catch (e: any) {
      message.error(formatApiError(e, 'common.error'))
    } finally { setDeploySaving(false) }
  }

  const saveCaptcha = async () => {
    setCaptchaSaving(true)
    try {
      await api.put('/settings/captcha', captcha)
      setCaptchaInitialProvider(captcha.provider)
      message.success(t('common.success'))
    } catch (e: any) {
      message.error(formatApiError(e, 'common.error'))
    } finally { setCaptchaSaving(false) }
  }

  const [captchaTesting, setCaptchaTesting] = useState(false)
  const testCaptcha = async () => {
    setCaptchaTesting(true)
    try {
      // Real-token mode: ask the configured widget for a fresh token,
      // then send it to the backend Test which can fully verify both
      // site key + secret. This is the only reliable way to validate
      // reCAPTCHA — Google's siteverify rejects fake tokens before it
      // even checks the secret. Turnstile also benefits because hostname
      // mismatches are caught at render time.
      let token = ''
      let action = 'tapstest'
      try {
        if (captcha.provider === 'turnstile') {
          token = await getTurnstileToken(captcha.siteKey)
        } else if (captcha.provider === 'recaptcha') {
          token = await getRecaptchaToken(captcha.siteKey, action)
        }
      } catch (e: any) {
        const msgKey = captcha.provider === 'recaptcha'
          ? 'settings.captchaTestSiteKeyFailRecaptcha'
          : 'settings.captchaTestSiteKeyFailTurnstile'
        message.error(`${t(msgKey)}: ${e?.message || 'render rejected'}`)
        return
      }
      const r = await api.post<{ ok: boolean; reason?: string }>('/settings/captcha/test', { ...captcha, token, action })
      if (r.data.ok) {
        message.success(t('settings.captchaTestOk'))
        setCaptchaPassedFor(JSON.stringify(captcha))
      } else {
        message.error(`${t('settings.captchaTestFail')}: ${r.data.reason}`)
      }
    } catch (e: any) {
      message.error(formatApiError(e, 'common.error'))
    } finally { setCaptchaTesting(false) }
  }

  const saveHib = async () => {
    if (hib.defaultMinutes < 1 || hib.defaultMinutes > 1440) {
      message.error(t('settings.hibRangeError'))
      return
    }
    setHibSaving(true)
    try {
      await api.put('/settings/hibernation', hib)
      message.success(t('common.success'))
    } catch (e: any) {
      message.error(formatApiError(e, 'common.error'))
    } finally { setHibSaving(false) }
  }

  return (
    <>
      <PageHeader title={t('menu.settings')} subtitle={t('settings.pageSubtitle')} />

      {/* Audit M3 onboarding: publicURL is a hard prerequisite for SSO,
          terminal cross-origin protection, and the CORS fallback. We
          surface a top-of-page banner with a deep link to the SSO card
          when it's empty so an admin can't accidentally deploy a panel
          that opens terminal WS without origin pinning.
          Gate on publicUrlLoaded so the banner doesn't flash on every
          refresh while the GET is in flight. */}
      {publicUrlLoaded && !publicUrl && (
        <Alert
          type="warning"
          showIcon
          style={{ marginBottom: 16 }}
          message={t('settings.publicUrlMissingTitle')}
          description={
            <>
              {t('settings.publicUrlMissingDesc')}{' '}
              <a href="#sso-public-url">{t('settings.publicUrlMissingJump')}</a>
            </>
          }
        />
      )}

      <Card title={t('settings.brandTitle')} style={{ maxWidth: 720, marginBottom: 16 }}>
        <Alert type="info" showIcon style={{ marginBottom: 16 }} message={t('settings.brandDesc')} />
        <Form layout="vertical">
          <Form.Item label={t('settings.brandSiteName')}
            extra={
              <span>
                {t('settings.brandSiteNameHelp')} ({t('settings.brandSiteNameWeight', { used: siteNameWeight(siteNameDraft), max: 16 })})
              </span>
            }>
            <Space.Compact style={{ width: '100%' }}>
              <Input value={siteNameDraft}
                onChange={(e) => setSiteNameDraft(filterSiteName(e.target.value))}
                placeholder="TapS" />
              <Button type="primary" loading={siteNameSaving} onClick={saveSiteName}
                disabled={!siteNameDraft.trim() || siteNameWeight(siteNameDraft.trim()) > 16}>{t('common.save')}</Button>
            </Space.Compact>
          </Form.Item>
          <Form.Item label={t('settings.brandFavicon')} extra={t('settings.brandFaviconHelp')}>
            <Space align="start">
              {brandHasFavicon && (
                <img
                  src={`/api/brand/favicon?_=${faviconKey}`}
                  width={32} height={32}
                  style={{ border: '1px solid var(--taps-border)', borderRadius: 4 }}
                />
              )}
              <Upload
                accept="image/png,image/x-icon,.ico"
                showUploadList={false}
                customRequest={async ({ file, onSuccess, onError }) => {
                  const fd = new FormData()
                  fd.append('file', file as Blob)
                  try {
                    await api.post('/settings/brand/favicon', fd)
                    message.success(t('common.success'))
                    setFaviconKey(k => k + 1)
                    await reloadBrand()
                    onSuccess?.({}, new XMLHttpRequest())
                  } catch (e: any) {
                    message.error(formatApiError(e, 'common.error'))
                    onError?.(e)
                  }
                }}
              >
                <Button icon={<UploadOutlined />}>{t('settings.brandFaviconUpload')}</Button>
              </Upload>
              {brandHasFavicon && (
                <Button icon={<DeleteOutlined />} danger onClick={async () => {
                  try {
                    await api.delete('/settings/brand/favicon')
                    setFaviconKey(k => k + 1)
                    await reloadBrand()
                  } catch (e: any) { message.error(formatApiError(e, 'common.error')) }
                }}>{t('common.delete')}</Button>
              )}
            </Space>
          </Form.Item>
        </Form>
      </Card>

      {/* Panel 公开地址 */}
      <SettingsSSO section="publicUrl" onPublicUrlSaved={(u) => setPublicUrl(u)} />

      <Card title={t('settings.panelPortTitle')} style={{ maxWidth: 720, marginBottom: 16 }}>
        <Alert type="warning" showIcon style={{ marginBottom: 16 }} message={t('settings.panelPortDesc')} />
        <Form layout="vertical">
          <Form.Item label={t('settings.panelPortLabel')} extra={t('settings.panelPortHelp')}>
            <InputNumber min={1024} max={65535} value={panelPort}
              onChange={(v) => setPanelPort(typeof v === 'number' ? v : 24444)}
              style={{ width: 240 }} />
          </Form.Item>
          <Button type="primary" loading={panelPortSaving} onClick={savePanelPort}>{t('common.save')}</Button>
        </Form>
      </Card>

      <Card title={t('settings.trustedProxiesTitle')} style={{ maxWidth: 720, marginBottom: 16 }}>
        <Alert type="warning" showIcon style={{ marginBottom: 16 }} message={t('settings.trustedProxiesDesc')} />
        <Form layout="vertical">
          <Form.Item label={t('settings.trustedProxiesLabel')} extra={t('settings.trustedProxiesHelp')}>
            <Input.TextArea
              value={trustedProxies}
              onChange={(e) => setTrustedProxies(e.target.value)}
              autoSize={{ minRows: 2, maxRows: 6 }}
              placeholder="127.0.0.1, ::1, 10.0.0.0/8"
              style={{ fontFamily: 'monospace' }}
            />
          </Form.Item>
          <Button type="primary" loading={trustedProxiesSaving} onClick={saveTrustedProxies}>{t('common.save')}</Button>
        </Form>
      </Card>

      <Card title={t('settings.corsOriginsTitle')} style={{ maxWidth: 720, marginBottom: 16 }}>
        <Alert type="info" showIcon style={{ marginBottom: 16 }} message={t('settings.corsOriginsDesc')} />
        <Form layout="vertical">
          <Form.Item label={t('settings.corsOriginsLabel')} extra={t('settings.corsOriginsHelp')}>
            <Input.TextArea
              value={corsOrigins}
              onChange={(e) => setCorsOrigins(e.target.value)}
              autoSize={{ minRows: 2, maxRows: 6 }}
              placeholder="https://taps.example.com, https://other.example.com"
              style={{ fontFamily: 'monospace' }}
            />
          </Form.Item>
          <Button type="primary" loading={corsOriginsSaving} onClick={saveCorsOrigins}>{t('common.save')}</Button>
        </Form>
      </Card>

      <Card title={t('settings.captchaTitle')} style={{ maxWidth: 720, marginBottom: 16 }}>
        <Alert type="info" showIcon style={{ marginBottom: 8 }} message={t('settings.captchaDesc')} />
        <Alert type="warning" showIcon style={{ marginBottom: 16 }} message={t('settings.captchaFailOpenNote')} />
        <Form layout="vertical">
          <Form.Item label={t('settings.captchaProvider')}>
            <Radio.Group value={captcha.provider} onChange={(e) => setCaptcha(s => ({ ...s, provider: e.target.value, siteKey: '', secret: '', hasSecret: false }))}>
              <Radio value="none">{t('settings.captchaNone')}</Radio>
              <Radio value="turnstile">Cloudflare Turnstile</Radio>
              <Radio value="recaptcha">reCAPTCHA Enterprise</Radio>
            </Radio.Group>
          </Form.Item>
          {captchaProviderChanged && (
            <Alert type="warning" showIcon style={{ marginBottom: 16 }}
              message={t('settings.captchaProviderChangedTitle')}
              description={t('settings.captchaProviderChangedDesc')} />
          )}
          {captcha.provider === 'turnstile' && (
            <>
              <Form.Item label={t('settings.captchaSiteKey')} extra={t('settings.captchaSiteKeyHelp')}>
                <Input value={captcha.siteKey} onChange={(e) => setCaptcha(s => ({ ...s, siteKey: e.target.value }))} placeholder="0x..." />
              </Form.Item>
              <Form.Item label={t('settings.captchaSecret')} extra={t('settings.captchaSecretHelp')}>
                <Input.Password value={captcha.secret}
                  placeholder={!captchaProviderChanged && captcha.hasSecret ? t('settings.captchaSecretKeepPh') : ''}
                  onChange={(e) => setCaptcha(s => ({ ...s, secret: e.target.value }))} />
              </Form.Item>
            </>
          )}
          {captcha.provider === 'recaptcha' && (
            <>
              <Form.Item label={t('settings.captchaRecaptchaKey')} extra={t('settings.captchaRecaptchaKeyHelp')}>
                <Input value={captcha.siteKey} onChange={(e) => setCaptcha(s => ({ ...s, siteKey: e.target.value }))}
                  placeholder={t('settings.captchaRecaptchaKeyPh')} />
              </Form.Item>
              <Form.Item label={t('settings.captchaRecaptchaSecret')} extra={t('settings.captchaRecaptchaSecretHelp')}>
                <Input.Password value={captcha.secret}
                  placeholder={!captchaProviderChanged && captcha.hasSecret ? t('settings.captchaSecretKeepPh') : ''}
                  onChange={(e) => setCaptcha(s => ({ ...s, secret: e.target.value }))} />
              </Form.Item>
              <Form.Item label={t('settings.captchaScore')} extra={t('settings.captchaScoreHelp')}>
                <InputNumber min={0.1} max={0.9} step={0.05} value={captcha.scoreThreshold}
                  onChange={(v) => setCaptcha(s => ({ ...s, scoreThreshold: typeof v === 'number' ? v : 0.5 }))} style={{ width: 200 }} />
              </Form.Item>
            </>
          )}
          <Space>
            <Tooltip title={captchaSaveAllowed ? '' : t('settings.captchaTestRequired')}>
              <Button type="primary" loading={captchaSaving} disabled={!captchaSaveAllowed} onClick={saveCaptcha}>{t('common.save')}</Button>
            </Tooltip>
            {captcha.provider !== 'none' && (
              <Button loading={captchaTesting} onClick={testCaptcha}>{t('settings.captchaTest')}</Button>
            )}
          </Space>
        </Form>
      </Card>

      {/* 登录方式 */}
      <SettingsSSO section="loginMethod" />

      {/* SSO 提供商（OIDC） */}
      <SettingsSSO section="providers" />

      <Card title={t('settings.deploySourceTitle')} style={{ maxWidth: 720, marginBottom: 16 }}>
        <Alert type="info" showIcon style={{ marginBottom: 16 }} message={t('settings.deploySourceDesc')} />
        <Radio.Group value={deploySource} onChange={(e) => saveDeploySource(e.target.value)} disabled={deploySaving}>
          <Space direction="vertical">
            <Radio value="fastmirror">
              <strong>FastMirror</strong> — {t('settings.deploySourceFastmirror')}
            </Radio>
            <Radio value="official">
              <strong>{t('settings.deploySourceOfficial')}</strong> — {t('settings.deploySourceOfficialHelp')}
            </Radio>
          </Space>
        </Radio.Group>
      </Card>

      <Card title={t('settings.hibTitle')} style={{ maxWidth: 720, marginBottom: 16 }}>
        <Alert type="info" showIcon style={{ marginBottom: 16 }} message={t('settings.hibDesc')} />
        <Form layout="vertical">
          <Form.Item label={t('settings.hibDefaultEnabled')} extra={t('settings.hibDefaultEnabledHelp')}>
            <Switch checked={hib.defaultEnabled} onChange={(v) => setHib(s => ({ ...s, defaultEnabled: v }))} />
          </Form.Item>
          <Form.Item label={t('settings.hibDefaultMinutes')} extra={t('settings.hibDefaultMinutesHelp')}>
            <InputNumber min={1} max={1440} value={hib.defaultMinutes}
              onChange={(v) => setHib(s => ({ ...s, defaultMinutes: v ?? 60 }))} style={{ width: 200 }} />
          </Form.Item>
          <Form.Item label={t('settings.hibWarmupMinutes')} extra={t('settings.hibWarmupMinutesHelp')}>
            <InputNumber min={0} max={60} value={hib.warmupMinutes}
              onChange={(v) => setHib(s => ({ ...s, warmupMinutes: v ?? 0 }))} style={{ width: 200 }} />
          </Form.Item>
          <Form.Item label={t('settings.hibMOTD')} extra={t('settings.hibColorHint')}>
            <Input value={hib.motd} onChange={(e) => setHib(s => ({ ...s, motd: e.target.value }))} className="taps-mono" />
          </Form.Item>
          <Form.Item label={t('settings.hibKick')} extra={t('settings.hibColorHint')}>
            <Input value={hib.kickMessage} onChange={(e) => setHib(s => ({ ...s, kickMessage: e.target.value }))} className="taps-mono" />
          </Form.Item>
          <Form.Item label={t('settings.hibIcon')} extra={t('settings.hibIconHelp')}>
            <Space align="start">
              {hib.hasIcon && (
                <Image
                  src={`/api/settings/hibernation/icon?_=${iconKey}`}
                  width={64} height={64}
                  style={{ border: '1px solid var(--taps-border)', borderRadius: 8, imageRendering: 'pixelated' }}
                />
              )}
              <Upload
                accept="image/png"
                showUploadList={false}
                customRequest={async ({ file, onSuccess, onError }) => {
                  const fd = new FormData()
                  fd.append('file', file as Blob)
                  try {
                    await api.post('/settings/hibernation/icon', fd)
                    message.success(t('common.success'))
                    setHib(s => ({ ...s, hasIcon: true }))
                    setIconKey(k => k + 1)
                    onSuccess?.({}, new XMLHttpRequest())
                  } catch (e: any) {
                    message.error(formatApiError(e, 'common.error'))
                    onError?.(e)
                  }
                }}
              >
                <Button icon={<UploadOutlined />}>{t('settings.hibIconUpload')}</Button>
              </Upload>
              {hib.hasIcon && (
                <Button icon={<DeleteOutlined />} danger onClick={async () => {
                  try {
                    await api.delete('/settings/hibernation/icon')
                    setHib(s => ({ ...s, hasIcon: false }))
                  } catch (e: any) { message.error(formatApiError(e, 'common.error')) }
                }}>{t('common.delete')}</Button>
              )}
            </Space>
          </Form.Item>
          <Button type="primary" loading={hibSaving} onClick={saveHib}>{t('common.save')}</Button>
        </Form>
      </Card>

      <Card title={t('settings.webhookTitle')} style={{ maxWidth: 720, marginBottom: 16 }}>
        <Alert type="info" showIcon style={{ marginBottom: 16 }} message={t('settings.webhookDesc')} />
        <Form layout="vertical">
          <Form.Item label={t('settings.webhookUrl')}>
            <Input value={url} onChange={(e) => setUrl(e.target.value)} placeholder="https://hooks.slack.com/..." />
          </Form.Item>
          <Form.Item extra={t('settings.webhookAllowPrivateHelp')}>
            <Switch checked={webhookAllowPrivate} onChange={setWebhookAllowPrivate} />
            <span style={{ marginLeft: 8 }}>{t('settings.webhookAllowPrivate')}</span>
          </Form.Item>
          <Space>
            <Button type="primary" loading={loading} onClick={save}>{t('common.save')}</Button>
            <Button onClick={test} disabled={!url}>{t('settings.webhookTest')}</Button>
          </Space>
        </Form>
      </Card>

      <Card title={t('settings.logLimitsTitle')} style={{ maxWidth: 720, marginBottom: 16 }}>
        <Alert type="info" showIcon style={{ marginBottom: 16 }} message={t('settings.logLimitsDesc')} />
        <Form layout="vertical">
          <Form.Item label={t('settings.logLimitsAudit')} extra={t('settings.logLimitsHelp')}>
            <InputNumber min={1000} max={100_000_000} step={10_000} value={logLimits.auditMaxRows}
              onChange={(v) => setLogLimits(s => ({ ...s, auditMaxRows: typeof v === 'number' ? v : 1_000_000 }))}
              style={{ width: 240 }} />
          </Form.Item>
          <Form.Item label={t('settings.logLimitsLogin')} extra={t('settings.logLimitsHelp')}>
            <InputNumber min={1000} max={100_000_000} step={10_000} value={logLimits.loginMaxRows}
              onChange={(v) => setLogLimits(s => ({ ...s, loginMaxRows: typeof v === 'number' ? v : 1_000_000 }))}
              style={{ width: 240 }} />
          </Form.Item>
          <Button type="primary" loading={logLimitsSaving} onClick={saveLogLimits}>{t('common.save')}</Button>
        </Form>
      </Card>

      <Card title={t('settings.rateLimitTitle')} style={{ maxWidth: 720, marginBottom: 16 }}>
        <Alert type="info" showIcon style={{ marginBottom: 16 }} message={t('settings.rateLimitDesc')} />
        <Form layout="vertical">
          <Form.Item label={t('settings.rateLimitPerMin')} extra={t('settings.rateLimitPerMinHelp')}>
            <InputNumber min={1} max={100} value={rateLimit.rateLimitPerMin}
              onChange={(v) => setRateLimit(s => ({ ...s, rateLimitPerMin: typeof v === 'number' ? v : 5 }))}
              style={{ width: 240 }} />
          </Form.Item>
          <Form.Item label={t('settings.rateLimitBan')} extra={t('settings.rateLimitBanHelp')}>
            <InputNumber min={1} max={1440} value={rateLimit.banDurationMinutes}
              onChange={(v) => setRateLimit(s => ({ ...s, banDurationMinutes: typeof v === 'number' ? v : 5 }))}
              style={{ width: 240 }} />
          </Form.Item>
          <Form.Item label={t('settings.oauthStartCount')} extra={t('settings.oauthStartCountHelp')}>
            <InputNumber min={1} max={1000} value={rateLimit.oauthStartCount}
              onChange={(v) => setRateLimit(s => ({ ...s, oauthStartCount: typeof v === 'number' ? v : 30 }))}
              style={{ width: 240 }} />
          </Form.Item>
          <Form.Item label={t('settings.oauthStartWindow')} extra={t('settings.oauthStartWindowHelp')}>
            <InputNumber min={30} max={3600} value={rateLimit.oauthStartWindowSec}
              onChange={(v) => setRateLimit(s => ({ ...s, oauthStartWindowSec: typeof v === 'number' ? v : 300 }))}
              addonAfter={t('common.seconds')} style={{ width: 240 }} />
          </Form.Item>
          <Form.Item label={t('settings.pkceStoreMax')} extra={t('settings.pkceStoreMaxHelp')}>
            <InputNumber min={100} max={1000000} value={rateLimit.pkceStoreMaxEntries}
              onChange={(v) => setRateLimit(s => ({ ...s, pkceStoreMaxEntries: typeof v === 'number' ? v : 10000 }))}
              style={{ width: 240 }} />
          </Form.Item>
          <Form.Item label={t('settings.terminalReadDeadline')} extra={t('settings.terminalReadDeadlineHelp')}>
            <InputNumber min={10} max={600} value={rateLimit.terminalReadDeadlineSec}
              onChange={(v) => setRateLimit(s => ({ ...s, terminalReadDeadlineSec: typeof v === 'number' ? v : 60 }))}
              addonAfter={t('common.seconds')} style={{ width: 240 }} />
          </Form.Item>
          <Form.Item label={t('settings.terminalInputRate')} extra={t('settings.terminalInputRateHelp')}>
            <InputNumber min={1} max={5000} value={rateLimit.terminalInputRatePerSec}
              onChange={(v) => setRateLimit(s => ({ ...s, terminalInputRatePerSec: typeof v === 'number' ? v : 200 }))}
              style={{ width: 240 }} />
          </Form.Item>
          <Form.Item label={t('settings.terminalInputBurst')} extra={t('settings.terminalInputBurstHelp')}>
            <InputNumber min={1} max={5000} value={rateLimit.terminalInputBurst}
              onChange={(v) => setRateLimit(s => ({ ...s, terminalInputBurst: typeof v === 'number' ? v : 50 }))}
              style={{ width: 240 }} />
          </Form.Item>
          <Form.Item label={t('settings.iconCacheMaxAge')} extra={t('settings.iconCacheMaxAgeHelp')}>
            <InputNumber min={0} max={86400} value={rateLimit.iconCacheMaxAgeSec}
              onChange={(v) => setRateLimit(s => ({ ...s, iconCacheMaxAgeSec: typeof v === 'number' ? v : 300 }))}
              addonAfter={t('common.seconds')} style={{ width: 240 }} />
          </Form.Item>
          <Form.Item label={t('settings.iconRatePerMin')} extra={t('settings.iconRatePerMinHelp')}>
            <InputNumber min={1} max={1000} value={rateLimit.iconRatePerMin}
              onChange={(v) => setRateLimit(s => ({ ...s, iconRatePerMin: typeof v === 'number' ? v : 10 }))}
              style={{ width: 240 }} />
          </Form.Item>
          <Button type="primary" loading={rateLimitSaving} onClick={saveRateLimit}>{t('common.save')}</Button>
        </Form>
      </Card>

      <Card title={t('settings.reqLimitsTitle')} style={{ maxWidth: 720, marginBottom: 16 }}>
        <Alert type="info" showIcon style={{ marginBottom: 16 }} message={t('settings.reqLimitsDesc')} />
        <Form layout="vertical">
          <Form.Item label={t('settings.reqLimitsGlobal')} extra={t('settings.reqLimitsGlobalHelp')}>
            <InputNumber min={1} max={4096} value={Math.round(reqLimits.maxRequestBodyBytes / KIB)}
              onChange={(v) => setReqLimits(s => ({ ...s, maxRequestBodyBytes: (typeof v === 'number' ? v : 128) * KIB }))}
              addonAfter="KiB" style={{ width: 240 }} />
          </Form.Item>
          <Form.Item label={t('settings.reqLimitsJsonBody')} extra={t('settings.reqLimitsJsonBodyHelp')}>
            <InputNumber min={1} max={128} value={Math.round(reqLimits.maxJsonBodyBytes / MIB)}
              onChange={(v) => setReqLimits(s => ({ ...s, maxJsonBodyBytes: (typeof v === 'number' ? v : 16) * MIB }))}
              addonAfter="MiB" style={{ width: 240 }} />
          </Form.Item>
          <Form.Item label={t('settings.reqLimitsWsFrame')} extra={t('settings.reqLimitsWsFrameHelp')}>
            <InputNumber min={1} max={128} value={Math.round(reqLimits.maxWsFrameBytes / MIB)}
              onChange={(v) => setReqLimits(s => ({ ...s, maxWsFrameBytes: (typeof v === 'number' ? v : 16) * MIB }))}
              addonAfter="MiB" style={{ width: 240 }} />
          </Form.Item>
          <Button type="primary" loading={reqLimitsSaving} onClick={saveReqLimits}>{t('common.save')}</Button>
        </Form>
      </Card>

      <Card title={t('settings.cspTitle')} style={{ maxWidth: 720, marginBottom: 16 }}>
        <Alert type="info" showIcon style={{ marginBottom: 16 }} message={t('settings.cspDesc')} />
        <Form layout="vertical">
          <Form.Item label={t('settings.cspScriptSrc')} extra={t('settings.cspScriptSrcHelp')}>
            <Input.TextArea
              value={cspScriptSrc}
              onChange={(e) => setCspScriptSrc(e.target.value)}
              autoSize={{ minRows: 2, maxRows: 4 }}
              placeholder="https://challenges.cloudflare.com,https://www.recaptcha.net"
              style={{ fontFamily: 'monospace' }}
            />
          </Form.Item>
          <Form.Item label={t('settings.cspFrameSrc')} extra={t('settings.cspFrameSrcHelp')}>
            <Input.TextArea
              value={cspFrameSrc}
              onChange={(e) => setCspFrameSrc(e.target.value)}
              autoSize={{ minRows: 2, maxRows: 4 }}
              placeholder="https://challenges.cloudflare.com,https://www.google.com,https://www.recaptcha.net"
              style={{ fontFamily: 'monospace' }}
            />
          </Form.Item>
          <Button type="primary" loading={cspSaving} onClick={saveCSP}>{t('common.save')}</Button>
        </Form>
      </Card>

      <Card title={t('settings.authTimingsTitle')} style={{ maxWidth: 720, marginBottom: 16 }}>
        <Alert type="info" showIcon style={{ marginBottom: 16 }} message={t('settings.authTimingsDesc')} />
        <Form layout="vertical">
          <Form.Item label={t('settings.authTimingsTtl')} extra={t('settings.authTimingsTtlHelp')}>
            <InputNumber min={5} max={1440} value={authTimings.jwtTtlMinutes}
              onChange={(v) => setAuthTimings(s => ({ ...s, jwtTtlMinutes: typeof v === 'number' ? v : 60 }))}
              addonAfter={t('settings.minutesUnit')} style={{ width: 240 }} />
          </Form.Item>
          <Form.Item label={t('settings.authTimingsHeartbeat')} extra={t('settings.authTimingsHeartbeatHelp')}>
            <InputNumber min={1} max={60} value={authTimings.wsHeartbeatMinutes}
              onChange={(v) => setAuthTimings(s => ({ ...s, wsHeartbeatMinutes: typeof v === 'number' ? v : 5 }))}
              addonAfter={t('settings.minutesUnit')} style={{ width: 240 }} />
          </Form.Item>
          <Button type="primary" loading={authTimingsSaving} onClick={saveAuthTimings}>{t('common.save')}</Button>
        </Form>
      </Card>

      <Card title={t('settings.httpTimeoutsTitle')} style={{ maxWidth: 720 }}>
        <Alert type="warning" showIcon style={{ marginBottom: 16 }} message={t('settings.httpTimeoutsDesc')} />
        <Form layout="vertical">
          <Form.Item label={t('settings.httpReadHeaderTimeout')} extra={t('settings.httpReadHeaderTimeoutHelp')}>
            <InputNumber min={1} max={3600} value={httpTimeouts.readHeaderTimeoutSec}
              onChange={(v) => setHttpTimeouts(s => ({ ...s, readHeaderTimeoutSec: typeof v === 'number' ? v : 10 }))}
              addonAfter={t('common.seconds')} style={{ width: 240 }} />
          </Form.Item>
          <Form.Item label={t('settings.httpReadTimeout')} extra={t('settings.httpReadTimeoutHelp')}>
            <InputNumber min={1} max={3600} value={httpTimeouts.readTimeoutSec}
              onChange={(v) => setHttpTimeouts(s => ({ ...s, readTimeoutSec: typeof v === 'number' ? v : 60 }))}
              addonAfter={t('common.seconds')} style={{ width: 240 }} />
          </Form.Item>
          <Form.Item label={t('settings.httpWriteTimeout')} extra={t('settings.httpWriteTimeoutHelp')}>
            <InputNumber min={1} max={3600} value={httpTimeouts.writeTimeoutSec}
              onChange={(v) => setHttpTimeouts(s => ({ ...s, writeTimeoutSec: typeof v === 'number' ? v : 120 }))}
              addonAfter={t('common.seconds')} style={{ width: 240 }} />
          </Form.Item>
          <Form.Item label={t('settings.httpIdleTimeout')} extra={t('settings.httpIdleTimeoutHelp')}>
            <InputNumber min={1} max={3600} value={httpTimeouts.idleTimeoutSec}
              onChange={(v) => setHttpTimeouts(s => ({ ...s, idleTimeoutSec: typeof v === 'number' ? v : 120 }))}
              addonAfter={t('common.seconds')} style={{ width: 240 }} />
          </Form.Item>
          <Button type="primary" loading={httpTimeoutsSaving} onClick={saveHttpTimeouts}>{t('common.save')}</Button>
        </Form>
      </Card>
    </>
  )
}

// getTurnstileToken renders an invisible Turnstile widget with the
// supplied site key, waits for a fresh token via the success
// callback, then tears the widget down. Rejects on render error
// (bad site key / hostname not in CF allow-list).
function getTurnstileToken(siteKey: string): Promise<string> {
  return new Promise<string>((resolve, reject) => {
    if (!siteKey) { reject(new Error('site key empty')); return }
    const ensure = (): Promise<void> => new Promise((res, rej) => {
      const w: any = window
      if (w.turnstile) { res(); return }
      const existing = document.getElementById('cf-turnstile') as HTMLScriptElement | null
      if (existing) { existing.addEventListener('load', () => res(), { once: true }); return }
      const s = document.createElement('script')
      s.id = 'cf-turnstile'
      s.async = true; s.defer = true
      s.src = 'https://challenges.cloudflare.com/turnstile/v0/api.js?render=explicit'
      s.onload = () => res()
      s.onerror = () => rej(new Error('script load failed'))
      document.head.appendChild(s)
    })
    ensure().then(() => {
      const w: any = window
      // audit-2026-04-25 MED10: bound the SDK-global wait at 5s so a
      // network-blocked or ad-blocked Cloudflare script doesn't keep
      // a 50ms polling timer alive forever — surfaced as a clear
      // toast instead of a silent wedge.
      waitFor(() => !!w.turnstile, 5000).then(() => {
        const div = document.createElement('div')
        div.style.position = 'fixed'; div.style.left = '-10000px'; div.style.top = '0'
        document.body.appendChild(div)
        let done = false
        let widgetId: any = null
        const finish = (err?: Error, token?: string) => {
          if (done) return; done = true
          try { if (widgetId != null) w.turnstile.remove(widgetId) } catch { /* ignore */ }
          div.remove()
          err ? reject(err) : resolve(token || '')
        }
        const tmo = window.setTimeout(() => finish(new Error('widget render timeout')), 8000)
        try {
          widgetId = w.turnstile.render(div, {
            sitekey: siteKey,
            size: 'invisible',
            callback: (token: string) => { window.clearTimeout(tmo); finish(undefined, token) },
            'error-callback': (code: string) => { window.clearTimeout(tmo); finish(new Error(code || 'widget error')) },
          })
        } catch (e: any) {
          window.clearTimeout(tmo); finish(e instanceof Error ? e : new Error(String(e)))
        }
      }).catch(() => reject(new Error('turnstile global never appeared (script blocked or SDK failed to init)')))
    }).catch(reject)
  })
}

// getRecaptchaToken loads the Enterprise SDK with the supplied site
// key (via recaptcha.net so installs in mainland China can reach it)
// and asks for a fresh token via grecaptcha.enterprise.execute. If
// the site key is invalid the script logs an error and execute
// rejects — we surface that as render failure.
function getRecaptchaToken(siteKey: string, action: string): Promise<string> {
  return new Promise<string>((resolve, reject) => {
    if (!siteKey) { reject(new Error('site key empty')); return }
    const id = 'g-recaptcha-enterprise-test'
    const w: any = window
    const exec = () => {
      try {
        w.grecaptcha.enterprise.ready(() => {
          w.grecaptcha.enterprise.execute(siteKey, { action })
            .then((tok: string) => resolve(tok))
            .catch((e: any) => reject(e instanceof Error ? e : new Error(String(e?.message || 'execute failed'))))
        })
      } catch (e: any) {
        reject(e instanceof Error ? e : new Error(String(e)))
      }
    }
    if (w.grecaptcha?.enterprise?.execute) { exec(); return }
    const existing = document.getElementById(id) as HTMLScriptElement | null
    if (existing) {
      // audit-2026-04-25 MED10: same 5s ceiling, surfaced via the
      // shared waitFor utility.
      waitFor(() => !!w.grecaptcha?.enterprise?.execute, 5000)
        .then(exec)
        .catch(() => reject(new Error('grecaptcha load timeout')))
      return
    }
    const s = document.createElement('script')
    s.id = id
    s.async = true; s.defer = true
    s.src = `https://www.recaptcha.net/recaptcha/enterprise.js?render=${encodeURIComponent(siteKey)}`
    s.onload = () => {
      waitFor(() => !!w.grecaptcha?.enterprise?.execute, 5000)
        .then(exec)
        .catch(() => reject(new Error('grecaptcha global timeout')))
    }
    s.onerror = () => reject(new Error('grecaptcha script load failed'))
    document.head.appendChild(s)
  })
}

// ---- siteName validation helpers (mirror the Go-side rules) ----

const PUNCT_ALLOWED = new Set([
  ' ', '-', '_', '.', ',', '!', '?', ':', ';',
  '(', ')', '[', ']', '@', '#', '+', '*', '/', '\\',
  "'", '"', '|', '&', '·', '~', '`', '<', '>', '=',
  '。', '，', '！', '？', '：', '；', '（', '）',
  '【', '】', '“', '”', '‘', '’', '《', '》', '、',
])

function isCJKChar(cp: number): boolean {
  return (
    (cp >= 0x4e00 && cp <= 0x9fff) ||
    (cp >= 0x3400 && cp <= 0x4dbf) ||
    (cp >= 0x3040 && cp <= 0x30ff) ||
    (cp >= 0xac00 && cp <= 0xd7af) ||
    (cp >= 0xff00 && cp <= 0xffef) ||
    (cp >= 0x3000 && cp <= 0x303f)
  )
}

function siteNameValid(s: string): boolean {
  for (const ch of s) {
    if (/[\p{L}\p{N}]/u.test(ch)) continue
    if (PUNCT_ALLOWED.has(ch)) continue
    const cp = ch.codePointAt(0) || 0
    if (cp >= 0x3000 && cp <= 0x303f) continue
    return false
  }
  return true
}

// siteNameWeight: CJK = 2 units, anything else = 1 unit. Cap = 16.
function siteNameWeight(s: string): number {
  let w = 0
  for (const ch of s) {
    const cp = ch.codePointAt(0) || 0
    w += isCJKChar(cp) ? 2 : 1
  }
  return w
}

// filterSiteName drops any disallowed character as the user types AND
// truncates the input once the weight would exceed 16. Mirrors the
// allowlist + weight logic above.
function filterSiteName(s: string): string {
  let out = ''
  let w = 0
  for (const ch of s) {
    if (!siteNameValid(ch)) continue
    const cp = ch.codePointAt(0) || 0
    const cw = isCJKChar(cp) ? 2 : 1
    if (w + cw > 16) break
    out += ch
    w += cw
  }
  return out
}
