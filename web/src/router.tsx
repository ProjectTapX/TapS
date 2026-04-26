import { lazy, Suspense, useEffect, useState } from 'react'
import { createBrowserRouter, Navigate, Outlet } from 'react-router-dom'
import { Spin } from 'antd'
import { useAuthStore } from '@/stores/auth'
import AppLayout from '@/layouts/AppLayout'
import ChunkErrorBoundary from '@/components/ChunkErrorBoundary'
import { api } from '@/api/client'

const LoginPage = lazy(() => import('@/pages/login'))
const DashboardPage = lazy(() => import('@/pages/dashboard'))
const UsersPage = lazy(() => import('@/pages/users'))
const NodesPage = lazy(() => import('@/pages/nodes'))
const GroupsPage = lazy(() => import('@/pages/groups'))
const InstancesPage = lazy(() => import('@/pages/instances'))
const InstanceDetailPage = lazy(() => import('@/pages/instances/detail'))
const ApiKeysPage = lazy(() => import('@/pages/apikeys'))
const ImagesPage = lazy(() => import('@/pages/images'))
const VolumesPage = lazy(() => import('@/pages/volumes'))
const AuditPage = lazy(() => import('@/pages/audit'))
const LoginsPage = lazy(() => import('@/pages/logins'))
const SettingsPage = lazy(() => import('@/pages/settings'))
const AccountPage = lazy(() => import('@/pages/account'))

function Fallback() {
  return <div style={{ padding: 24, textAlign: 'center' }}><Spin /></div>
}

// useHydrated waits for zustand-persist to finish reading localStorage.
// Until that completes, useAuthStore.getState().token reads the initial
// value (null) regardless of what's actually stored — so any guard that
// reads token synchronously on first paint can false-negative and bounce
// the user to /login. Sync localStorage usually finishes before paint,
// but isn't guaranteed (slow disk, custom storage shims, future swap to
// IndexedDB). Gating Protected/HomeRedirect on this avoids that race.
function useHydrated() {
  const [hydrated, setHydrated] = useState(() => useAuthStore.persist.hasHydrated())
  useEffect(() => {
    if (hydrated) return
    const unsub = useAuthStore.persist.onFinishHydration(() => setHydrated(true))
    if (useAuthStore.persist.hasHydrated()) setHydrated(true)
    return unsub
  }, [hydrated])
  return hydrated
}

function Lazy({ Cmp }: { Cmp: React.LazyExoticComponent<any> }) {
  // ChunkErrorBoundary catches the post-deploy "old hash, new server"
  // failure mode that <Suspense> alone surfaces as a permanent spinner.
  // The boundary lives inside Lazy so each lazy route gets its own
  // recovery affordance — top-level placement would also work but
  // would hide whatever loaded fine while one chunk failed.
  return (
    <ChunkErrorBoundary>
      <Suspense fallback={<Fallback />}><Cmp /></Suspense>
    </ChunkErrorBoundary>
  )
}

function Protected() {
  const hydrated = useHydrated()
  const token = useAuthStore((s) => s.token)
  // audit-2026-04-25 MED12: persist only stores token + immutable
  // identity (id/username/role). Volatile flags like
  // mustChangePassword / hasPassword have to be re-fetched after
  // hydration so a server-side state change (admin clears
  // mustChangePassword, user claims a real password from another
  // tab) takes effect on the next mount instead of waiting for the
  // next manual /auth/me call.
  useEffect(() => {
    if (!hydrated || !token) return
    api.get('/auth/me')
      .then((r) => useAuthStore.getState().setUser(r.data))
      .catch(() => { /* 401 handled by axios interceptor */ })
  }, [hydrated, token])
  if (!hydrated) return <Fallback />
  // Preserve the URL hash so a fresh OIDC callback landing on `/`
  // (`/#oauth-token=...`) still reaches the login page, which is where
  // the hash-fragment handler lives. <Navigate to="/login"> alone
  // would drop the hash and silently lose the token.
  if (!token) return <Navigate to={{ pathname: '/login', hash: window.location.hash }} replace />
  return <Outlet />
}

function AdminOnly() {
  const role = useAuthStore((s) => s.user?.role)
  if (role !== 'admin') return <Navigate to="/instances" replace />
  return <Outlet />
}

function HomeRedirect() {
  const hydrated = useHydrated()
  const token = useAuthStore((s) => s.token)
  if (!hydrated) return <Fallback />
  if (!token) return <Navigate to={{ pathname: '/login', hash: window.location.hash }} replace />
  return <Navigate to="/dashboard" replace />
}

export const router = createBrowserRouter([
  { path: '/login', element: <Lazy Cmp={LoginPage} /> },
  {
    element: <Protected />,
    children: [
      {
        element: <AppLayout />,
        children: [
          { path: '/', element: <HomeRedirect /> },
          { path: '/dashboard', element: <Lazy Cmp={DashboardPage} /> },
          { path: '/instances', element: <Lazy Cmp={InstancesPage} /> },
          { path: '/instances/:daemonId/:uuid', element: <Lazy Cmp={InstanceDetailPage} /> },
          { path: '/apikeys', element: <Lazy Cmp={ApiKeysPage} /> },
          { path: '/account', element: <Lazy Cmp={AccountPage} /> },
          // admin-only routes
          {
            element: <AdminOnly />,
            children: [
              { path: '/nodes', element: <Lazy Cmp={NodesPage} /> },
              { path: '/groups', element: <Lazy Cmp={GroupsPage} /> },
              { path: '/users', element: <Lazy Cmp={UsersPage} /> },
              { path: '/images', element: <Lazy Cmp={ImagesPage} /> },
              { path: '/volumes', element: <Lazy Cmp={VolumesPage} /> },
              { path: '/audit', element: <Lazy Cmp={AuditPage} /> },
              { path: '/logins', element: <Lazy Cmp={LoginsPage} /> },
              { path: '/settings', element: <Lazy Cmp={SettingsPage} /> },
            ],
          },
        ],
      },
    ],
  },
])
