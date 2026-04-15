import { useEffect, useMemo, useState, type FormEvent } from 'react'
import { createFileRoute } from '@tanstack/react-router'
import { AppFrame } from '../components/AppFrame'
import {
  useBacktestCSVMeta,
  useBacktestResults,
  useBacktestResult,
  useRunBacktest,
} from '../hooks/useBacktest'
import { useSymbols } from '../hooks/useSymbols'
import { EquityCurveChart } from '../components/EquityCurveChart'
import type { BacktestResult, BacktestRunRequest, BacktestTrade } from '../lib/api'

export const Route = createFileRoute('/backtest')({ component: BacktestPage })

type BacktestRunForm = {
  data: string
  dataHtf: string
  from: string
  to: string
  initialBalance: string
  spread: string
  carryingCost: string
  slippage: string
  tradeAmount: string
  stopLossPercent: string
  takeProfitPercent: string
  maxPositionAmount: string
  maxDailyLoss: string
  maxConsecutiveLosses: string
  cooldownMinutes: string
}

const defaultRunForm: BacktestRunForm = {
  data: 'data/candles_BTC_JPY_PT15M.csv',
  dataHtf: 'data/candles_BTC_JPY_PT1H.csv',
  from: '',
  to: '',
  initialBalance: '100000',
  spread: '0.1',
  carryingCost: '0.04',
  slippage: '0',
  tradeAmount: '0.01',
  stopLossPercent: '5',
  takeProfitPercent: '10',
  maxPositionAmount: '',
  maxDailyLoss: '',
  maxConsecutiveLosses: '',
  cooldownMinutes: '',
}

const fallbackBacktestPairs = ['BTC_JPY', 'LTC_JPY'] as const

function buildAutoCSVPaths(currencyPair: string) {
  return {
    primary: `data/candles_${currencyPair}_PT15M.csv`,
    higherTF: `data/candles_${currencyPair}_PT1H.csv`,
  }
}

function toJSTDateInput(timestamp: number): string {
  // JST has no DST, so fixed +9h offset is deterministic for date-only conversion.
  return new Date(timestamp + 9*60*60*1000).toISOString().slice(0, 10)
}

function parseOptionalNumber(label: string, value: string, integer = false): number | undefined {
  const trimmed = value.trim()
  if (trimmed === '') {
    return undefined
  }
  const parsed = Number(trimmed)
  if (!Number.isFinite(parsed)) {
    throw new Error(`${label} は数値で入力してください。`)
  }
  if (integer && !Number.isInteger(parsed)) {
    throw new Error(`${label} は整数で入力してください。`)
  }
  return parsed
}

function buildRunRequest(form: BacktestRunForm): BacktestRunRequest {
  const data = form.data.trim()
  if (data === '') {
    throw new Error('Primary CSV(data) は必須です。')
  }

  const request: BacktestRunRequest = { data }
  if (form.dataHtf.trim() !== '') request.dataHtf = form.dataHtf.trim()
  if (form.from.trim() !== '') request.from = form.from.trim()
  if (form.to.trim() !== '') request.to = form.to.trim()

  const initialBalance = parseOptionalNumber('Initial Balance', form.initialBalance)
  const spread = parseOptionalNumber('Spread', form.spread)
  const carryingCost = parseOptionalNumber('Carrying Cost', form.carryingCost)
  const slippage = parseOptionalNumber('Slippage', form.slippage)
  const tradeAmount = parseOptionalNumber('Trade Amount', form.tradeAmount)
  const stopLossPercent = parseOptionalNumber('Stop Loss Percent', form.stopLossPercent)
  const takeProfitPercent = parseOptionalNumber('Take Profit Percent', form.takeProfitPercent)
  const maxPositionAmount = parseOptionalNumber('Max Position Amount', form.maxPositionAmount)
  const maxDailyLoss = parseOptionalNumber('Max Daily Loss', form.maxDailyLoss)
  const maxConsecutiveLosses = parseOptionalNumber(
    'Max Consecutive Losses',
    form.maxConsecutiveLosses,
    true,
  )
  const cooldownMinutes = parseOptionalNumber('Cooldown Minutes', form.cooldownMinutes, true)

  if (initialBalance !== undefined) request.initialBalance = initialBalance
  if (spread !== undefined) request.spread = spread
  if (carryingCost !== undefined) request.carryingCost = carryingCost
  if (slippage !== undefined) request.slippage = slippage
  if (tradeAmount !== undefined) request.tradeAmount = tradeAmount
  if (stopLossPercent !== undefined) request.stopLossPercent = stopLossPercent
  if (takeProfitPercent !== undefined) request.takeProfitPercent = takeProfitPercent
  if (maxPositionAmount !== undefined) request.maxPositionAmount = maxPositionAmount
  if (maxDailyLoss !== undefined) request.maxDailyLoss = maxDailyLoss
  if (maxConsecutiveLosses !== undefined) request.maxConsecutiveLosses = maxConsecutiveLosses
  if (cooldownMinutes !== undefined) request.cooldownMinutes = cooldownMinutes

  return request
}

