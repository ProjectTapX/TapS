import type { Terminal } from '@xterm/xterm'

// Visual column width of a code point — CJK / fullwidth glyphs occupy 2
// xterm cells, everything else 1. Matches the table we use elsewhere
// (e.g. the old erase-on-backspace logic).
function isWide(ch: string): boolean {
  const cp = ch.codePointAt(0) ?? 0
  return (
    (cp >= 0x1100 && cp <= 0x115F) ||
    (cp >= 0x2E80 && cp <= 0x303E) ||
    (cp >= 0x3041 && cp <= 0x33FF) ||
    (cp >= 0x3400 && cp <= 0x4DBF) ||
    (cp >= 0x4E00 && cp <= 0x9FFF) ||
    (cp >= 0xA000 && cp <= 0xA4CF) ||
    (cp >= 0xAC00 && cp <= 0xD7A3) ||
    (cp >= 0xF900 && cp <= 0xFAFF) ||
    (cp >= 0xFE30 && cp <= 0xFE4F) ||
    (cp >= 0xFF00 && cp <= 0xFF60) ||
    (cp >= 0xFFE0 && cp <= 0xFFE6) ||
    (cp >= 0x20000 && cp <= 0x2FFFD)
  )
}
function strWidth(s: string): number {
  let w = 0
  for (const ch of s) w += isWide(ch) ? 2 : 1
  return w
}

interface LineEditorOpts {
  term: Terminal
  send: (data: string) => void
  // Source of completion candidates beyond the rolling history. Called
  // every Tab press so callers can change vocabulary at runtime.
  getCandidates: () => string[]
  // Storage adapter — defaults to localStorage; kept abstract so tests can
  // inject an in-memory map.
  historyKey: string
  historyLimit?: number
}

interface LineEditor {
  // Feed raw onData chunks from xterm; returns true if the editor handled
  // the chunk (and the caller should NOT also send it to the server),
  // false to let the caller fall through to its own behaviour.
  feed: (data: string) => boolean
  // External output (server stdout) is about to be written — ask the
  // editor to clear its visible row first. After the caller has written
  // the output, call render() so the in-progress line reappears.
  hide: () => void
  render: () => void
  // True when there's a partially-typed buffer; lets the terminal know
  // whether to bother calling hide()/render().
  hasInput: () => boolean
}

const HIST_LIMIT_DEFAULT = 200

function loadHistory(key: string): string[] {
  try {
    const raw = window.localStorage.getItem(key)
    if (!raw) return []
    const parsed = JSON.parse(raw)
    return Array.isArray(parsed) ? parsed.slice(-HIST_LIMIT_DEFAULT) : []
  } catch { return [] }
}
function saveHistory(key: string, hist: string[]) {
  try { window.localStorage.setItem(key, JSON.stringify(hist)) } catch { /* quota or disabled */ }
}

