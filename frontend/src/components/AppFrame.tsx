import type { ReactNode } from 'react'
import { Link, useSearch } from '@tanstack/react-router'
import { SymbolSelector } from './SymbolSelector'

type AppFrameProps = {
  title: string
  subtitle: string
  children: ReactNode
}

const navItems = [
  { to: '/', label: 'ダッシュボード' },
  { to: '/settings', label: '設定' },
  { to: '/history', label: '履歴' },
  { to: '/backtest', label: 'バックテスト' },
  { to: '/backtest-multi', label: 'マルチ期間' },
] as const

export function AppFrame({ title, subtitle, children }: AppFrameProps) {
  const rootSearch = useSearch({ from: '__root__' }) as { symbol?: string }
  return (
    <main className="mx-auto min-h-screen w-full max-w-7xl px-4 py-6 sm:px-6 lg:px-8">
      <header className="mb-6 overflow-hidden rounded-3xl border border-white/8 bg-[linear-gradient(135deg,rgba(55,66,250,0.24),rgba(8,12,32,0.92)_45%,rgba(0,212,170,0.18))] p-5 sm:p-6 shadow-[0_20px_80px_rgba(0,0,0,0.35)]">
        <div className="flex flex-col gap-5 lg:flex-row lg:items-end lg:justify-between">
          <div>
            <p className="text-[0.7rem] uppercase tracking-[0.35em] text-cyan-200/70">Rakuten CFD Bot</p>
            <h1 className="mt-2 text-2xl font-semibold tracking-tight text-white sm:text-3xl lg:text-4xl">{title}</h1>
            <p className="mt-2 max-w-2xl text-sm text-slate-300">{subtitle}</p>
          </div>
          <div className="flex flex-col gap-3 lg:items-end">
            <SymbolSelector />
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
