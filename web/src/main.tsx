import React from 'react'
import ReactDOM from 'react-dom/client'
import { ConfigProvider, theme as antdTheme, App as AntApp } from 'antd'
import { RouterProvider } from 'react-router-dom'
import zhCN from 'antd/locale/zh_CN'
import enUS from 'antd/locale/en_US'
import jaJP from 'antd/locale/ja_JP'
import { router } from './router'
import './i18n'
import { useTranslation } from 'react-i18next'
import { useEffect } from 'react'
import { usePrefs } from './stores/auth'
import { useBrandStore } from './stores/brand'
import './index.css'

function Root() {
  const { i18n } = useTranslation()
  const lang = i18n.language?.startsWith('en') ? enUS : i18n.language?.startsWith('ja') ? jaJP : zhCN
  const mode = usePrefs((s) => s.theme)
  const loadBrand = useBrandStore((s) => s.load)

  // expose theme to CSS variables (used by index.css)
  useEffect(() => {
    document.documentElement.dataset.theme = mode
  }, [mode])

  // Pull brand (site name + favicon) once at boot so document.title
  // and the <link rel=icon> reflect the configured values for all
  // pages, including the bare login screen.
  useEffect(() => { loadBrand() }, [loadBrand])

  const isDark = mode === 'dark'
  return (
    <ConfigProvider
      locale={lang}
      theme={{
        algorithm: isDark ? antdTheme.darkAlgorithm : antdTheme.defaultAlgorithm,
        token: {
          colorPrimary: '#007BFC',
          colorSuccess: '#10b981',
          colorWarning: '#f59e0b',
          colorError:   '#ef4444',
          colorInfo:    '#007BFC',
          colorBgLayout: isDark ? '#0a0f1f' : '#f5f7fa',
          colorBgContainer: isDark ? '#131a2c' : '#ffffff',
          colorBorder: isDark ? '#1f2940' : '#e6e9ef',
          borderRadius: 10,
          borderRadiusLG: 14,
          borderRadiusSM: 6,
          fontFamily: "'Inter', -apple-system, BlinkMacSystemFont, 'Segoe UI', 'PingFang SC', 'Microsoft YaHei', sans-serif",
          fontSize: 14,
          controlHeight: 36,
          wireframe: false,
        },
        components: {
          Layout: {
            siderBg: 'transparent', // .taps-sider in index.css drives the actual color
            headerBg: isDark ? '#131a2c' : '#ffffff',
            headerHeight: 60,
            headerPadding: '0 24px',
            bodyBg: isDark ? '#0a0f1f' : '#f5f7fa',
          },
          Menu: {
            darkItemBg: 'transparent',
            darkSubMenuItemBg: 'transparent',
            darkItemSelectedBg: 'rgba(0,123,252,0.18)',
            darkItemHoverBg: 'rgba(255,255,255,0.04)',
            iconSize: 16,
          },
          Button: {
            controlHeight: 36,
            fontWeight: 500,
            primaryShadow: 'none',
            defaultShadow: 'none',
          },
          Card: {
            borderRadiusLG: 14,
            paddingLG: 20,
          },
          Table: {
            cellPaddingBlock: 12,
          },
          Tag: {
            borderRadiusSM: 999, // pill-shaped
          },
        },
      }}
    >
      <AntApp>
        <RouterProvider router={router} />
      </AntApp>
    </ConfigProvider>
  )
}

ReactDOM.createRoot(document.getElementById('root')!).render(
  <React.StrictMode>
    <Root />
  </React.StrictMode>,
)
