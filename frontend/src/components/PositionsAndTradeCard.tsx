import { useState } from 'react'
import type { Position } from '../lib/api'
import { useManualOrder } from '../hooks/useManualOrder'

type Props = {
  symbolId: number
  positions: Position[] | undefined
  currencyPair?: string
  lotStep?: number
  minLot?: number
}

const DEFAULT_LOT_STEP = 0.1
const DEFAULT_MIN_LOT = 0.1

export function PositionsAndTradeCard({
  symbolId,
  positions,
  currencyPair,
  lotStep,
  minLot,
}: Props) {
  const step = lotStep ?? DEFAULT_LOT_STEP
  const min = minLot ?? DEFAULT_MIN_LOT
  const [amount, setAmount] = useState<number>(min)
  const [pending, setPending] = useState<{ side: 'BUY' | 'SELL'; amount: number } | null>(null)
  const [feedback, setFeedback] = useState<{ kind: 'ok' | 'err'; message: string } | null>(null)
  const order = useManualOrder()

  const submit = (side: 'BUY' | 'SELL') => {
    setFeedback(null)
    setPending({ side, amount })
  }

  const confirm = () => {
    if (!pending) return
    const { side, amount: amt } = pending
    setPending(null)
    order.mutate(
      { symbolId, side, amount: amt },
      {
        onSuccess: (res) => {
          if (res.executed) {
            setFeedback({ kind: 'ok', message: `${side} ${amt} 約定 (orderId=${res.orderId})` })
          } else {
            setFeedback({ kind: 'err', message: res.reason || '約定しませんでした' })
          }
        },
        onError: (err) => {
          setFeedback({ kind: 'err', message: err.message })
        },
      },
    )
  }

  const baseAsset = currencyPair?.split('_')[0] ?? ''
  const totalFloating = (positions ?? []).reduce((s, p) => s + p.floatingProfit, 0)

  return (
    <section className="rounded-3xl border border-white/8 bg-bg-card/90 p-5 shadow-[0_12px_40px_rgba(0,0,0,0.22)]">
      <header className="flex items-center justify-between">
        <div>
          <p className="text-xs uppercase tracking-[0.28em] text-text-secondary">
            Positions & Manual Trade
          </p>
          <h2 className="mt-2 text-xl font-semibold text-white">ポジション・手動取引</h2>
        </div>
        <span className="rounded-full bg-white/8 px-2.5 py-1 font-mono text-[11px] text-slate-300">
          {currencyPair?.replace('_', '/') ?? '—'}
        </span>
      </header>

      <div className="mt-4">
        <div className="mb-2 flex items-center justify-between text-xs text-text-secondary">
          <span>建玉 ({positions?.length ?? 0} 件)</span>
          {positions && positions.length > 0 && (
            <span
              className={
                totalFloating >= 0 ? 'text-accent-green' : 'text-accent-red'
              }
            >
              合計 {totalFloating >= 0 ? '+' : ''}¥{totalFloating.toLocaleString()}
            </span>
          )}
        </div>
        {!positions || positions.length === 0 ? (
          <div className="rounded-2xl border border-white/8 bg-white/3 p-3 text-center text-xs text-text-secondary">
            ポジションなし
          </div>
        ) : (
          <div className="max-h-56 space-y-2 overflow-y-auto pr-1">
            {positions.map((pos) => (
              <div
                key={pos.id}
                className="rounded-2xl border border-white/6 bg-white/3 p-3 text-sm"
              >
                <div className="flex justify-between">
                  <span
                    className={
                      pos.orderSide === 'BUY' ? 'text-accent-green' : 'text-accent-red'
                    }
                  >
                    {pos.orderSide === 'BUY' ? 'LONG' : 'SHORT'} {pos.remainingAmount}
                  </span>
                  <span
                    className={
                      pos.floatingProfit >= 0 ? 'text-accent-green' : 'text-accent-red'
                    }
                  >
                    {pos.floatingProfit >= 0 ? '+' : ''}¥
                    {pos.floatingProfit.toLocaleString()}
                  </span>
                </div>
                <div className="mt-1 text-text-secondary">
                  @ ¥{pos.price.toLocaleString()}
                </div>
              </div>
            ))}
          </div>
        )}
      </div>

      <div className="mt-5 border-t border-white/8 pt-4">
        <label className="block text-xs uppercase tracking-wide text-text-secondary">
          数量{baseAsset && ` (${baseAsset})`}
          <div className="mt-2 flex items-center gap-2">
            <button
              type="button"
              onClick={() => setAmount((a) => Math.max(min, +(a - step).toFixed(2)))}
              className="h-8 w-8 rounded-full border border-white/15 text-slate-200 hover:bg-white/10"
            >
              −
            </button>
            <input
              type="number"
              min={min}
              step={step}
              value={amount}
              onChange={(e) => {
                const v = parseFloat(e.target.value)
                if (Number.isFinite(v)) setAmount(v)
              }}
              className="flex-1 rounded-xl border border-white/10 bg-white/5 px-3 py-2 text-center text-base font-semibold text-white focus:border-accent-green/50 focus:outline-none"
            />
            <button
              type="button"
              onClick={() => setAmount((a) => +(a + step).toFixed(2))}
              className="h-8 w-8 rounded-full border border-white/15 text-slate-200 hover:bg-white/10"
            >
              +
            </button>
          </div>
        </label>

        <div className="mt-3 grid grid-cols-2 gap-3">
          <button
            type="button"
            onClick={() => submit('BUY')}
            disabled={order.isPending || amount < min}
            className="rounded-2xl bg-accent-green/90 px-4 py-3 text-base font-semibold text-bg-dark transition hover:bg-accent-green disabled:cursor-not-allowed disabled:opacity-50"
          >
            BUY (買い)
          </button>
          <button
            type="button"
            onClick={() => submit('SELL')}
            disabled={order.isPending || amount < min}
            className="rounded-2xl bg-accent-red/90 px-4 py-3 text-base font-semibold text-white transition hover:bg-accent-red disabled:cursor-not-allowed disabled:opacity-50"
          >
            SELL (売り)
          </button>
        </div>

        {feedback && (
          <p
            className={`mt-3 rounded-xl px-3 py-2 text-xs ${
              feedback.kind === 'ok'
                ? 'bg-accent-green/10 text-accent-green'
                : 'bg-accent-red/10 text-accent-red'
            }`}
          >
            {feedback.message}
          </p>
        )}

        <p className="mt-3 text-[11px] leading-relaxed text-text-secondary">
          成行注文のみ。送信後は確認ダイアログを出します。約定するとブラウザ通知 (有効時) と履歴・ポジションが自動で更新されます。
        </p>
      </div>

      {pending && (
        <ConfirmDialog
          side={pending.side}
          amount={pending.amount}
          currencyPair={currencyPair}
          baseAsset={baseAsset}
          onCancel={() => setPending(null)}
          onConfirm={confirm}
          pending={order.isPending}
        />
      )}
    </section>
  )
}

