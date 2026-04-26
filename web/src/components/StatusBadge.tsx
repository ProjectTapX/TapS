import type { CSSProperties, ReactNode } from 'react'

type Variant = 'success' | 'warning' | 'danger' | 'info' | 'neutral' | 'processing'

const palette: Record<Variant, { fg: string; bg: string }> = {
  success:    { fg: '#047857', bg: '#d1fae5' },
  warning:    { fg: '#b45309', bg: '#fef3c7' },
  danger:     { fg: '#b91c1c', bg: '#fee2e2' },
  info:       { fg: '#1e40af', bg: '#dbeafe' },
  processing: { fg: '#3730a3', bg: '#e0e7ff' },
  neutral:    { fg: '#374151', bg: '#e5e7eb' },
}

interface Props {
  variant?: Variant
  children: ReactNode
  dot?: boolean
  style?: CSSProperties
}

export default function StatusBadge({ variant = 'neutral', children, dot = true, style }: Props) {
  const c = palette[variant]
  return (
    <span style={{
      display: 'inline-flex', alignItems: 'center', gap: 6,
      padding: '2px 10px',
      borderRadius: 999,
      background: c.bg, color: c.fg,
      fontSize: 12, fontWeight: 500, lineHeight: '20px',
      whiteSpace: 'nowrap',
      ...style,
    }}>
      {dot && <span style={{ width: 6, height: 6, borderRadius: '50%', background: c.fg }} />}
      {children}
    </span>
  )
}
