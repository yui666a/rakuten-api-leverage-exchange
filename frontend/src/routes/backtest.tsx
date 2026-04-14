import { useState } from 'react'
import { createFileRoute } from '@tanstack/react-router'
import { AppFrame } from '../components/AppFrame'
import { useBacktestResults, useBacktestResult } from '../hooks/useBacktest'
import { EquityCurveChart } from '../components/EquityCurveChart'
import type { BacktestResult, BacktestTrade } from '../lib/api'

export const Route = createFileRoute('/backtest')({ component: BacktestPage })

function BacktestPage() {
  const [selectedId, setSelectedId] = useState('')
  const { data, isLoading, isError } = useBacktestResults()
  const { data: detail, isLoading: detailLoading } = useBacktestResult(selectedId)

  const results = data?.results ?? []

  return (
    <AppFrame
      title="Backtest Results"
      subtitle="過去のバックテスト結果の一覧と詳細を確認できます。"
    >
      {isError && (
        <div className="mb-4 rounded-2xl border border-accent-red/40 bg-accent-red/10 px-5 py-3 text-sm text-accent-red">
          バックテスト結果の取得に失敗しました。
        </div>
      )}

      {/* Results list */}
      <section className="rounded-3xl border border-white/8 bg-bg-card/90 p-5 shadow-[0_12px_40px_rgba(0,0,0,0.22)]">
        <p className="text-xs uppercase tracking-[0.28em] text-text-secondary">Results</p>
        <h2 className="mt-2 text-xl font-semibold text-white">バックテスト一覧</h2>

        {isLoading ? (
          <p className="mt-4 text-sm text-text-secondary">読み込み中...</p>
        ) : results.length === 0 ? (
          <p className="mt-4 text-sm text-text-secondary">
            バックテスト結果がありません。
          </p>
        ) : (
          <div className="mt-4 overflow-x-auto">
            <table className="w-full min-w-[800px] text-sm">
              <thead>
                <tr className="border-b border-white/8 text-left text-xs uppercase tracking-wider text-text-secondary">
                  <th className="px-3 py-2">ID</th>
                  <th className="px-3 py-2">Symbol</th>
                  <th className="px-3 py-2">期間</th>
                  <th className="px-3 py-2 text-right">Total Return</th>
                  <th className="px-3 py-2 text-right">Win Rate</th>
                  <th className="px-3 py-2 text-right">Sharpe</th>
                  <th className="px-3 py-2 text-right">Max DD</th>
                  <th className="px-3 py-2 text-right">Trades</th>
                  <th className="px-3 py-2">作成日</th>
                </tr>
              </thead>
              <tbody>
                {results.map((r) => (
                  <ResultRow
                    key={r.id}
                    result={r}
                    selected={r.id === selectedId}
                    onSelect={() => setSelectedId(r.id === selectedId ? '' : r.id)}
                  />
                ))}
              </tbody>
            </table>
          </div>
        )}
      </section>

      {/* Detail panel */}
      {selectedId !== '' && (
        <section className="mt-4 rounded-3xl border border-white/8 bg-bg-card/90 p-5 shadow-[0_12px_40px_rgba(0,0,0,0.22)]">
          {detailLoading ? (
            <p className="text-sm text-text-secondary">詳細を読み込み中...</p>
          ) : detail ? (
            <DetailPanel result={detail} />
          ) : (
            <p className="text-sm text-text-secondary">詳細を取得できませんでした。</p>
          )}
        </section>
      )}
    </AppFrame>
  )
}

/* ------------------------------------------------------------------ */
/* Result row                                                          */
/* ------------------------------------------------------------------ */

type ResultRowProps = {
  result: BacktestResult
  selected: boolean
  onSelect: () => void
}

function ResultRow({ result, selected, onSelect }: ResultRowProps) {
  const { config, summary } = result
  const periodFrom = new Date(config.fromTimestamp).toLocaleDateString('ja-JP')
  const periodTo = new Date(config.toTimestamp).toLocaleDateString('ja-JP')
  const created = new Date(result.createdAt * 1000).toLocaleDateString('ja-JP')

  return (
    <tr
      onClick={onSelect}
      className={`cursor-pointer border-b border-white/5 transition hover:bg-white/5 ${
        selected ? 'bg-white/8' : ''
      }`}
    >
      <td className="px-3 py-2.5 font-mono text-xs text-text-secondary">
        {result.id.slice(0, 8)}
      </td>
      <td className="px-3 py-2.5 text-white">{config.symbol}</td>
      <td className="px-3 py-2.5 text-text-secondary">
        {periodFrom} - {periodTo}
      </td>
      <td className={`px-3 py-2.5 text-right font-medium ${pnlColor(summary.totalReturn)}`}>
        {formatPercent(summary.totalReturn)}
      </td>
      <td className="px-3 py-2.5 text-right text-white">
        {summary.winRate.toFixed(1)}%
      </td>
      <td className="px-3 py-2.5 text-right text-white">
        {summary.sharpeRatio.toFixed(2)}
      </td>
      <td className="px-3 py-2.5 text-right text-accent-red">
        {formatPercent(summary.maxDrawdown)}
      </td>
      <td className="px-3 py-2.5 text-right text-white">{summary.totalTrades}</td>
      <td className="px-3 py-2.5 text-text-secondary">{created}</td>
    </tr>
  )
}

