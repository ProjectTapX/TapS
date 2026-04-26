// LoginCaptcha mounts the configured captcha widget (Cloudflare
// Turnstile or Google reCAPTCHA Enterprise) inline on the login form.
// Both libraries are loaded lazily from their CDNs — reCAPTCHA via
// recaptcha.net (not google.com) so installs in mainland China can
// still reach the script.
//
// Exposes an imperative getToken() to the parent: Turnstile gives an
// implicit-rendered token via a callback; reCAPTCHA Enterprise uses
// the v3-style "execute" call gated on user submit.
import { useEffect, useImperativeHandle, useRef, forwardRef } from 'react'
import { useTranslation } from 'react-i18next'
import { waitFor } from '@/utils/waitFor'

export type CaptchaConfig =
  | { provider: 'none'; siteKey: '' }
  | { provider: 'turnstile'; siteKey: string }
  | { provider: 'recaptcha'; siteKey: string }

export interface CaptchaHandle {
  // getToken returns the most-recent token, or asks the underlying
  // SDK for a fresh one (reCAPTCHA Enterprise requires this — every
  // token is single-use). Resolves "" if captcha is disabled.
  getToken: () => Promise<string>
  // reset is called after a failed login so the user can retry.
  reset: () => void
}

declare global {
  interface Window {
    turnstile?: any
    grecaptcha?: any
    __tapsCaptchaCallbacks?: Record<string, (token: string) => void>
  }
}

function loadScript(src: string, id: string): Promise<void> {
  return new Promise((resolve, reject) => {
    const existing = document.getElementById(id) as HTMLScriptElement | null
    if (existing) {
      // If the tag is already there but the script hasn't finished
      // loading yet (StrictMode double-mount, fast re-renders), wait
      // for it instead of resolving immediately.
      if ((existing as any)._loaded) { resolve(); return }
      existing.addEventListener('load', () => resolve(), { once: true })
      existing.addEventListener('error', () => reject(new Error('failed to load ' + src)), { once: true })
      return
    }
    const s = document.createElement('script')
    s.id = id
    s.src = src
    s.async = true
    s.defer = true
    s.onload = () => { (s as any)._loaded = true; resolve() }
    s.onerror = () => reject(new Error('failed to load ' + src))
    document.head.appendChild(s)
  })
}

const LoginCaptcha = forwardRef<CaptchaHandle, {
  config: CaptchaConfig | null
  // Fires whenever the widget's readiness changes. Parent uses this
  // to gate the submit button so a click before the script loads (or
  // before the user solves a visible challenge) can't submit an
  // empty token.
  onReadyChange?: (ready: boolean) => void
}>(({ config, onReadyChange }, ref) => {
  const { t } = useTranslation()
  const containerRef = useRef<HTMLDivElement | null>(null)
  const tokenRef = useRef<string>('')
  const widgetIdRef = useRef<any>(null)

  const setReady = (r: boolean) => { onReadyChange?.(r) }

  useImperativeHandle(ref, () => ({
    getToken: async () => {
      if (!config || config.provider === 'none') return ''
      if (config.provider === 'turnstile') return tokenRef.current
      if (config.provider === 'recaptcha') {
        if (!window.grecaptcha?.enterprise) throw new Error('recaptcha not ready')
        return await window.grecaptcha.enterprise.execute(config.siteKey, { action: 'login' })
      }
      return ''
    },
    reset: () => {
      tokenRef.current = ''
      // For Turnstile, the widget is one-shot — we re-render it and
      // wait for a new callback before "ready" again. For reCAPTCHA
      // Enterprise score-mode the SDK is always ready (execute() is
      // on-demand), so we keep ready=true to avoid stranding the
      // submit button in the "loading" state after a rejected login.
      if (config?.provider === 'turnstile') {
        setReady(false)
        if (window.turnstile && widgetIdRef.current != null) {
          try { window.turnstile.reset(widgetIdRef.current) } catch { /* ignore */ }
        }
      }
    },
  }), [config])

  useEffect(() => {
    if (!config || config.provider === 'none') {
      // No captcha configured → always "ready", parent never disables.
      setReady(true)
      return
    }
    setReady(false)
    let cancelled = false

    if (config.provider === 'turnstile') {
      loadScript('https://challenges.cloudflare.com/turnstile/v0/api.js?render=explicit', 'cf-turnstile')
        .then(() => waitFor(() => !!window.turnstile))
        .then(() => {
          if (cancelled || !containerRef.current || !window.turnstile) return
          containerRef.current.innerHTML = ''
          try {
            widgetIdRef.current = window.turnstile.render(containerRef.current, {
              sitekey: config.siteKey,
              size: 'flexible',
              callback: (t: string) => { tokenRef.current = t; setReady(true) },
              'expired-callback': () => { tokenRef.current = ''; setReady(false) },
              'error-callback': () => { tokenRef.current = ''; setReady(false) },
            })
          } catch (err) {
            console.error('turnstile render failed', err)
          }
        })
        .catch((err) => { console.error('turnstile load failed', err) })
    }

    if (config.provider === 'recaptcha') {
      // Enterprise score-based mode is always "ready" once the global
      // SDK has loaded — execute() runs on-demand at submit time.
      loadScript(
        `https://www.recaptcha.net/recaptcha/enterprise.js?render=${encodeURIComponent(config.siteKey)}`,
        'g-recaptcha-enterprise',
      )
        .then(() => waitFor(() => !!window.grecaptcha?.enterprise?.execute))
        .then(() => { setReady(true) })
        .catch((err) => { console.error('recaptcha load failed', err) })
    }

    return () => {
      cancelled = true
      if (config.provider === 'turnstile' && window.turnstile && widgetIdRef.current != null) {
        try { window.turnstile.remove(widgetIdRef.current) } catch { /* ignore */ }
        widgetIdRef.current = null
      }
      // reCAPTCHA cleanup: SDK pins a persistent .grecaptcha-badge to
      // <body> plus a couple of helper iframes after the first execute().
      // Without an explicit removal they linger across SPA route changes
      // (the badge is hidden globally via index.css, but the iframes
      // still occupy ~0 size DOM space and keep timers alive). Drop the
      // whole lot when the login page unmounts so post-login routes are
      // clean. The script tag stays — re-entering /login will reuse it.
      if (config.provider === 'recaptcha') {
        document.querySelectorAll('.grecaptcha-badge').forEach((el) => el.remove())
        document.querySelectorAll('iframe[src*="recaptcha"]').forEach((el) => el.remove())
      }
    }
  }, [config?.provider, config?.siteKey])

  if (!config || config.provider === 'none') return null
  if (config.provider === 'turnstile') {
    return <div ref={containerRef} style={{ width: '100%', marginBottom: 16 }} />
  }
  // reCAPTCHA Enterprise score-based mode renders an invisible badge,
  // so we don't need a container — but Google requires either the
  // badge be visible or a privacy disclosure. We surface the latter.
  return (
    <div style={{ fontSize: 11, color: 'var(--taps-text-muted)', marginBottom: 12 }}>
      {t('captcha.recaptchaProtected')}
      {' '}<a href="https://policies.google.com/privacy" target="_blank" rel="noreferrer">{t('captcha.privacy')}</a>
      {' / '}<a href="https://policies.google.com/terms" target="_blank" rel="noreferrer">{t('captcha.terms')}</a>
    </div>
  )
})

export default LoginCaptcha
