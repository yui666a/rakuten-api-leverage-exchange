import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { renderHook, waitFor } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import type { ReactNode } from 'react'
import { useStartBot, useStopBot } from '../useBotControl'

function createWrapper() {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })
  return function Wrapper({ children }: { children: ReactNode }) {
    return (
      <QueryClientProvider client={queryClient}>{children}</QueryClientProvider>
    )
  }
}

beforeEach(() => {
  vi.stubGlobal('fetch', vi.fn())
})

afterEach(() => {
  vi.restoreAllMocks()
})

describe('useStartBot', () => {
  it('sends POST to /start', async () => {
    vi.mocked(fetch).mockResolvedValue({
      ok: true,
      json: () =>
        Promise.resolve({
          status: 'running',
          tradingHalted: false,
          manuallyStopped: false,
        }),
    } as Response)

    const { result } = renderHook(() => useStartBot(), {
      wrapper: createWrapper(),
    })

    result.current.mutate()

    await waitFor(() => expect(result.current.isSuccess).toBe(true))

    expect(fetch).toHaveBeenCalledWith(
      expect.stringContaining('/api/v1/start'),
      expect.objectContaining({ method: 'POST' }),
    )
  })

  it('returns error state on failure', async () => {
    vi.mocked(fetch).mockResolvedValue({
      ok: false,
      status: 500,
      statusText: 'Internal Server Error',
    } as Response)

    const { result } = renderHook(() => useStartBot(), {
      wrapper: createWrapper(),
    })

    result.current.mutate()

    await waitFor(() => expect(result.current.isError).toBe(true))

    expect(result.current.error?.message).toContain('500')
  })
})

describe('useStopBot', () => {
  it('sends POST to /stop', async () => {
    vi.mocked(fetch).mockResolvedValue({
      ok: true,
      json: () =>
        Promise.resolve({
          status: 'stopped',
          tradingHalted: false,
          manuallyStopped: true,
        }),
    } as Response)

    const { result } = renderHook(() => useStopBot(), {
      wrapper: createWrapper(),
    })

    result.current.mutate()

    await waitFor(() => expect(result.current.isSuccess).toBe(true))

    expect(fetch).toHaveBeenCalledWith(
      expect.stringContaining('/api/v1/stop'),
      expect.objectContaining({ method: 'POST' }),
    )
  })

  it('returns error state on failure', async () => {
    vi.mocked(fetch).mockResolvedValue({
      ok: false,
      status: 503,
      statusText: 'Service Unavailable',
    } as Response)

    const { result } = renderHook(() => useStopBot(), {
      wrapper: createWrapper(),
    })

    result.current.mutate()

    await waitFor(() => expect(result.current.isError).toBe(true))

    expect(result.current.error?.message).toContain('503')
  })
})
