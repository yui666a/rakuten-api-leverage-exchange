import type { Position } from '../lib/api'

type PositionPanelProps = {
  positions: Position[] | undefined
}

export function PositionPanel({ positions }: PositionPanelProps) {
  return (
    <div className="bg-bg-card rounded-lg p-4">
      <div className="text-text-secondary text-xs mb-3">ポジション</div>
      {!positions || positions.length === 0 ? (
        <div className="text-text-secondary text-sm">ポジションなし</div>
      ) : (
        <div className="space-y-2">
          {positions.map((pos) => (
            <div key={pos.id} className="rounded-2xl border border-white/6 bg-white/3 p-3 text-sm">
              <div className="flex justify-between">
                <span className={pos.orderSide === 'BUY' ? 'text-accent-green' : 'text-accent-red'}>
                  {pos.orderSide === 'BUY' ? 'LONG' : 'SHORT'} {pos.remainingAmount}
                </span>
                <span className={pos.floatingProfit >= 0 ? 'text-accent-green' : 'text-accent-red'}>
                  {pos.floatingProfit >= 0 ? '+' : ''}¥{pos.floatingProfit.toLocaleString()}
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
  )
}
