// Pulls a user-facing message out of an axios error.
//
// New backend responses look like:
//   { "error": "sso.last_binding", "message": "Set a local password..." }
// Old responses still look like:
//   { "error": "some english or chinese message" }
//
// formatApiError tries the new shape first (looks up i18n key
// `errors.<code>`); falls back to the message field; falls back to the
// old `error` plain string; finally to a generic localized fallback.
//
// Pass via App.useApp().message.error(formatApiError(err)) — never read
// `err.response.data.error` directly any more.

import i18n from '@/i18n'

interface ApiErrorBody {
  error?:   string
  message?: string
  params?:  Record<string, unknown>
}

// Heuristic: a code is something like "domain.snake_case" — letters,
// digits, underscores, dots only. Anything containing whitespace,
// punctuation, or non-ASCII almost certainly came from the legacy
// "throw english message" path.
const CODE_RE = /^[a-z][a-z0-9]*(?:\.[a-z0-9_]+)+$/

export function formatApiError(err: unknown, fallbackKey = 'common.error'): string {
  const body = (err as { response?: { data?: ApiErrorBody } })?.response?.data
  const code = body?.error
  const msg  = body?.message

  if (code && CODE_RE.test(code)) {
    const key = `errors.${code}`
    const translated = i18n.t(key, { ...(body?.params ?? {}), defaultValue: '' })
    if (translated && translated !== key) return translated
    if (msg) return msg
    return i18n.t(fallbackKey)
  }

  // Legacy plain-string error field, or a non-code value — show as-is.
  if (code) return code
  if (msg)  return msg
  return i18n.t(fallbackKey)
}
