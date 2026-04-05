type Position = {
  id: number
  symbolId: number
  orderSide: string
  price: number
  remainingAmount: number
  floatingProfit: number
}

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
            <div key={pos.id} className="flex justify-between text-sm">
              <span className={pos.orderSide === 'BUY' ? 'text-accent-green' : 'text-accent-red'}>
                {pos.orderSide === 'BUY' ? 'LONG' : 'SHORT'} {pos.remainingAmount}
              </span>
              <span className="text-text-secondary">
                @ ¥{pos.price.toLocaleString()}
              </span>
            </div>
          ))}
        </div>
      )}
    </div>
  )
}
