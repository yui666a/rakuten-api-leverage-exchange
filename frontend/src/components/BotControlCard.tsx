import type { StatusResponse } from '../lib/api'

type BotControlCardProps = {
  status: StatusResponse | undefined
  onStart: () => void
  onStop: () => void
  isPending: boolean
}

function getStatusLabel(status: StatusResponse | undefined): string {
  if (!status) return '読込中'
  if (status.manuallyStopped) return '手動停止'
  if (status.tradingHalted) return 'リスク停止'
  if (status.status === 'running') return '稼働中'
  return status.status ?? '不明'
}

export function BotControlCard({ status, onStart, onStop, isPending }: BotControlCardProps) {
  const current = getStatusLabel(status)
  const disabled = isPending
  const isRunning = status?.status === 'running' && !status?.manuallyStopped && !status?.tradingHalted
  const badgeClass = !status
    ? 'bg-white/6 text-slate-300'
    : isRunning
      ? 'bg-accent-green/18 text-accent-green'
      : 'bg-accent-red/18 text-accent-red'

  return (
    <section className="rounded-3xl border border-white/8 bg-bg-card/90 p-5 shadow-[0_12px_40px_rgba(0,0,0,0.22)]">
      <div className="flex items-start justify-between gap-4">
        <div>
          <p className="text-xs uppercase tracking-[0.28em] text-text-secondary">ボット制御</p>
          <h2 className="mt-2 text-xl font-semibold text-white">起動 / 停止</h2>
          <p className="mt-2 text-sm text-slate-300">現在状態: <span className="font-medium text-white">{current}</span></p>
        </div>
        <div className={`rounded-full border border-white/10 px-3 py-1 text-xs ${badgeClass}`}>
          {current}
        </div>
      </div>

      <div className="mt-5 flex gap-3">
        <button
          type="button"
          onClick={onStart}
          disabled={disabled}
          className="rounded-full bg-accent-green px-4 py-2 text-sm font-semibold text-slate-950 transition hover:brightness-110 disabled:cursor-not-allowed disabled:opacity-50"
        >
          起動
        </button>
        <button
          type="button"
          onClick={onStop}
          disabled={disabled}
          className="rounded-full bg-accent-red px-4 py-2 text-sm font-semibold text-white transition hover:brightness-110 disabled:cursor-not-allowed disabled:opacity-50"
        >
          停止
        </button>
      </div>
    </section>
  )
}
