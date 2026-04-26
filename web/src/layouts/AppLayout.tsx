import { Layout, Menu, Button, Space, Select, Dropdown, Avatar, type MenuProps } from 'antd'
import {
  DashboardOutlined,
  UserOutlined,
  LogoutOutlined,
  ClusterOutlined,
  AppstoreOutlined,
  KeyOutlined,
  ContainerOutlined,
  HddOutlined,
  SunOutlined,
  MoonOutlined,
  AuditOutlined,
  HistoryOutlined,
  SettingOutlined,
  LockOutlined,
  TranslationOutlined,
} from '@ant-design/icons'
import { Outlet, useLocation, useNavigate } from 'react-router-dom'
import { useTranslation } from 'react-i18next'
import { useEffect, useState } from 'react'
import { useAuthStore, usePrefs } from '@/stores/auth'
import { useBrandStore } from '@/stores/brand'
import ChangePasswordModal from '@/components/ChangePasswordModal'
import { api } from '@/api/client'
import { authConfigApi, type LoginMethod } from '@/api/resources'

const { Header, Sider, Content } = Layout

export default function AppLayout() {
  const nav = useNavigate()
  const loc = useLocation()
  const { t, i18n } = useTranslation()
  const user = useAuthStore((s) => s.user)
  const setUser = useAuthStore((s) => s.setUser)
  const logout = useAuthStore((s) => s.logout)
  const isAdmin = user?.role === 'admin'
  const themeMode = usePrefs((s) => s.theme)
  const setTheme = usePrefs((s) => s.setTheme)
  const [pwdOpen, setPwdOpen] = useState(false)
  // loginMethod gates which account-management actions we expose
  // (change/set password, account page). Initial value is null so we
  // *don't* render any of those choices before the server tells us
  // which mode the panel is actually in. The previous default of
  // 'password-only' caused the change-password modal to flash open on
  // oidc-only deployments whenever a user with mustChangePassword
  // mounted the layout. `loginMethodFailed` surfaces fetch errors as
  // a disabled menu line instead of silently falling back to a
  // concrete (and possibly wrong) mode.
  const [loginMethod, setLoginMethod] = useState<LoginMethod | null>(null)
  const [loginMethodFailed, setLoginMethodFailed] = useState(false)

  useEffect(() => {
    // Skip the /auth/me round-trip if the auth store already holds a
    // user — typically the case right after login (password or OIDC
    // hash handler), where setAuth() pre-populated everything we need.
    // Fetching anyway burned an extra request per session and, more
    // importantly, doubled the audit-log noise for what is already a
    // hot endpoint. We refresh on mounts where user is missing
    // (cold reload with only a token in localStorage).
    if (!user) {
      api.get('/auth/me').then(r => setUser(r.data)).catch(() => { /* ignore */ })
    }
    authConfigApi.getMethod()
      .then(m => { setLoginMethod(m); setLoginMethodFailed(false) })
      .catch(() => { setLoginMethod(null); setLoginMethodFailed(true) })
  }, [])

  useEffect(() => {
    // Wait until we know the mode — never auto-open the modal in
    // ambiguous state. oidc-only never opens it; other modes do.
    if (loginMethod === null) return
    if (loginMethod === 'oidc-only') return
    if (user?.mustChangePassword) setPwdOpen(true)
  }, [user?.mustChangePassword, loginMethod])

  const items = isAdmin ? [
    { key: '/dashboard', icon: <DashboardOutlined />, label: t('menu.dashboard') },
    { key: '/instances', icon: <AppstoreOutlined />, label: t('menu.instances') },
    { key: '/nodes', icon: <ClusterOutlined />, label: t('menu.nodes') },
    { key: '/groups', icon: <ClusterOutlined />, label: t('menu.groups') },
    { key: '/images', icon: <ContainerOutlined />, label: t('menu.images') },
    { key: '/volumes', icon: <HddOutlined />, label: t('menu.volumes') },
    { key: '/users', icon: <UserOutlined />, label: t('menu.users') },
    { key: '/apikeys', icon: <KeyOutlined />, label: t('menu.apikeys') },
    { key: '/audit', icon: <AuditOutlined />, label: t('menu.audit') },
    { key: '/logins', icon: <HistoryOutlined />, label: t('menu.logins') },
    { key: '/settings', icon: <SettingOutlined />, label: t('menu.settings') },
  ] : [
    // Non-admin users: dashboard + instance control + own API keys.
    { key: '/dashboard', icon: <DashboardOutlined />, label: t('menu.dashboard') },
    { key: '/instances', icon: <AppstoreOutlined />, label: t('menu.instances') },
    { key: '/apikeys', icon: <KeyOutlined />, label: t('menu.apikeys') },
  ]

  // While loginMethod is unknown (null), hide both account/password
  // menu items rather than guess. If the fetch outright failed, show
  // a single disabled hint so the operator knows why those actions
  // are missing instead of staring at a silently-pruned menu.
  const passwordMenuItem = loginMethod === null || loginMethod === 'oidc-only'
    ? null
    : { key: 'pwd', icon: <LockOutlined />, label: user?.hasPassword === false ? t('user.setPassword') : t('user.changePassword'), onClick: () => setPwdOpen(true) }
  const accountMenuItem = loginMethod === null || loginMethod === 'oidc-only'
    ? null
    : { key: 'account', icon: <UserOutlined />, label: t('user.account'), onClick: () => nav('/account') }
  const loginMethodFailedItem = loginMethodFailed
    ? { key: 'lmFail', icon: <LockOutlined />, label: t('user.loginMethodUnavailable'), disabled: true }
    : null

  const userMenu: MenuProps['items'] = [
    {
      key: 'who',
      label: (
        <div style={{ padding: '4px 0' }}>
          <div style={{ fontWeight: 600 }}>{user?.username}</div>
          <div style={{ color: 'var(--taps-text-muted)', fontSize: 12 }}>{user?.role}</div>
        </div>
      ),
      disabled: true,
    },
    { type: 'divider' },
    ...(accountMenuItem ? [accountMenuItem] : []),
    ...(passwordMenuItem ? [passwordMenuItem] : []),
    ...(loginMethodFailedItem ? [loginMethodFailedItem] : []),
    { key: 'logout', icon: <LogoutOutlined />, label: t('common.logout'), danger: true, onClick: () => { logout(); nav('/login') } },
  ]

  const currentItem = items.find((i) => i.key === loc.pathname)
                    ?? (loc.pathname.startsWith('/instances/') ? items.find(i => i.key === '/instances') : null)

  return (
    <Layout style={{ minHeight: '100vh' }}>
      <Sider breakpoint="lg" collapsedWidth={0} width={232} className="taps-sider" theme={themeMode === 'dark' ? 'dark' : 'light'}>
        <div style={{
          padding: '20px 24px 24px', color: 'var(--taps-sider-brand-fg)',
        }}>
          <div style={{ fontWeight: 700, fontSize: 22, letterSpacing: '-0.02em' }}>{useBrandStore((s) => s.siteName)}</div>
          <div style={{ color: 'var(--taps-sider-text-muted)', fontSize: 11, lineHeight: 1.2, marginTop: 2 }}>Server Manager</div>
        </div>
        <Menu theme={themeMode === 'dark' ? 'dark' : 'light'} mode="inline" selectedKeys={[loc.pathname]} items={items} onClick={(e) => nav(e.key)} />
        <div style={{
          position: 'absolute', bottom: 16, left: 0, right: 0, padding: '0 24px',
          color: 'var(--taps-sider-text-muted)', fontSize: 11,
        }}>
          v{__APP_VERSION__}
        </div>
      </Sider>
      <Layout>
        <Header style={{
          display: 'flex', alignItems: 'center', justifyContent: 'space-between',
          padding: '0 24px',
          borderBottom: '1px solid var(--taps-border)',
        }}>
          <div style={{ fontSize: 15, fontWeight: 600 }}>
            {currentItem?.label ?? ''}
          </div>
          <Space size={8}>
            <Button type="text" shape="circle" icon={themeMode === 'dark' ? <SunOutlined /> : <MoonOutlined />}
              onClick={() => setTheme(themeMode === 'dark' ? 'light' : 'dark')} />
            <Select size="small" variant="borderless"
              value={i18n.language?.startsWith('en') ? 'en' : i18n.language?.startsWith('ja') ? 'ja' : 'zh'}
              onChange={(v) => i18n.changeLanguage(v)}
              suffixIcon={<TranslationOutlined />}
              options={[{ label: '中文', value: 'zh' }, { label: 'English', value: 'en' }, { label: '日本語', value: 'ja' }]}
              style={{ width: 110 }} />
            <Dropdown menu={{ items: userMenu }} placement="bottomRight" trigger={['click']}>
              <Button type="text" style={{ padding: '4px 8px' }}>
                <Space>
                  <Avatar size={28} style={{ background: '#007BFC', fontSize: 13 }}>
                    {(user?.username ?? '?').slice(0,1).toUpperCase()}
                  </Avatar>
                  <span style={{ fontWeight: 500 }}>{user?.username}</span>
                </Space>
              </Button>
            </Dropdown>
          </Space>
        </Header>
        <Content style={{ padding: 24 }}>
          <div className="taps-page" key={loc.pathname}>
            <Outlet />
          </div>
        </Content>
      </Layout>

      <ChangePasswordModal open={pwdOpen} onClose={() => setPwdOpen(false)} forced={!!user?.mustChangePassword} />
    </Layout>
  )
}
