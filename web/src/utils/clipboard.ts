// copyToClipboard works on both secure (HTTPS / localhost) and insecure
// (HTTP) contexts. The Clipboard API is gated on a secure context so on a
// plain http:// deployment `navigator.clipboard` is undefined and the
// "copy" buttons would silently no-op. We fall back to the legacy
// document.execCommand('copy') trick using a hidden textarea.
export async function copyToClipboard(text: string): Promise<boolean> {
  if (navigator.clipboard && window.isSecureContext) {
    try {
      await navigator.clipboard.writeText(text)
      return true
    } catch {
      // fall through to the legacy path
    }
  }
  try {
    const ta = document.createElement('textarea')
    ta.value = text
    ta.style.position = 'fixed'
    ta.style.opacity = '0'
    ta.style.pointerEvents = 'none'
    document.body.appendChild(ta)
    ta.focus()
    ta.select()
    const ok = document.execCommand('copy')
    document.body.removeChild(ta)
    return ok
  } catch {
    return false
  }
}
