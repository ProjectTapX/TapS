import type { ReactNode } from 'react'
import { Card } from 'antd'

interface Props {
  label: ReactNode
  value: ReactNode
  hint?: ReactNode
  icon?: ReactNode
  accent?: string // hex
}

export default function StatTile({ label, value, hint, icon, accent = '#007BFC' }: Props) {
  return (
    <Card
      bodyStyle={{ padding: 18 }}
      style={{ overflow: 'hidden', position: 'relative' }}
    >
      <div style={{ display: 'flex', alignItems: 'flex-start', justifyContent: 'space-between' }}>
        <div>
          <div style={{ fontSize: 12, color: 'var(--taps-text-muted)', textTransform: 'uppercase', letterSpacing: '0.06em', fontWeight: 500 }}>
            {label}
          </div>
          <div style={{ marginTop: 8, fontSize: 26, fontWeight: 600, letterSpacing: '-0.02em', lineHeight: 1.2 }}>
            {value}
          </div>
          {hint && <div style={{ marginTop: 4, fontSize: 12, color: 'var(--taps-text-muted)' }}>{hint}</div>}
        </div>
        {icon && (
          <div style={{
            width: 40, height: 40, borderRadius: 10,
            display: 'flex', alignItems: 'center', justifyContent: 'center',
            background: accent + '14', color: accent, fontSize: 20,
          }}>
            {icon}
          </div>
        )}
      </div>
    </Card>
  )
}
