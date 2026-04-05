type KpiCardProps = {
  label: string
  value: string
  color?: string
}

export function KpiCard({ label, value, color = 'text-text-primary' }: KpiCardProps) {
  return (
    <div className="rounded-3xl border border-white/8 bg-bg-card/90 p-4 text-center shadow-[0_12px_36px_rgba(0,0,0,0.18)]">
      <div className={`text-2xl font-bold ${color}`}>{value}</div>
      <div className="text-text-secondary text-xs mt-1">{label}</div>
    </div>
  )
}
