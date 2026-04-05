import type { StatusResponse } from '../lib/api'

type BotControlCardProps = {
  status: StatusResponse | undefined
  onStart: () => void
  onStop: () => void
  isPending: boolean
}

function getStatusLabel(status: StatusResponse | undefined): string {
  if (!status) return 'loading'
  if (status.manuallyStopped) return 'manual stop'
  if (status.tradingHalted) return 'risk halted'
  return 'running'
}

export function BotControlCard({ status, onStart, onStop, isPending }: BotControlCardProps) {
  const current = getStatusLabel(status)
  const disabled = isPending

  return (
    <section className="rounded-3xl border border-white/8 bg-bg-card/90 p-5 shadow-[0_12px_40px_rgba(0,0,0,0.22)]">
      <div className="flex items-start justify-between gap-4">
        <div>
          <p className="text-xs uppercase tracking-[0.28em] text-text-secondary">Bot Control</p>
          <h2 className="mt-2 text-xl font-semibold text-white">起動 / 停止</h2>
          <p className="mt-2 text-sm text-slate-300">現在状態: <span className="font-medium text-white">{current}</span></p>
        </div>
        <div className="rounded-full border border-white/10 bg-white/6 px-3 py-1 text-xs text-slate-300">
          {status?.status ?? 'unknown'}
        </div>
      </div>

      <div className="mt-5 flex gap-3">
        <button
          type="button"
          onClick={onStart}
          disabled={disabled}
          className="rounded-full bg-accent-green px-4 py-2 text-sm font-semibold text-slate-950 transition hover:brightness-110 disabled:cursor-not-allowed disabled:opacity-50"
        >
          Start
        </button>
        <button
          type="button"
          onClick={onStop}
          disabled={disabled}
          className="rounded-full bg-accent-red px-4 py-2 text-sm font-semibold text-white transition hover:brightness-110 disabled:cursor-not-allowed disabled:opacity-50"
        >
          Stop
        </button>
      </div>
    </section>
  )
}
