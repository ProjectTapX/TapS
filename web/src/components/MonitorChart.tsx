import { ResponsiveContainer, LineChart, Line, XAxis, YAxis, Tooltip, CartesianGrid } from 'recharts'
import type { MonitorSnapshot } from '@/api/fs'

interface Props {
  data: MonitorSnapshot[]
  height?: number
}

export default function MonitorChart({ data, height = 140 }: Props) {
  const series = data.map(s => ({
    t: new Date(s.timestamp * 1000).toLocaleTimeString(),
    cpu: Number(s.cpuPercent.toFixed(1)),
    mem: Number(s.memPercent.toFixed(1)),
    disk: Number(s.diskPercent.toFixed(1)),
  }))
  return (
    <ResponsiveContainer width="100%" height={height}>
      <LineChart data={series} margin={{ top: 4, right: 8, left: -16, bottom: 0 }}>
        <CartesianGrid strokeDasharray="3 3" stroke="#333" />
        <XAxis dataKey="t" hide />
        <YAxis domain={[0, 100]} stroke="#888" fontSize={11} />
        <Tooltip
          contentStyle={{ background: '#1c1c2a', border: '1px solid #333', fontSize: 12 }}
          labelStyle={{ color: '#aaa' }}
        />
        <Line type="monotone" dataKey="cpu" stroke="#7c5cff" dot={false} strokeWidth={2} isAnimationActive={false} />
        <Line type="monotone" dataKey="mem" stroke="#36cfc9" dot={false} strokeWidth={2} isAnimationActive={false} />
        <Line type="monotone" dataKey="disk" stroke="#ff7a45" dot={false} strokeWidth={2} isAnimationActive={false} />
      </LineChart>
    </ResponsiveContainer>
  )
}