export function createLineEditor(opts: LineEditorOpts): LineEditor {
  const { term, send, getCandidates, historyKey } = opts
  const limit = opts.historyLimit ?? HIST_LIMIT_DEFAULT

  let buf = ''                  // current line buffer (code points joined)
  let pos = 0                   // cursor index measured in code points
  // History walker: histIdx === -1 means "at the live draft"; 0 = newest.
  // draft holds the in-progress line so ↑ then ↓ can restore it.
  const history: string[] = loadHistory(historyKey)
  let histIdx = -1
  let draft = ''
  // Tab cycling state — set on the first matching Tab press, advanced on
  // subsequent presses, dropped on any non-Tab key.
  let tabCandidates: string[] = []
  let tabIdx = 0
  let tabPrefix = ''            // the token we were completing
  let tabPosBefore = 0          // position the prefix started at

  const chars = () => Array.from(buf)

  // Visual width of buf[0..pos) — used to position the cursor. CJK aware.
  function widthBefore(): number {
    let w = 0
    let i = 0
    for (const ch of chars()) {
      if (i++ >= pos) break
      w += isWide(ch) ? 2 : 1
    }
    return w
  }

  // Erase what we last drew, leaving the cursor at the start of the input.
  function clear() {
    const back = widthBefore()
    if (back > 0) term.write(`\x1b[${back}D`)
    const total = strWidth(buf)
    if (total > 0) term.write(`\x1b[${total}X`)        // erase characters in place
    // Some terminals respect EL (erase-in-line to end of line) better:
    term.write('\x1b[K')
  }
  function redraw() {
    term.write(buf)
    const trail = strWidth(buf) - widthBefore()
    if (trail > 0) term.write(`\x1b[${trail}D`)
  }
  function rerender() { clear(); redraw() }

  function pushHistory(line: string) {
    if (!line) return
    if (history.length > 0 && history[history.length - 1] === line) return
    history.push(line)
    if (history.length > limit) history.splice(0, history.length - limit)
    saveHistory(historyKey, history)
  }

  function setBuf(next: string, nextPos = next.length) {
    clear()
    buf = next
    pos = Math.min(nextPos, Array.from(buf).length)
    redraw()
  }

  function resetTabState() {
    tabCandidates = []
    tabIdx = 0
    tabPrefix = ''
    tabPosBefore = 0
  }

  // The "current token" is everything from the last whitespace before pos
  // through pos. Tab completes against this token only.
  function currentToken(): { value: string; start: number } {
    const cs = chars()
    let i = pos
    while (i > 0 && !/\s/.test(cs[i - 1])) i--
    return { value: cs.slice(i, pos).join(''), start: i }
  }

  function computeCandidates(prefix: string): string[] {
    if (!prefix) return []
    const seen = new Set<string>()
    const out: string[] = []
    const push = (s: string) => {
      if (!s.startsWith(prefix) || s === prefix) return
      if (seen.has(s)) return
      seen.add(s); out.push(s)
    }
    // History tokens (any token, not just commands) — newest first.
    for (let i = history.length - 1; i >= 0; i--) {
      for (const tok of history[i].split(/\s+/)) push(tok)
    }
    for (const c of getCandidates()) push(c)
    return out
  }

  function applyCompletion(replacement: string, tokenStart: number) {
    const cs = chars()
    const before = cs.slice(0, tokenStart).join('')
    const after = cs.slice(pos).join('')
    setBuf(before + replacement + after, Array.from(before + replacement).length)
  }

  function handleTab() {
    // Continuing an active cycle?
    if (tabCandidates.length > 0) {
      tabIdx = (tabIdx + 1) % tabCandidates.length
      applyCompletion(tabCandidates[tabIdx], tabPosBefore)
      return
    }
    const { value, start } = currentToken()
    if (!value) return
    const cands = computeCandidates(value)
    if (cands.length === 0) return
    if (cands.length === 1) {
      applyCompletion(cands[0], start)
      return
    }
    tabPrefix = value
    tabPosBefore = start
    tabCandidates = cands
    tabIdx = 0
    applyCompletion(cands[0], start)
  }

  // Map a single ESC sequence (without the leading \x1b) to an action.
  // Returns the number of trailing chars consumed (excluding the ESC).
  function handleEsc(rest: string): number {
    // We support only the minimal CSI subset xterm sends for arrows,
    // Home/End, Delete, and a couple of legacy aliases. Anything else
    // is consumed silently to avoid garbage in the buffer.
    const m = rest.match(/^\[(\d*)([A-D~HF])/) || rest.match(/^O([A-DHF])/)
    if (!m) return rest.length > 0 ? 1 : 0
    const arg = m.length === 3 ? m[1] : ''
    const code = m[m.length - 1]
    const len = m[0].length
    const cs = chars()
    switch (code) {
      case 'A': // up
        if (history.length === 0) break
        if (histIdx === -1) draft = buf
        if (histIdx < history.length - 1) {
          histIdx++
          setBuf(history[history.length - 1 - histIdx])
        }
        break
      case 'B': // down
        if (histIdx === -1) break
        histIdx--
        if (histIdx === -1) setBuf(draft)
        else setBuf(history[history.length - 1 - histIdx])
        break
      case 'C': // right
        if (pos < cs.length) {
          const ch = cs[pos]
          pos++
          term.write(`\x1b[${isWide(ch) ? 2 : 1}C`)
        }
        break
      case 'D': // left
        if (pos > 0) {
          const ch = cs[pos - 1]
          pos--
          term.write(`\x1b[${isWide(ch) ? 2 : 1}D`)
        }
        break
      case 'H': // Home
      case '1': // legacy "ESC[1~" Home
        if (pos > 0) {
          const w = widthBefore()
          pos = 0
          term.write(`\x1b[${w}D`)
        }
        break
      case 'F': // End
      case '4': // legacy "ESC[4~" End
        if (pos < cs.length) {
          const trail = strWidth(cs.slice(pos).join(''))
          pos = cs.length
          term.write(`\x1b[${trail}C`)
        }
        break
      case '3': // ESC[3~ Delete
        if (arg === '3' && pos < cs.length) {
          const next = cs.slice(0, pos).join('') + cs.slice(pos + 1).join('')
          setBuf(next, pos)
        }
        break
    }
    return len
  }

  function feed(data: string): boolean {
    let i = 0
    while (i < data.length) {
      const ch = data[i]
      // ESC sequence
      if (ch === '\x1b') {
        const consumed = handleEsc(data.slice(i + 1))
        i += 1 + consumed
        // any non-Tab input cancels Tab cycle
        if (tabCandidates.length > 0) resetTabState()
        // any movement/edit cancels history-walking only when content changes;
        // simple navigation keeps histIdx so user can keep walking.
        continue
      }
      if (ch === '\r' || ch === '\n') {
        const line = buf
        // Echo CRLF locally so the prompt looks committed; server gets line+\n
        term.write('\r\n')
        send(line + '\n')
        pushHistory(line)
        buf = ''; pos = 0
        histIdx = -1; draft = ''
        resetTabState()
        i++; continue
      }
      if (ch === '\x7f' || ch === '\b') {
        if (pos > 0) {
          const cs = chars()
          const popped = cs[pos - 1]
          const next = cs.slice(0, pos - 1).join('') + cs.slice(pos).join('')
          setBuf(next, pos - 1)
          // setBuf already redraws; popped only matters for cursor
          void popped
        }
        if (tabCandidates.length > 0) resetTabState()
        i++; continue
      }
      if (ch === '\t') {
        handleTab()
        i++; continue
      }
      if (ch === '\x03') {     // Ctrl+C
        send(ch)
        term.write('^C\r\n')
        buf = ''; pos = 0
        histIdx = -1; draft = ''
        resetTabState()
        i++; continue
      }
      if (ch === '\x01') {     // Ctrl+A → Home
        if (pos > 0) { const w = widthBefore(); pos = 0; term.write(`\x1b[${w}D`) }
        i++; continue
      }
      if (ch === '\x05') {     // Ctrl+E → End
        const cs = chars()
        if (pos < cs.length) {
          const trail = strWidth(cs.slice(pos).join(''))
          pos = cs.length
          term.write(`\x1b[${trail}C`)
        }
        i++; continue
      }
      if (ch === '\x15') {     // Ctrl+U → kill line
        if (buf.length > 0) setBuf('', 0)
        if (tabCandidates.length > 0) resetTabState()
        i++; continue
      }
      if (ch === '\x17') {     // Ctrl+W → delete word back
        if (pos > 0) {
          const cs = chars()
          let j = pos
          while (j > 0 && /\s/.test(cs[j - 1])) j--
          while (j > 0 && !/\s/.test(cs[j - 1])) j--
          const next = cs.slice(0, j).join('') + cs.slice(pos).join('')
          setBuf(next, j)
        }
        if (tabCandidates.length > 0) resetTabState()
        i++; continue
      }
      // Printable: insert at pos
      if (ch >= ' ') {
        const cs = chars()
        const next = cs.slice(0, pos).join('') + ch + cs.slice(pos).join('')
        setBuf(next, pos + 1)
        if (tabCandidates.length > 0) resetTabState()
        i++; continue
      }
      // Anything else (other control chars) → swallow
      i++
    }
    return true
  }

  function hide() { if (buf.length > 0) clear() }
  function render() { if (buf.length > 0) redraw() }
  function hasInput() { return buf.length > 0 }

  return { feed, hide, render, hasInput }
}