function getErrorMessage(error: unknown): string {
  if (error instanceof Error) {
    return error.message
  }
  return 'バックテスト実行に失敗しました。'
}

function BacktestPage() {
  const { data: symbols } = useSymbols()
  const pairOptions = useMemo(() => {
    const values = [...fallbackBacktestPairs]
    for (const symbol of symbols ?? []) {
      if (!values.includes(symbol.currencyPair)) {
        values.push(symbol.currencyPair)
      }
    }
    return values
  }, [symbols])

  const [selectedPair, setSelectedPair] = useState('BTC_JPY')
  const autoPaths = useMemo(() => buildAutoCSVPaths(selectedPair), [selectedPair])

  const [selectedId, setSelectedId] = useState('')
  const [runForm, setRunForm] = useState<BacktestRunForm>(defaultRunForm)
  const [runValidationError, setRunValidationError] = useState('')
  const runBacktest = useRunBacktest()
  const { data, isLoading, isError } = useBacktestResults()
  const { data: detail, isLoading: detailLoading } = useBacktestResult(selectedId)
  const {
    data: csvMeta,
    isLoading: isCSVMetaLoading,
    isError: isCSVMetaError,
  } = useBacktestCSVMeta(runForm.data)

  const results = data?.results ?? []

  useEffect(() => {
    if (pairOptions.length === 0) return
    if (pairOptions.includes(selectedPair)) return
    setSelectedPair(pairOptions[0])
  }, [pairOptions, selectedPair])

  useEffect(() => {
    setRunForm((current) => {
      if (
        current.data === autoPaths.primary &&
        current.dataHtf === autoPaths.higherTF &&
        current.from === '' &&
        current.to === ''
      ) {
        return current
      }
      return {
        ...current,
        data: autoPaths.primary,
        dataHtf: autoPaths.higherTF,
        from: '',
        to: '',
      }
    })
    setRunValidationError('')
  }, [autoPaths.higherTF, autoPaths.primary])

  useEffect(() => {
    if (!csvMeta) return
    setRunForm((current) => ({
      ...current,
      from: toJSTDateInput(csvMeta.fromTimestamp),
      to: toJSTDateInput(csvMeta.toTimestamp),
    }))
  }, [csvMeta])

  const setRunField = (key: keyof BacktestRunForm, value: string) => {
    setRunForm((current) => ({ ...current, [key]: value }))
  }

  const handleRun = (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault()
    setRunValidationError('')

    let request: BacktestRunRequest
    try {
      request = buildRunRequest(runForm)
    } catch (error) {
      setRunValidationError(getErrorMessage(error))
      return
    }

    runBacktest.mutate(request, {
      onSuccess: (result) => {
        setSelectedId(result.id)
      },
    })
  }

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

      <section className="mb-4 rounded-3xl border border-white/8 bg-bg-card/90 p-5 shadow-[0_12px_40px_rgba(0,0,0,0.22)]">
        <p className="text-xs uppercase tracking-[0.28em] text-text-secondary">Run</p>
        <h2 className="mt-2 text-xl font-semibold text-white">バックテスト実行</h2>
        <p className="mt-2 text-sm text-text-secondary">
          `data` / `dataHtf` は backend コンテナから参照可能な CSV パスを指定してください。
        </p>
        <label className="mt-3 block">
          <span className="mb-2 block text-sm text-slate-300">通貨ペア</span>
          <select
            value={selectedPair}
            onChange={(event) => setSelectedPair(event.target.value)}
            className="w-full rounded-2xl border border-white/10 bg-white/6 px-4 py-3 text-white outline-none transition focus:border-cyan-200 sm:w-[320px]"
          >
            {pairOptions.map((pair) => (
              <option key={pair} value={pair} className="bg-bg-card text-white">
                {pair.replace('_', '/')}
              </option>
            ))}
          </select>
        </label>
        <p className="mt-2 text-xs text-text-secondary">
          選択中の通貨ペア: {selectedPair.replace('_', '/')}（CSVパスと期間は自動反映）
        </p>
        {isCSVMetaLoading && (
          <p className="mt-1 text-xs text-text-secondary">CSV期間を読み込み中...</p>
        )}
        {isCSVMetaError && (
          <p className="mt-1 text-xs text-accent-red">
            CSV期間の自動取得に失敗しました。CSVパスを確認してください。
          </p>
        )}

        <form onSubmit={handleRun} className="mt-4">
          <div className="grid gap-4 sm:grid-cols-2">
            <RunField
              label="Primary CSV (data)"
              value={runForm.data}
              onChange={(value) => setRunField('data', value)}
              placeholder="data/candles_BTC_JPY_PT15M.csv"
            />
            <RunField
              label="Higher TF CSV (dataHtf)"
              value={runForm.dataHtf}
              onChange={(value) => setRunField('dataHtf', value)}
              placeholder="data/candles_BTC_JPY_PT1H.csv"
            />
            <RunField
              label="From (YYYY-MM-DD)"
              value={runForm.from}
              onChange={(value) => setRunField('from', value)}
              type="date"
            />
            <RunField
              label="To (YYYY-MM-DD)"
              value={runForm.to}
              onChange={(value) => setRunField('to', value)}
              type="date"
            />
            <RunField
              label="Initial Balance"
              value={runForm.initialBalance}
              onChange={(value) => setRunField('initialBalance', value)}
              type="number"
              step="1"
            />
            <RunField
              label="Trade Amount"
              value={runForm.tradeAmount}
              onChange={(value) => setRunField('tradeAmount', value)}
              type="number"
              step="0.0001"
            />
            <RunField
              label="Spread (%)"
              value={runForm.spread}
              onChange={(value) => setRunField('spread', value)}
              type="number"
              step="0.0001"
            />
            <RunField
              label="Carrying Cost (% / day)"
              value={runForm.carryingCost}
              onChange={(value) => setRunField('carryingCost', value)}
              type="number"
              step="0.0001"
            />
            <RunField
              label="Slippage (%)"
              value={runForm.slippage}
              onChange={(value) => setRunField('slippage', value)}
              type="number"
              step="0.0001"
            />
            <RunField
              label="Stop Loss (%)"
              value={runForm.stopLossPercent}
              onChange={(value) => setRunField('stopLossPercent', value)}
              type="number"
              step="0.1"
            />
            <RunField
              label="Take Profit (%)"
              value={runForm.takeProfitPercent}
              onChange={(value) => setRunField('takeProfitPercent', value)}
              type="number"
              step="0.1"
            />
            <RunField
              label="Max Position Amount (optional)"
              value={runForm.maxPositionAmount}
              onChange={(value) => setRunField('maxPositionAmount', value)}
              type="number"
              step="1"
            />
            <RunField
              label="Max Daily Loss (optional)"
              value={runForm.maxDailyLoss}
              onChange={(value) => setRunField('maxDailyLoss', value)}
              type="number"
              step="1"
            />
            <RunField
              label="Max Consecutive Losses (optional)"
              value={runForm.maxConsecutiveLosses}
              onChange={(value) => setRunField('maxConsecutiveLosses', value)}
              type="number"
              step="1"
            />
            <RunField
              label="Cooldown Minutes (optional)"
              value={runForm.cooldownMinutes}
              onChange={(value) => setRunField('cooldownMinutes', value)}
              type="number"
              step="1"
            />
          </div>

          {runValidationError !== '' && (
            <div className="mt-4 rounded-2xl border border-accent-red/40 bg-accent-red/10 px-4 py-3 text-sm text-accent-red">
              {runValidationError}
            </div>
          )}
          {runBacktest.isError && (
            <div className="mt-4 rounded-2xl border border-accent-red/40 bg-accent-red/10 px-4 py-3 text-sm text-accent-red">
              {getErrorMessage(runBacktest.error)}
            </div>
          )}
          {runBacktest.isSuccess && (
            <div className="mt-4 rounded-2xl border border-accent-green/40 bg-accent-green/10 px-4 py-3 text-sm text-accent-green">
              実行完了: {runBacktest.data.id}
            </div>
          )}

          <div className="mt-4 flex justify-end">
            <button
              type="submit"
              disabled={runBacktest.isPending}
              className="rounded-full bg-cyan-200 px-5 py-2 text-sm font-semibold text-slate-950 transition hover:bg-cyan-100 disabled:cursor-not-allowed disabled:opacity-50"
            >
              {runBacktest.isPending ? '実行中...' : 'バックテストを実行'}
            </button>
          </div>
        </form>
      </section>

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

type RunFieldProps = {
  label: string
  value: string
  onChange: (value: string) => void
  placeholder?: string
  type?: 'text' | 'number' | 'date'
  step?: string
}

function RunField({
  label,
  value,
  onChange,
  placeholder,
  type = 'text',
  step,
}: RunFieldProps) {
  return (
    <label className="block">
      <span className="mb-2 block text-sm text-slate-300">{label}</span>
      <input
        type={type}
        value={value}
        onChange={(event) => onChange(event.target.value)}
        placeholder={placeholder}
        step={step}
        className="w-full rounded-2xl border border-white/10 bg-white/6 px-4 py-3 text-white outline-none transition placeholder:text-slate-500 focus:border-cyan-200"
      />
    </label>
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
