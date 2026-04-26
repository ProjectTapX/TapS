import type { ReactNode } from 'react'
import { Breadcrumb, Space } from 'antd'
import { Link } from 'react-router-dom'

interface CrumbItem {
  to?: string
  title: ReactNode
}

interface Props {
  title: ReactNode
  subtitle?: ReactNode
  crumbs?: CrumbItem[]
  extra?: ReactNode
}

export default function PageHeader({ title, subtitle, crumbs, extra }: Props) {
  return (
    <div style={{ marginBottom: 24 }}>
      {crumbs && crumbs.length > 0 && (
        <Breadcrumb
          style={{ marginBottom: 8, fontSize: 12 }}
          items={crumbs.map(c => ({
            title: c.to ? <Link to={c.to}>{c.title}</Link> : c.title,
          }))}
        />
      )}
      <div style={{ display: 'flex', alignItems: 'flex-start', justifyContent: 'space-between', gap: 16, flexWrap: 'wrap' }}>
        <div>
          <h1 style={{ margin: 0, fontSize: 24, fontWeight: 600, letterSpacing: '-0.01em' }}>{title}</h1>
          {subtitle && <div style={{ marginTop: 4, color: 'var(--taps-text-muted)', fontSize: 14 }}>{subtitle}</div>}
        </div>
        {extra && <Space wrap>{extra}</Space>}
      </div>
    </div>
  )
}
