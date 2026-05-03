import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { render, screen, waitFor } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { createMemoryHistory, createRouter, createRootRoute, RouterProvider } from '@tanstack/react-router'
import type { ReactNode } from 'react'
import { RecentDecisionsCard } from '../RecentDecisionsCard'
import type { DecisionLogItem, DecisionLogResponse } from '../../lib/api'

function makeItem(i: number): DecisionLogItem {
  return {
    id: i,
    barCloseAt: 1_700_000_000_000 + i * 60_000,
    sequenceInBar: 0,
    triggerKind: 'BAR_CLOSE',
    symbolId: 3,
    currencyPair: 'LTC_JPY',
    primaryInterval: 'PT15M',
    stance: 'TREND_FOLLOW',
    lastPrice: 10000 + i,
    signal: { action: 'HOLD', confidence: 0, reason: '' },
    risk: { outcome: 'SKIPPED', reason: '' },
    bookGate: { outcome: 'SKIPPED', reason: '' },
    order: { outcome: 'NOOP', orderId: 0, amount: 0, price: 0, error: '' },
    closedPositionId: 0,
    openedPositionId: 0,
    indicators: {},
    higherTfIndicators: {},
    createdAt: 1_700_000_000_000 + i * 60_000,
  }
}

function renderWithProviders(ui: ReactNode) {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })
  const rootRoute = createRootRoute({ component: () => <>{ui}</> })
  const router = createRouter({
    routeTree: rootRoute,
    history: createMemoryHistory({ initialEntries: ['/'] }),
  })
  return render(
    <QueryClientProvider client={queryClient}>
      <RouterProvider router={router} />
    </QueryClientProvider>,
  )
}

beforeEach(() => {
  vi.stubGlobal('fetch', vi.fn())
  // useVirtualizer は jsdom でサイズ 0 になり virtualItems が空になるため、ダミーサイズを与える
  Object.defineProperty(HTMLElement.prototype, 'offsetHeight', {
    configurable: true,
    value: 432,
  })
  Object.defineProperty(HTMLElement.prototype, 'offsetWidth', {
    configurable: true,
    value: 800,
  })
})

afterEach(() => {
  vi.restoreAllMocks()
})

describe('RecentDecisionsCard', () => {
  it('200 件取得しても thead が描画され、行は仮想化される', async () => {
    const decisions = Array.from({ length: 200 }, (_, i) => makeItem(i))
    const response: DecisionLogResponse = { decisions, nextCursor: 0, hasMore: false }
    vi.mocked(fetch).mockResolvedValue({
      ok: true,
      json: () => Promise.resolve(response),
    } as Response)

    renderWithProviders(
      <RecentDecisionsCard symbolId={3} strategy={undefined} rootSearch={{}} />,
    )

    await waitFor(() => {
      expect(screen.getByText('時刻')).toBeInTheDocument()
    })

    // 仮想化されているなら、200 件すべての行は描画されない。
    // grid 構造に置き換わったため translateY を持つ要素を行とみなす。
    const rows = document.querySelectorAll('[style*="translateY"]')
    expect(rows.length).toBeLessThan(200)
    expect(rows.length).toBeGreaterThan(0)
  })

  it('limit=200 で /decisions を叩く', async () => {
    vi.mocked(fetch).mockResolvedValue({
      ok: true,
      json: () => Promise.resolve({ decisions: [], nextCursor: 0, hasMore: false }),
    } as Response)

    renderWithProviders(
      <RecentDecisionsCard symbolId={3} strategy={undefined} rootSearch={{}} />,
    )

    await waitFor(() => {
      expect(vi.mocked(fetch)).toHaveBeenCalled()
    })
    const url = vi.mocked(fetch).mock.calls[0][0] as string
    expect(url).toContain('limit=200')
  })
})
