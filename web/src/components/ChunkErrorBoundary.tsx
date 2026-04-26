import React from 'react'
import { Result, Button } from 'antd'
import i18n from '@/i18n'

interface Props {
  children: React.ReactNode
}
interface State {
  chunkFailed: boolean
}

// ChunkErrorBoundary catches the specific class of runtime error that
// happens when a deployed SPA's index.html still references an asset
// chunk hash that the new server build no longer ships — the user is
// holding a stale tab from before the deploy. React's default behavior
// is to surface the error all the way up and unmount the tree, leaving
// users staring at a permanent <Spin /> from <Suspense> with no signal
// that "just refresh" would fix it.
//
// We deliberately scope this to chunk-load errors only and let any
// other render-time exception propagate so dev-time stack traces and
// genuine bugs stay visible.
export default class ChunkErrorBoundary extends React.Component<Props, State> {
  state: State = { chunkFailed: false }

  static getDerivedStateFromError(error: Error): State | null {
    if (
      error?.name === 'ChunkLoadError' ||
      /Loading chunk \d+ failed/.test(error?.message ?? '')
    ) {
      return { chunkFailed: true }
    }
    // audit-2026-04-25 MED11: returning null leaves the boundary's
    // state untouched. React then re-renders our children, which
    // throw the same error again — but this time componentDidCatch
    // below logs it via console.error so it stays visible in the
    // browser DevTools (and in Sentry-style tools that hook into
    // window.onerror). Throwing from getDerivedStateFromError is
    // an anti-pattern: React treats it as the boundary itself
    // crashing and unmounts the entire subtree, replacing the page
    // with a blank screen instead of letting the error surface.
    return null
  }

  componentDidCatch(error: Error, info: React.ErrorInfo) {
    if (this.state.chunkFailed) {
      console.warn('[chunk-load] stale bundle detected, prompting refresh:', error.message)
      return
    }
    // Non-chunk errors: surface to the console with the full
    // component stack so devs see the failure even though we
    // intentionally let the children re-render (and re-throw).
    console.error('[ChunkErrorBoundary] uncaught error in subtree:', error, info?.componentStack)
  }

  render() {
    if (this.state.chunkFailed) {
      return (
        <Result
          status="warning"
          title={i18n.t('router.chunkExpired.title')}
          subTitle={i18n.t('router.chunkExpired.subtitle')}
          extra={
            <Button type="primary" onClick={() => location.reload()}>
              {i18n.t('router.chunkExpired.action')}
            </Button>
          }
        />
      )
    }
    return this.props.children
  }
}