/* ------------------------------------------------------------------ */
/* Detail panel                                                        */
/* ------------------------------------------------------------------ */

function DetailPanel({ result }: { result: BacktestResult }) {
  const { config, summary } = result
  const periodFrom = new Date(config.fromTimestamp).toLocaleDateString('ja-JP')
  const periodTo = new Date(config.toTimestamp).toLocaleDateString('ja-JP')

  return (
    <div>
      <div className="flex flex-wrap items-baseline gap-3">
        <p className="text-xs uppercase tracking-[0.28em] text-text-secondary">Detail</p>
        <h2 className="text-xl font-semibold text-white">
          {config.symbol} / {periodFrom} - {periodTo}
        </h2>
      </div>

      {/* Config info */}
      <div className="mt-3 flex flex-wrap gap-3 text-xs text-text-secondary">
        <span>Interval: {config.primaryInterval}</span>
        <span>Higher TF: {config.higherTfInterval}</span>
        <span>Spread: {config.spreadPercent}%</span>
        <span>Slippage: {config.slippagePercent}%</span>
        <span>Carry Cost: {config.dailyCarryCost}</span>
      </div>

      {/* KPI cards */}
      <div className="mt-5 grid gap-3 sm:grid-cols-2 lg:grid-cols-4">
        <KpiCard
          label="Final Balance"
          value={`\u00a5${summary.finalBalance.toLocaleString('ja-JP', { maximumFractionDigits: 0 })}`}
          color={pnlColor(summary.finalBalance - summary.initialBalance)}
        />
        <KpiCard
          label="Total Return"
          value={formatPercent(summary.totalReturn)}
          color={pnlColor(summary.totalReturn)}
        />
        <KpiCard label="Win / Lose" value={`${summary.winTrades} / ${summary.lossTrades}`} />
        <KpiCard
          label="Win Rate"
          value={`${summary.winRate.toFixed(1)}%`}
        />
        <KpiCard
          label="Profit Factor"
          value={summary.profitFactor.toFixed(2)}
          color={summary.profitFactor >= 1 ? 'text-accent-green' : 'text-accent-red'}
        />
        <KpiCard label="Sharpe Ratio" value={summary.sharpeRatio.toFixed(2)} />
        <KpiCard
          label="Max Drawdown"
          value={formatPercent(summary.maxDrawdown)}
          color="text-accent-red"
        />
        <KpiCard
          label="Max DD Balance"
          value={`\u00a5${summary.maxDrawdownBalance.toLocaleString('ja-JP', { maximumFractionDigits: 0 })}`}
          color="text-accent-red"
        />
        <KpiCard
          label="Avg Hold Time"
          value={formatHoldTime(summary.avgHoldSeconds)}
        />
        <KpiCard
          label="Total Trades"
          value={String(summary.totalTrades)}
        />
        <KpiCard
          label="Carrying Cost"
          value={`\u00a5${summary.totalCarryingCost.toLocaleString('ja-JP', { maximumFractionDigits: 0 })}`}
        />
        <KpiCard
          label="Spread Cost"
          value={`\u00a5${summary.totalSpreadCost.toLocaleString('ja-JP', { maximumFractionDigits: 0 })}`}
        />
      </div>

      {/* Equity curve */}
      {result.trades && result.trades.length > 0 && (
        <div className="mt-6">
          <p className="text-xs uppercase tracking-[0.28em] text-text-secondary">Equity Curve</p>
          <h3 className="mt-2 text-lg font-semibold text-white">資産推移</h3>
          <div className="mt-3 h-[400px]">
            <EquityCurveChart
              trades={result.trades}
              initialBalance={result.summary.initialBalance}
              periodFrom={result.config.fromTimestamp}
              periodTo={result.config.toTimestamp}
            />
          </div>
        </div>
      )}

      {/* Trades table */}
      {result.trades && result.trades.length > 0 && (
        <div className="mt-6">
          <p className="text-xs uppercase tracking-[0.28em] text-text-secondary">Trades</p>
          <h3 className="mt-2 text-lg font-semibold text-white">
            取引一覧 ({result.trades.length} 件)
          </h3>
          <div className="mt-3 overflow-x-auto">
            <table className="w-full min-w-[900px] text-sm">
              <thead>
                <tr className="border-b border-white/8 text-left text-xs uppercase tracking-wider text-text-secondary">
                  <th className="px-3 py-2">#</th>
                  <th className="px-3 py-2">Side</th>
                  <th className="px-3 py-2">Entry</th>
                  <th className="px-3 py-2">Exit</th>
                  <th className="px-3 py-2 text-right">Entry Price</th>
                  <th className="px-3 py-2 text-right">Exit Price</th>
                  <th className="px-3 py-2 text-right">Amount</th>
                  <th className="px-3 py-2 text-right">PnL</th>
                  <th className="px-3 py-2 text-right">PnL %</th>
                  <th className="px-3 py-2">Entry Reason</th>
                  <th className="px-3 py-2">Exit Reason</th>
                </tr>
              </thead>
              <tbody>
                {result.trades.map((t) => (
                  <TradeRow key={t.tradeId} trade={t} />
                ))}
              </tbody>
            </table>
          </div>
        </div>
      )}
    </div>
  )
}

