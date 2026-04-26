import { useEffect, useRef, useState } from 'react'
import { Terminal } from '@xterm/xterm'
import { FitAddon } from '@xterm/addon-fit'
import '@xterm/xterm/css/xterm.css'
import { useAuthStore } from '@/stores/auth'
import { createLineEditor } from './lineEditor'

interface Props {
  daemonId: number
  uuid: string
  // localEcho enables a tiny line editor inside the browser. Docker
  // containers run with a plain stdio pipe (not a PTY), so the process
  // never echoes user input and never honours raw backspace bytes — we
  // need to echo locally and only send the *final* line to the server on
  // Enter so a deleted character doesn't end up in the server's stdin
  // buffer as `c\bd`.
  localEcho?: boolean
  // Tab-completion candidates. Re-evaluated on every Tab press so the
  // caller can change vocabulary based on instance type or user-edited
  // word lists.
  completionCandidates?: () => string[]
}

export default function InstanceTerminal({ daemonId, uuid, localEcho, completionCandidates }: Props) {
  const wrapRef = useRef<HTMLDivElement>(null)
  const termHostRef = useRef<HTMLDivElement>(null)
  const [height, setHeight] = useState<number>(220)
  // Hold the latest candidate-source function in a ref so updates from
  // the parent (e.g. user edits the custom word list) take effect on the
  // next Tab press without tearing down the whole terminal.
  const completionRef = useRef(completionCandidates)
  useEffect(() => { completionRef.current = completionCandidates }, [completionCandidates])

  // Size the terminal to fill exactly the remaining viewport.
  //
  // We measure the wrapper's *document-absolute* top (rect.top + scrollY)
  // so the result is independent of the current scroll position — without
  // that adjustment, mounting the terminal while the page is scrolled
  // down (e.g. after browsing a long file list and switching back) would
  // see a negative rect.top and grow the terminal far past the viewport.
  //
  // We additionally guard against transient pre-layout measurements: a
  // tab switch can fire compute() before antd's display:none sibling has
  // been removed, leaving rect.top stale. Two passes (rAF + a short
  // timeout chain) plus an IntersectionObserver tied to the wrapper's
  // visibility cover the realistic cases. A final overflow-check trims
  // any residual scroll.
  useEffect(() => {
    const el = wrapRef.current
    if (!el) return
    const compute = () => {
      const r = el.getBoundingClientRect()
      // If the wrapper has zero size (display:none in an inactive tab) or
      // an obviously bogus position (rect collapsed before layout),
      // skip — IntersectionObserver / ResizeObserver will retry once it
      // actually appears.
      if (r.width === 0 || r.height === 0) return
      const absoluteTop = r.top + window.scrollY
      const target = Math.max(240, Math.floor(window.innerHeight - absoluteTop - 24))
      setHeight(prev => Math.abs(prev - target) < 2 ? prev : target)
      // After committing, one more frame to reconcile any leftover body
      // overflow (e.g. an async resource card landed late). Subtract the
      // overshoot directly from the height.
      requestAnimationFrame(() => {
        const overflow = document.documentElement.scrollHeight - document.documentElement.clientHeight
        if (overflow > 4) {
          setHeight(prev => Math.max(240, prev - overflow))
        }
      })
    }
    const timers: number[] = []
    timers.push(requestAnimationFrame(compute))
    timers.push(window.setTimeout(compute, 100))
    timers.push(window.setTimeout(compute, 300))
    window.addEventListener('resize', compute)
    const ro = new ResizeObserver(compute)
    ro.observe(document.body)
    ro.observe(el)
    // Re-measure when the terminal becomes visible after a tab switch.
    const io = new IntersectionObserver((entries) => {
      for (const e of entries) if (e.isIntersecting) compute()
    })
    io.observe(el)
    return () => {
      timers.forEach((t, i) => i === 0 ? cancelAnimationFrame(t) : clearTimeout(t))
      window.removeEventListener('resize', compute)
      ro.disconnect()
      io.disconnect()
    }
  }, [])

  useEffect(() => {
    if (!termHostRef.current) return
    const term = new Terminal({
      fontFamily: 'Consolas, Menlo, "Courier New", monospace',
      fontSize: 13,
      cursorBlink: true,
      // Docker stdout emits LF only; convert to CRLF so xterm doesn't
      // produce the staircase indent.
      convertEol: true,
      scrollback: 5000,
      theme: { background: '#0e0e1a' },
    })
    const fit = new FitAddon()
    term.loadAddon(fit)
    term.open(termHostRef.current)
    fit.fit()

    const proto = location.protocol === 'https:' ? 'wss' : 'ws'

    // Wrap WebSocket in a tiny supervisor so a server hiccup or panel
    // restart doesn't leave the user stranded — close → 5 s timer →
    // re-open the same URL. The xterm itself stays mounted across
    // reconnects so scrollback survives.
    let ws: WebSocket
    let reconnectTimer: number | undefined
    let disposed = false
    const sendResize = () => {
      try {
        ws?.send(JSON.stringify({ type: 'resize', cols: term.cols, rows: term.rows }))
      } catch { /* ws not open yet */ }
    }
    const sendInput = (data: string) => {
      if (ws && ws.readyState === WebSocket.OPEN) {
        ws.send(JSON.stringify({ type: 'input', data }))
      }
    }

    // Per-instance line editor: handles arrow keys / history / Tab / Home /
    // End / Ctrl+A,E,U,W / CJK-aware backspace. Active only in localEcho
    // mode (i.e. when the server doesn't echo for us).
    const editor = localEcho
      ? createLineEditor({
          term,
          send: sendInput,
          getCandidates: () => completionRef.current?.() ?? [],
          historyKey: `taps:hist:${daemonId}/${uuid}`,
        })
      : null

    const connect = () => {
      // audit-2026-04-25 MED13: re-read the token on every connect.
      // The previous code captured token at mount time, so after a
      // password change (which revokes the old JWT via
      // TokensInvalidBefore) the supervisor would dial back with
      // the dead token forever. Reading getState() each time means
      // the next reconnect picks up the freshest token in the
      // store — including ones X-Refreshed-Token slid in.
      const token = useAuthStore.getState().token ?? ''
      const url = `${proto}://${location.host}/api/ws/instance/${daemonId}/${uuid}/terminal?token=${encodeURIComponent(token)}`
      ws = new WebSocket(url)
      ws.binaryType = 'arraybuffer'
      ws.onopen = () => sendResize()
      ws.onmessage = (e) => {
        const text = typeof e.data === 'string'
          ? e.data
          : new TextDecoder().decode(new Uint8Array(e.data))
        // If the user has a partially-typed line, briefly hide it before
        // writing server output and restore afterwards. Without this the
        // output overlays the in-progress text.
        if (editor && editor.hasInput()) {
          editor.hide()
          term.write(text)
          editor.render()
        } else {
          term.write(text)
        }
      }
      ws.onclose = () => {
        if (disposed) return
        term.writeln('\r\n\x1b[31m[disconnected, retrying in 5s…]\x1b[0m')
        reconnectTimer = window.setTimeout(() => {
          if (disposed) return
          term.writeln('\x1b[2m[reconnecting…]\x1b[0m')
          connect()
        }, 5000)
      }
    }
    connect()

    const dataDisp = term.onData((d) => {
      if (editor) editor.feed(d)
      else sendInput(d)
    })

    const onResize = () => {
      try { fit.fit() } catch { /* container not sized yet */ }
      sendResize()
    }
    window.addEventListener('resize', onResize)
    const ro = new ResizeObserver(() => onResize())
    ro.observe(termHostRef.current)

    const resizeDisp = term.onResize(() => sendResize())

    return () => {
      window.removeEventListener('resize', onResize)
      ro.disconnect()
      resizeDisp.dispose()
      dataDisp.dispose()
      disposed = true
      if (reconnectTimer) clearTimeout(reconnectTimer)
      ws?.close()
      term.dispose()
    }
  }, [daemonId, uuid, localEcho])

  return (
    <div
      ref={wrapRef}
      style={{
        width: '100%',
        height,
        background: '#0e0e1a',
        borderRadius: 8,
        overflow: 'hidden',
        // Padding lives on the wrapper (with border-box) so the host div
        // is the *exact* height that xterm's FitAddon measures. Putting
        // padding on the host instead would have FitAddon ignore it and
        // render an extra row that gets clipped by the rounded corner.
        padding: '8px 8px 6px',
        boxSizing: 'border-box',
      }}
    >
      <div ref={termHostRef} style={{ width: '100%', height: '100%' }} />
    </div>
  )
}
