import type { ReactNode } from 'react'
import { Link } from '@tanstack/react-router'

type AppFrameProps = {
  title: string
  subtitle: string
  children: ReactNode
}

const navItems = [
  { to: '/', label: 'Dashboard' },
  { to: '/settings', label: 'Settings' },
  { to: '/history', label: 'History' },
] as const

export function AppFrame({ title, subtitle, children }: AppFrameProps) {
  return (
    <main className="mx-auto min-h-screen w-full max-w-7xl px-4 py-6 sm:px-6 lg:px-8">
      <header className="mb-6 overflow-hidden rounded-3xl border border-white/8 bg-[linear-gradient(135deg,rgba(55,66,250,0.24),rgba(8,12,32,0.92)_45%,rgba(0,212,170,0.18))] p-6 shadow-[0_20px_80px_rgba(0,0,0,0.35)]">
        <div className="flex flex-col gap-5 lg:flex-row lg:items-end lg:justify-between">
          <div>
            <p className="text-[0.7rem] uppercase tracking-[0.35em] text-cyan-200/70">Rakuten CFD Bot</p>
            <h1 className="mt-2 text-3xl font-semibold tracking-tight text-white sm:text-4xl">{title}</h1>
            <p className="mt-2 max-w-2xl text-sm text-slate-300">{subtitle}</p>
          </div>
          <nav className="flex flex-wrap gap-2">
            {navItems.map((item) => (
              <Link
                key={item.to}
                to={item.to}
                activeProps={{ className: 'bg-white text-slate-950 shadow-lg' }}
                inactiveProps={{ className: 'bg-white/8 text-slate-200 hover:bg-white/14' }}
                className="rounded-full px-4 py-2 text-sm font-medium transition"
              >
                {item.label}
              </Link>
            ))}
          </nav>
        </div>
      </header>
      {children}
    </main>
  )
}
