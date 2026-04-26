import { Form, Input, Button, App, Typography, Select, Divider, Space } from 'antd'
import { ArrowRightOutlined, TranslationOutlined, SunOutlined, MoonOutlined } from '@ant-design/icons'
import { useNavigate } from 'react-router-dom'
import { useTranslation } from 'react-i18next'
import { useEffect, useRef, useState } from 'react'
import { api } from '@/api/client'
import { useAuthStore } from '@/stores/auth'
import { usePrefs } from '@/stores/auth'
import LoginCaptcha, { type CaptchaConfig, type CaptchaHandle } from '@/components/LoginCaptcha'
import { useBrandStore } from '@/stores/brand'
import { ssoApi, authConfigApi, type SSOProviderPublic, type LoginMethod } from '@/api/resources'
import { formatApiError } from '@/api/errors'

export default function LoginPage() {
  const nav = useNavigate()
  const { t, i18n } = useTranslation()
  const { message } = App.useApp()
  const setAuth = useAuthStore((s) => s.setAuth)
  const siteName = useBrandStore((s) => s.siteName)
  const isDark = usePrefs((s) => s.theme) === 'dark'
  const setTheme = usePrefs((s) => s.setTheme)
  const [captchaCfg, setCaptchaCfg] = useState<CaptchaConfig | null>(null)
  const captchaRef = useRef<CaptchaHandle | null>(null)
  // Default to true so the button isn't disabled before the config
  // call resolves (no-captcha case is the most common).
  const [captchaReady, setCaptchaReady] = useState(true)
  const [submitting, setSubmitting] = useState(false)
  // SSO providers + login method govern which form sections we show.
  // Both load lazily; UI degrades gracefully if either request fails.
  const [providers, setProviders] = useState<SSOProviderPublic[]>([])
  const [loginMethod, setLoginMethod] = useState<LoginMethod>('password-only')

  // Load captcha config + SSO config once on mount; either may fail
  // silently — login should still work for the password-only case.
  useEffect(() => {
    api.get<CaptchaConfig>('/captcha/config')
      .then(r => setCaptchaCfg(r.data))
      .catch(() => setCaptchaCfg({ provider: 'none', siteKey: '' }))
    ssoApi.publicProviders().then(setProviders).catch(() => setProviders([]))
    authConfigApi.getMethod().then(setLoginMethod).catch(() => setLoginMethod('password-only'))
  }, [])

  // Pick up an OIDC callback's hash fragment exactly once. The panel
  // signs the TapS JWT after IdP success and 302s the browser to
  // <publicURL>/#oauth-token=<jwt>. Strip the hash *before* doing
  // anything else (so a Referer leak / refresh / catch-path can't keep
  // the JWT in the URL bar), then load /me. /#oauth-error=<msg>
  // displays as a toast. The useRef gate stops React StrictMode's
  // intentional double-mount from running this twice.
  const oauthHandled = useRef(false)
  useEffect(() => {
    if (oauthHandled.current) return
    const hash = window.location.hash
    if (!hash.startsWith('#oauth-token=') && !hash.startsWith('#oauth-error=')) return
    oauthHandled.current = true
    // Strip the hash up front — covers both success and failure paths.
    // Doing this before any await/network call also closes the window
    // where a chunk-load failure or unhandled rejection could leave
    // the JWT visible in window.location.
    const cleanPath = window.location.pathname + window.location.search
    window.history.replaceState(null, '', cleanPath)

    if (hash.startsWith('#oauth-token=')) {
      let tok = ''
      try {
        tok = decodeURIComponent(hash.substring('#oauth-token='.length))
      } catch {
        message.error(t('login.failed'))
        return
      }
      ;(async () => {
        try {
          // Pull /me with a one-shot Authorization header so the fake
          // placeholder user never lands in zustand-persist (see B20).
          const r = await api.get('/auth/me', { headers: { Authorization: `Bearer ${tok}` } })
          useAuthStore.getState().setAuth(tok, r.data)
          nav('/')
        } catch (e: any) {
          message.error(formatApiError(e, 'login.failed'))
          useAuthStore.getState().logout()
        }
      })()
      return
    }
    // #oauth-error=...  (MED17: backend now sends a stable error code,
    // never a free-form message — translate via i18n, fall back to the
    // raw code for forward compat with codes the frontend hasn't shipped
    // a translation for yet.)
    let code = ''
    try {
      code = decodeURIComponent(hash.substring('#oauth-error='.length))
    } catch {
      code = ''
    }
    const translated = code ? t(`errors.${code}`, { defaultValue: code }) : ''
    message.error(t('login.ssoFailed') + (translated ? ': ' + translated : ''))
  }, [nav, message, t])

  const onFinish = async (v: { username: string; password: string }) => {
    let captchaToken = ''
    if (captchaCfg && captchaCfg.provider !== 'none') {
      try {
        captchaToken = await captchaRef.current?.getToken() ?? ''
      } catch {
        message.error(t('login.captchaNotReady'))
        return
      }
      if (!captchaToken) {
        message.error(t('login.captchaRequired'))
        return
      }
    }
    setSubmitting(true)
    try {
      const { data } = await api.post('/auth/login', { ...v, captchaToken })
      setAuth(data.token, data.user)
      nav('/')
    } catch (e: any) {
      captchaRef.current?.reset()
      const status = e?.response?.status
      const code = e?.response?.data?.error
      // audit-2026-04-25 MED14: switch on the structured error code,
      // not the human-readable English message. Without this any
      // future tweak to the backend message string silently demotes
      // the captcha-failure branch to the generic invalidCredentials
      // toast and the user can't tell why login keeps refusing them.
      if (code === 'auth.captcha_failed') {
        message.error(t('login.captchaFailed'))
      } else if (status === 401) {
        message.error(t('login.invalidCredentials'))
      } else {
        message.error(formatApiError(e, 'login.failed'))
      }
    } finally {
      setSubmitting(false)
    }
  }

  const showPasswordForm = loginMethod !== 'oidc-only'
  const showSSOButtons = loginMethod !== 'password-only' && providers.length > 0

  return (
    <div style={{ minHeight: '100vh', display: 'grid', gridTemplateColumns: '1.1fr 1fr', background: isDark ? '#0a0f1f' : '#fff', color: isDark ? '#e6e9ef' : 'inherit' }}>
      {/* left: brand panel */}
      <div style={{
        position: 'relative',
        background: isDark
          ? 'radial-gradient(circle at 20% 20%, #00C2FF 0%, #007BFC 35%, #1a3eb5 70%, #050810 100%)'
          : 'radial-gradient(circle at 20% 20%, #00C2FF 0%, #007BFC 35%, #1a3eb5 75%, #1a1f3d 100%)',
        color: '#fff',
        display: 'flex', flexDirection: 'column', justifyContent: 'space-between',
        padding: 48,
        overflow: 'hidden',
      }}>
        <div style={{ fontSize: 28, fontWeight: 700, letterSpacing: '-0.02em' }}>{siteName}</div>

        <div>
          <h1 style={{
            color: '#fff', fontSize: 42, lineHeight: 1.15, fontWeight: 700,
            letterSpacing: '-0.02em', marginBottom: 16, whiteSpace: 'pre-line',
          }}>
            {t('login.heroTitle')}
          </h1>
          <p style={{ color: 'rgba(255,255,255,0.85)', fontSize: 15, lineHeight: 1.7, maxWidth: 520 }}>
            {t('login.heroDesc')}
          </p>
        </div>

        <div />

        {/* decorative blobs */}
        <div style={{
          position: 'absolute', right: -120, top: -120, width: 360, height: 360,
          borderRadius: '50%', background: 'rgba(255,255,255,0.06)', filter: 'blur(8px)',
        }} />
        <div style={{
          position: 'absolute', right: -200, bottom: -200, width: 480, height: 480,
          borderRadius: '50%', background: 'rgba(0,194,255,0.18)', filter: 'blur(40px)',
        }} />
      </div>

      {/* right: login form */}
      <div style={{ display: 'flex', flexDirection: 'column', alignItems: 'center', justifyContent: 'center', padding: 24, position: 'relative' }}>
        {/* language switcher + theme toggle pinned to top-right of the right pane */}
        <div style={{ position: 'absolute', top: 20, right: 24, display: 'flex', alignItems: 'center', gap: 4 }}>
          <Button type="text" shape="circle" icon={isDark ? <SunOutlined /> : <MoonOutlined />}
            onClick={() => setTheme(isDark ? 'light' : 'dark')} />
          <Select
            size="small"
            variant="borderless"
            value={i18n.language?.startsWith('en') ? 'en' : i18n.language?.startsWith('ja') ? 'ja' : 'zh'}
            onChange={(v) => i18n.changeLanguage(v)}
            suffixIcon={<TranslationOutlined />}
            options={[{ label: '中文', value: 'zh' }, { label: 'English', value: 'en' }, { label: '日本語', value: 'ja' }]}
            style={{ width: 110 }}
          />
        </div>

        <div style={{ width: 360 }}>
          <Typography.Title level={3} style={{ marginBottom: 4, fontWeight: 600 }}>{t('login.title', { name: siteName })}</Typography.Title>
          <p style={{ color: 'var(--taps-text-muted)', marginBottom: 32 }}>
            {t('login.welcome')}
          </p>
          <Form layout="vertical" onFinish={onFinish} requiredMark={false} size="large">
            {showPasswordForm && (
              <>
                <Form.Item name="username" label={t('login.username')} rules={[{ required: true }]}>
                  <Input autoComplete="username" placeholder={t('login.usernamePh')} />
                </Form.Item>
                <Form.Item name="password" label={t('login.password')} rules={[{ required: true }]}>
                  <Input.Password autoComplete="current-password" placeholder="••••••••" />
                </Form.Item>
                <LoginCaptcha ref={captchaRef} config={captchaCfg} onReadyChange={setCaptchaReady} />
                <Button type="primary" htmlType="submit" block size="large"
                  icon={<ArrowRightOutlined />} iconPosition="end"
                  loading={submitting}
                  disabled={!captchaReady || submitting}>
                  {!captchaReady ? t('login.captchaWaiting')
                    : submitting ? t('login.submitting')
                    : t('login.submit')}
                </Button>
              </>
            )}
            {showSSOButtons && (
              <>
                {showPasswordForm && <Divider plain style={{ margin: '24px 0 16px', color: 'var(--taps-text-muted)' }}>{t('login.or')}</Divider>}
                <Space direction="vertical" size={8} style={{ width: '100%' }}>
                  {providers.map(p => {
                    // Send the user straight to the panel's OIDC start
                    // endpoint; the server will 302 to the IdP. Using
                    // location.assign (full navigation) so the OIDC
                    // session cookies the IdP may set work correctly.
                    const href = `/api/oauth/start/${encodeURIComponent(p.name)}`
                    return (
                      <Button key={p.name} block size="large" onClick={() => window.location.assign(href)}>
                        {t('login.signInWith', { name: p.displayName })}
                      </Button>
                    )
                  })}
                </Space>
              </>
            )}
            {!showPasswordForm && !showSSOButtons && (
              <Typography.Text type="warning">{t('login.noLoginMethod')}</Typography.Text>
            )}
          </Form>
        </div>
      </div>
    </div>
  )
}
