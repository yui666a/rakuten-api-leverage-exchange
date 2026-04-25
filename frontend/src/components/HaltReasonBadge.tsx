type HaltReasonBadgeProps = {
  haltReason?: string
  manuallyStopped?: boolean
  tradingHalted?: boolean
}

// Renders a critical-red badge when the bot is halted automatically (circuit
// breaker / reconciliation / daily-loss). Returns null when the bot is
// running or stopped via a plain manual /stop — in those cases the existing
// "ステータス" KPI carries the full message.
export function HaltReasonBadge({ haltReason, manuallyStopped, tradingHalted }: HaltReasonBadgeProps) {
  // Only show when an automatic halt left a reason — manual stops carry an
  // empty haltReason and don't deserve the red bar.
  if (!haltReason) return null
  // Some backend paths set both manuallyStopped + haltReason (auto halt is
  // implemented via manual_stop=true under the hood); we still want to show
  // the red bar then.
  void manuallyStopped
  void tradingHalted
  const [category, ...detail] = haltReason.split(':')
  const displayCategory = category === 'circuit_breaker'
    ? 'サーキットブレーカー'
    : category === 'reconciliation'
      ? '整合性違反'
      : category
  return (
    <div className="rounded-2xl border border-accent-red/40 bg-accent-red/10 px-4 py-3 text-sm text-accent-red shadow-[0_0_24px_rgba(255,80,80,0.15)]">
      <div className="flex items-center gap-3">
        <span className="text-base font-semibold">🚨 自動停止中</span>
        <span className="rounded-full bg-accent-red/20 px-2 py-0.5 font-mono text-[11px]">
          {displayCategory}
        </span>
      </div>
      {detail.length > 0 && (
        <p className="mt-1 font-mono text-xs text-accent-red/90">{detail.join(':')}</p>
      )}
      <p className="mt-2 text-[11px] text-accent-red/80">
        手動で /start を叩くまで自動売買は停止します。原因を確認してから再開してください。
      </p>
    </div>
  )
}