function ConfirmDialog({
  side,
  amount,
  currencyPair,
  baseAsset,
  onCancel,
  onConfirm,
  pending,
}: {
  side: 'BUY' | 'SELL'
  amount: number
  currencyPair?: string
  baseAsset: string
  onCancel: () => void
  onConfirm: () => void
  pending: boolean
}) {
  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/60 px-4 backdrop-blur-sm">
      <div className="w-full max-w-sm rounded-3xl border border-white/10 bg-bg-card p-6 shadow-[0_30px_80px_rgba(0,0,0,0.5)]">
        <h3 className="text-lg font-semibold text-white">注文を送信しますか？</h3>
        <dl className="mt-4 space-y-2 text-sm">
          <Row label="銘柄" value={currencyPair?.replace('_', '/') ?? '—'} />
          <Row label="方向" value={side === 'BUY' ? '買い (BUY)' : '売り (SELL)'} />
          <Row label="数量" value={baseAsset ? `${amount} ${baseAsset}` : `${amount}`} />
          <Row label="種別" value="成行 (MARKET)" />
        </dl>
        <p className="mt-4 text-xs text-text-secondary">
          実際の楽天ウォレット証拠金取引口座に注文が送信されます。
        </p>
        <div className="mt-5 grid grid-cols-2 gap-3">
          <button
            type="button"
            onClick={onCancel}
            disabled={pending}
            className="rounded-2xl border border-white/15 bg-white/5 px-4 py-2.5 text-sm font-semibold text-slate-200 hover:bg-white/10 disabled:opacity-50"
          >
            キャンセル
          </button>
          <button
            type="button"
            onClick={onConfirm}
            disabled={pending}
            className={`rounded-2xl px-4 py-2.5 text-sm font-semibold transition disabled:opacity-50 ${
              side === 'BUY'
                ? 'bg-accent-green text-bg-dark hover:brightness-110'
                : 'bg-accent-red text-white hover:brightness-110'
            }`}
          >
            {pending ? '送信中...' : '送信する'}
          </button>
        </div>
      </div>
    </div>
  )
}

function Row({ label, value }: { label: string; value: string }) {
  return (
    <div className="flex items-center justify-between">
      <dt className="text-text-secondary">{label}</dt>
      <dd className="font-mono text-white">{value}</dd>
    </div>
  )
}
