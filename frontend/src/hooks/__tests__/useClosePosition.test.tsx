import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { act, renderHook, waitFor } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import type { ReactNode } from 'react'
import { useClosePosition } from '../useClosePosition'

function createWrapper() {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })
  return {
    queryClient,
    Wrapper: ({ children }: { children: ReactNode }) => (
      <QueryClientProvider client={queryClient}>{children}</QueryClientProvider>
    ),
  }
}

beforeEach(() => {
  vi.stubGlobal('fetch', vi.fn())
})

afterEach(() => {
  vi.restoreAllMocks()
})

describe('useClosePosition', () => {
  it('POSTs to /positions/:id/close with a generated clientOrderId and returns the response', async () => {
    const mockResponse = {
      clientOrderId: 'manual-close-42-abc',
      executed: true,
      orderId: 123,
    }
    vi.mocked(fetch).mockResolvedValue({
      ok: true,
      json: () => Promise.resolve(mockResponse),
    } as Response)

    const { Wrapper } = createWrapper()
    const { result } = renderHook(() => useClosePosition(), { wrapper: Wrapper })

    await act(async () => {
      result.current.mutate({ positionId: 42, symbolId: 7 })
    })

    await waitFor(() => expect(result.current.isSuccess).toBe(true))
    expect(result.current.data).toEqual(mockResponse)

    const call = vi.mocked(fetch).mock.calls[0]
    const url = call[0] as string
    const init = call[1] as RequestInit
    expect(url).toContain('/api/v1/positions/42/close')
    expect(init.method).toBe('POST')
    const body = JSON.parse(init.body as string) as {
      symbolId: number
      clientOrderId: string
    }
    expect(body.symbolId).toBe(7)
    expect(body.clientOrderId).toMatch(/^manual-close-42-/)
  })

  it('invalidates positions / trades / pnl queries on success', async () => {
    vi.mocked(fetch).mockResolvedValue({
      ok: true,
      json: () => Promise.resolve({ clientOrderId: 'x', executed: true }),
    } as Response)

    const { queryClient, Wrapper } = createWrapper()
    const spy = vi.spyOn(queryClient, 'invalidateQueries')

    const { result } = renderHook(() => useClosePosition(), { wrapper: Wrapper })

    await act(async () => {
      result.current.mutate({ positionId: 1, symbolId: 7 })
    })

    await waitFor(() => expect(result.current.isSuccess).toBe(true))

    const invalidatedKeys = spy.mock.calls.map((c) => c[0]?.queryKey?.[0])
    expect(invalidatedKeys).toEqual(
      expect.arrayContaining(['positions', 'trades', 'pnl']),
    )
  })

  it('surfaces API errors', async () => {
    vi.mocked(fetch).mockResolvedValue({
      ok: false,
      status: 404,
      statusText: 'Not Found',
    } as Response)

    const { Wrapper } = createWrapper()
    const { result } = renderHook(() => useClosePosition(), { wrapper: Wrapper })

    await act(async () => {
      result.current.mutate({ positionId: 999, symbolId: 7 })
    })

    await waitFor(() => expect(result.current.isError).toBe(true))
    expect(result.current.error?.message).toContain('404')
  })
})
