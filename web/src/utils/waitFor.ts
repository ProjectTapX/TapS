// waitFor polls `predicate` every 60 ms until it returns true or
// `timeoutMs` elapses. Resolves on success, rejects with a timeout
// error otherwise. Bridges the gap between "third-party SDK <script>
// tag finished loading" and "library global is initialised" — both
// Cloudflare Turnstile and Google reCAPTCHA Enterprise attach their
// globals asynchronously after the script's `load` event fires.
//
// Used by both the login page captcha and the settings page captcha
// connectivity test (audit-2026-04-25 MED10) so the polling logic
// has one home and one timeout policy.
export function waitFor(predicate: () => boolean, timeoutMs = 5000): Promise<void> {
  return new Promise((resolve, reject) => {
    if (predicate()) { resolve(); return }
    const start = Date.now()
    const handle = window.setInterval(() => {
      if (predicate()) { window.clearInterval(handle); resolve() }
      else if (Date.now() - start > timeoutMs) {
        window.clearInterval(handle)
        reject(new Error('timeout waiting for predicate'))
      }
    }, 60)
  })
}
