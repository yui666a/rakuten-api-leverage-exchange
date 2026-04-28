import type { ReactNode } from 'react'
import { Link, useSearch } from '@tanstack/react-router'
import { SymbolSelector } from './SymbolSelector'
import { NotificationToggle } from './NotificationToggle'

type AppFrameProps = {
  title: string
  subtitle: string
  children: ReactNode
}

// ナビは「目的別 4 セクション」に集約する:
//   監視 (Live)     ... ライブの状況把握
//   分析 (Analysis) ... 過去データに対するバックテスト・WFO・マルチ期間
//   運用 (Operations) ... ボット制御・リスク設定・通知設定
//   履歴 (Journal)  ... 取引と判断の証跡
//
// 「分析」リンクは現状 /backtest を指している。バックテスト系 3 画面の
// 統合は後続 PR で /analysis ルートに移し替え予定。
const navItems = [
  { to: '/', label: '監視' },
  { to: '/backtest', label: '分析' },
  { to: '/operations', label: '運用' },
  { to: '/history', label: '履歴' },
] as const

export function AppFrame({ title, subtitle, children }: AppFrameProps) {
  const rootSearch = useSearch({ from: '__root__' }) as { symbol?: string }
  return (
    <main className="mx-auto min-h-screen w-full max-w-[1440px] px-4 py-6 sm:px-6 lg:px-8">
      <header className="mb-6 overflow-hidden rounded-3xl border border-white/8 bg-[linear-gradient(135deg,rgba(55,66,250,0.24),rgba(8,12,32,0.92)_45%,rgba(0,212,170,0.18))] p-5 sm:p-6 shadow-[0_20px_80px_rgba(0,0,0,0.35)]">
        <div className="flex flex-col gap-5 lg:flex-row lg:items-end lg:justify-between">
          <div>
            <p className="text-[0.7rem] uppercase tracking-[0.35em] text-cyan-200/70">Rakuten CFD Bot</p>
            <h1 className="mt-2 text-2xl font-semibold tracking-tight text-white sm:text-3xl lg:text-4xl">{title}</h1>
            <p className="mt-2 max-w-2xl text-sm text-slate-300">{subtitle}</p>
          </div>
          <div className="flex flex-col gap-3 lg:items-end">
            <div className="flex items-center gap-2">
              <SymbolSelector />
              <NotificationToggle />
            </div>
            <nav className="-mx-1 flex gap-2 overflow-x-auto px-1 pb-1 lg:overflow-visible">
              {navItems.map((item) => (
                <Link
                  key={item.to}
                  to={item.to}
                  search={rootSearch}
                  activeProps={{
                    style: {
                      backgroundColor: '#00d4aa',
                      color: '#0f172a',
                      boxShadow: '0 8px 24px rgba(0,212,170,0.35)',
                      borderColor: 'rgba(0,212,170,0.6)',
                    },
                  }}
                  inactiveProps={{
                    style: {
                      backgroundColor: 'rgba(255,255,255,0.08)',
                      color: '#e2e8f0',
                      borderColor: 'rgba(255,255,255,0.12)',
                    },
                  }}
                  className="whitespace-nowrap rounded-full border px-4 py-2 text-sm font-semibold transition hover:brightness-110"
                >
                  {item.label}
                </Link>
              ))}
            </nav>
          </div>
        </div>
      </header>
      {children}
    </main>
  )
}