/* ------------------------------------------------------------------ */
/* Trade row                                                           */
/* ------------------------------------------------------------------ */

function TradeRow({ trade }: { trade: BacktestTrade }) {
  const entry = new Date(trade.entryTime).toLocaleString('ja-JP')
  const exit = new Date(trade.exitTime).toLocaleString('ja-JP')

  return (
    <tr className="border-b border-white/5 hover:bg-white/5">
      <td className="px-3 py-2 text-text-secondary">{trade.tradeId}</td>
      <td className={`px-3 py-2 font-medium ${trade.side === 'BUY' ? 'text-accent-green' : 'text-accent-red'}`}>
        {trade.side}
      </td>
      <td className="px-3 py-2 text-text-secondary text-xs">{entry}</td>
      <td className="px-3 py-2 text-text-secondary text-xs">{exit}</td>
      <td className="px-3 py-2 text-right text-white">
        {trade.entryPrice.toLocaleString('ja-JP')}
      </td>
      <td className="px-3 py-2 text-right text-white">
        {trade.exitPrice.toLocaleString('ja-JP')}
      </td>
      <td className="px-3 py-2 text-right text-white">{trade.amount}</td>
      <td className={`px-3 py-2 text-right font-medium ${pnlColor(trade.pnl)}`}>
        {trade.pnl >= 0 ? '+' : ''}{trade.pnl.toLocaleString('ja-JP', { maximumFractionDigits: 0 })}
      </td>
      <td className={`px-3 py-2 text-right ${pnlColor(trade.pnlPercent)}`}>
        {formatPercent(trade.pnlPercent)}
      </td>
      <td className="px-3 py-2 text-xs text-text-secondary">{trade.reasonEntry}</td>
      <td className="px-3 py-2 text-xs text-text-secondary">{trade.reasonExit}</td>
    </tr>
  )
}

/* ------------------------------------------------------------------ */
/* KPI card                                                            */
/* ------------------------------------------------------------------ */

type KpiCardProps = {
  label: string
  value: string
  color?: string
}

function KpiCard({ label, value, color = 'text-white' }: KpiCardProps) {
  return (
    <div className="rounded-2xl border border-white/8 bg-white/4 p-4">
      <p className="text-xs uppercase tracking-[0.25em] text-text-secondary">{label}</p>
      <p className={`mt-2 text-lg font-semibold ${color}`}>{value}</p>
    </div>
  )
}

/* ------------------------------------------------------------------ */
/* Helpers                                                             */
/* ------------------------------------------------------------------ */

function pnlColor(value: number): string {
  if (value > 0) return 'text-accent-green'
  if (value < 0) return 'text-accent-red'
  return 'text-white'
}

function formatPercent(value: number): string {
  return `${(value * 100).toFixed(2)}%`
}

function formatHoldTime(seconds: number): string {
  if (seconds < 60) return `${Math.round(seconds)}s`
  if (seconds < 3600) return `${Math.round(seconds / 60)}m`
  if (seconds < 86400) return `${(seconds / 3600).toFixed(1)}h`
  return `${(seconds / 86400).toFixed(1)}d`
}
