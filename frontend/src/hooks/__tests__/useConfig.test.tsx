import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { renderHook, waitFor } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import type { ReactNode } from 'react'
import { useConfig, useUpdateConfig } from '../useConfig'
import type { RiskConfig } from '../../lib/api'

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

const mockConfig: RiskConfig = {
  maxPositionAmount: 10,
  maxDailyLoss: 5000,
  stopLossPercent: 2,
  takeProfitPercent: 3,
  initialCapital: 100000,
  maxConsecutiveLosses: 3,
  cooldownMinutes: 30,
}

beforeEach(() => {
  vi.stubGlobal('fetch', vi.fn())
})

afterEach(() => {
  vi.restoreAllMocks()
})

describe('useConfig', () => {
  it('fetches config from /config endpoint', async () => {
    vi.mocked(fetch).mockResolvedValue({
      ok: true,
      json: () => Promise.resolve(mockConfig),
    } as Response)

    const { result } = renderHook(() => useConfig(), {
      wrapper: createWrapper(),
    })

    await waitFor(() => expect(result.current.isSuccess).toBe(true))

    expect(result.current.data).toEqual(mockConfig)
    expect(fetch).toHaveBeenCalledWith(
      expect.stringContaining('/api/v1/config'),
    )
  })

  it('returns error state on fetch failure', async () => {
    vi.mocked(fetch).mockResolvedValue({
      ok: false,
      status: 500,
      statusText: 'Internal Server Error',
    } as Response)

    const { result } = renderHook(() => useConfig(), {
      wrapper: createWrapper(),
    })

    await waitFor(() => expect(result.current.isError).toBe(true))

    expect(result.current.error).toBeDefined()
  })
})

describe('useUpdateConfig', () => {
  it('sends PUT to /config with the provided config', async () => {
    vi.mocked(fetch).mockResolvedValue({
      ok: true,
      json: () => Promise.resolve(mockConfig),
    } as Response)

    const { result } = renderHook(() => useUpdateConfig(), {
      wrapper: createWrapper(),
    })

    result.current.mutate(mockConfig)

    await waitFor(() => expect(result.current.isSuccess).toBe(true))

    expect(fetch).toHaveBeenCalledWith(
      expect.stringContaining('/api/v1/config'),
      expect.objectContaining({
        method: 'PUT',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(mockConfig),
      }),
    )
  })

  it('enters error state when mutation fails', async () => {
    vi.mocked(fetch).mockResolvedValue({
      ok: false,
      status: 422,
      statusText: 'Unprocessable Entity',
    } as Response)

    const { result } = renderHook(() => useUpdateConfig(), {
      wrapper: createWrapper(),
    })

    result.current.mutate(mockConfig)

    await waitFor(() => expect(result.current.isError).toBe(true))

    expect(result.current.error?.message).toContain('422')
  })
})
