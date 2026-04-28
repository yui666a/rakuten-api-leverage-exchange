import { useEffect, useState } from 'react'
import { createFileRoute } from '@tanstack/react-router'
import { AppFrame } from '../components/AppFrame'
import { BotControlCard } from '../components/BotControlCard'
import { useConfig, useUpdateConfig } from '../hooks/useConfig'
import { useStatus } from '../hooks/useStatus'
import { useStartBot, useStopBot } from '../hooks/useBotControl'
import { useMarketTickerStream } from '../hooks/useMarketTickerStream'
import { useSymbolContext } from '../contexts/SymbolContext'
import type { RiskConfig } from '../lib/api'

export const Route = createFileRoute('/operations')({ component: OperationsPage })

function OperationsPage() {
  const { symbolId } = useSymbolContext()
  const { data: config } = useConfig()
  const { data: status } = useStatus()
  const updateConfig = useUpdateConfig()
  const startBot = useStartBot()
  const stopBot = useStopBot()
  useMarketTickerStream(symbolId)
  const [form, setForm] = useState<RiskConfig>({
    maxPositionAmount: 0,
    maxDailyLoss: 0,
    stopLossPercent: 0,
    takeProfitPercent: 0,
    initialCapital: 0,
    maxConsecutiveLosses: 0,
    cooldownMinutes: 0,
  })

  useEffect(() => {
    if (config) {
      setForm(config)
    }
  }, [config])

  const handleNumberChange = (key: keyof RiskConfig, value: string) => {
    setForm((current) => ({
      ...current,
      [key]: Number(value),
    }))
  }

  return (
    <AppFrame
      title="運用設定"
      subtitle="ボット制御・リスク設定・通知設定を 1 か所に集約した運用画面です。"
    >
      <div className="grid gap-4 xl:grid-cols-[minmax(0,1.2fr)_minmax(320px,0.8fr)]">
        <section className="rounded-3xl border border-white/8 bg-bg-card/90 p-5 shadow-[0_12px_40px_rgba(0,0,0,0.22)]">
          <p className="text-xs uppercase tracking-[0.28em] text-text-secondary">Risk Config</p>
          <h2 className="mt-2 text-xl font-semibold text-white">リスク設定</h2>

          <div className="mt-5 grid gap-4 sm:grid-cols-2">
            <Field
              label="最大ポジション額"
              value={form.maxPositionAmount}
              onChange={(value) => handleNumberChange('maxPositionAmount', value)}
            />
            <Field
              label="日次損失上限"
              value={form.maxDailyLoss}
              onChange={(value) => handleNumberChange('maxDailyLoss', value)}
            />
            <Field
              label="損切り率 (%)"
              value={form.stopLossPercent}
              onChange={(value) => handleNumberChange('stopLossPercent', value)}
            />
            <Field
              label="利確率 (%)"
              value={form.takeProfitPercent}
              onChange={(value) => handleNumberChange('takeProfitPercent', value)}
            />
            <Field
              label="初期資金"
              value={form.initialCapital}
              onChange={(value) => handleNumberChange('initialCapital', value)}
            />
            <Field
              label="連敗上限 (0=無効)"
              value={form.maxConsecutiveLosses}
              onChange={(value) => handleNumberChange('maxConsecutiveLosses', value)}
            />
            <Field
              label="冷却期間 (分)"
              value={form.cooldownMinutes}
              onChange={(value) => handleNumberChange('cooldownMinutes', value)}
            />
          </div>

          <div className="mt-5 flex items-center justify-between gap-3">
            <p className="text-sm text-slate-300">
              {updateConfig.isSuccess ? '保存済み' : '変更後に Save で反映'}
            </p>
            <button
              type="button"
              onClick={() => updateConfig.mutate(form)}
              disabled={updateConfig.isPending}
              className="rounded-full bg-cyan-200 px-5 py-2 text-sm font-semibold text-slate-950 transition hover:bg-cyan-100 disabled:cursor-not-allowed disabled:opacity-50"
            >
              Save
            </button>
          </div>
        </section>

        <BotControlCard
          status={status}
          onStart={() => startBot.mutate()}
          onStop={() => stopBot.mutate()}
          isPending={startBot.isPending || stopBot.isPending}
        />
      </div>
    </AppFrame>
  )
}

type FieldProps = {
  label: string
  value: number
  onChange: (value: string) => void
}

function Field({ label, value, onChange }: FieldProps) {
  return (
    <label className="block">
      <span className="mb-2 block text-sm text-slate-300">{label}</span>
      <input
        type="number"
        value={value}
        onChange={(event) => onChange(event.target.value)}
        className="w-full rounded-2xl border border-white/10 bg-white/6 px-4 py-3 text-white outline-none transition placeholder:text-slate-500 focus:border-cyan-200"
      />
    </label>
  )
}
