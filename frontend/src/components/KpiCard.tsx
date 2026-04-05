type KpiCardProps = {
  label: string
  value: string
  color?: string
}

export function KpiCard({ label, value, color = 'text-text-primary' }: KpiCardProps) {
  return (
    <div className="bg-bg-card rounded-lg p-4 text-center">
      <div className={`text-2xl font-bold ${color}`}>{value}</div>
      <div className="text-text-secondary text-xs mt-1">{label}</div>
    </div>
  )
}
