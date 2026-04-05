import type { TradeHistoryItem } from '../lib/api'

type TradeHistoryTableProps = {
  trades: TradeHistoryItem[]
}

function formatYen(value: number) {
  return `¥${value.toLocaleString('ja-JP', { maximumFractionDigits: 0 })}`
}

function formatTimestamp(timestamp: number) {
  return new Date(timestamp).toLocaleString('ja-JP')
}

export function TradeHistoryTable({ trades }: TradeHistoryTableProps) {
  return (
    <div className="overflow-hidden rounded-3xl border border-white/8 bg-bg-card/90 shadow-[0_12px_40px_rgba(0,0,0,0.22)]">
      <div className="border-b border-white/8 px-5 py-4">
        <p className="text-xs uppercase tracking-[0.28em] text-text-secondary">Trade History</p>
        <h2 className="mt-2 text-xl font-semibold text-white">約定履歴</h2>
      </div>
      <div className="overflow-x-auto">
        <table className="min-w-full text-sm">
          <thead className="bg-white/4 text-left text-text-secondary">
            <tr>
              <th className="px-5 py-3 font-medium">日時</th>
              <th className="px-5 py-3 font-medium">方向</th>
              <th className="px-5 py-3 font-medium">数量</th>
              <th className="px-5 py-3 font-medium">価格</th>
              <th className="px-5 py-3 font-medium">損益</th>
              <th className="px-5 py-3 font-medium">Fee</th>
            </tr>
          </thead>
          <tbody>
            {trades.length === 0 ? (
              <tr>
                <td colSpan={6} className="px-5 py-10 text-center text-text-secondary">
                  約定履歴はありません
                </td>
              </tr>
            ) : (
              trades.map((trade) => (
                <tr key={trade.id} className="border-t border-white/6 text-slate-100">
                  <td className="px-5 py-4">{formatTimestamp(trade.createdAt)}</td>
                  <td className={`px-5 py-4 font-medium ${trade.orderSide === 'BUY' ? 'text-accent-green' : 'text-accent-red'}`}>
                    {trade.orderSide}
                  </td>
                  <td className="px-5 py-4">{trade.amount}</td>
                  <td className="px-5 py-4">{formatYen(trade.price)}</td>
                  <td className={`px-5 py-4 ${trade.profit >= 0 ? 'text-accent-green' : 'text-accent-red'}`}>
                    {formatYen(trade.profit)}
                  </td>
                  <td className="px-5 py-4">{formatYen(trade.fee)}</td>
                </tr>
              ))
            )}
          </tbody>
        </table>
      </div>
    </div>
  )
}
