import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { renderHook, waitFor } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import type { ReactNode } from 'react'
import { useBacktestResults } from '../useBacktest'

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

function lastFetchURL(): string {
  const mock = vi.mocked(fetch)
  const last = mock.mock.calls[mock.mock.calls.length - 1]?.[0]
  if (typeof last !== 'string') {
    throw new Error('fetch was not called with a string URL')
  }
  return last
}

beforeEach(() => {
  vi.stubGlobal(
    'fetch',
    vi.fn().mockResolvedValue({
      ok: true,
      json: () => Promise.resolve({ results: [] }),
    } as Response),
  )
})

afterEach(() => {
  vi.restoreAllMocks()
})

describe('useBacktestResults', () => {
  it('fetches with default limit/offset and no filter params', async () => {
    const { result } = renderHook(() => useBacktestResults(), {
      wrapper: createWrapper(),
    })

    await waitFor(() => expect(result.current.isSuccess).toBe(true))

    const url = lastFetchURL()
    expect(url).toContain('/backtest/results?')
    expect(url).toContain('limit=20')
    expect(url).toContain('offset=0')
    expect(url).not.toContain('profileName')
    expect(url).not.toContain('pdcaCycleId')
    expect(url).not.toContain('hasParent')
    expect(url).not.toContain('parentResultId')
  })

  it('serialises profileName into the query string', async () => {
    const { result } = renderHook(
      () => useBacktestResults({ profileName: 'production' }),
      { wrapper: createWrapper() },
    )

    await waitFor(() => expect(result.current.isSuccess).toBe(true))

    expect(lastFetchURL()).toContain('profileName=production')
  })

  it('serialises hasParent=false as a literal `false` string', async () => {
    const { result } = renderHook(
      () => useBacktestResults({ hasParent: false }),
      { wrapper: createWrapper() },
    )

    await waitFor(() => expect(result.current.isSuccess).toBe(true))

    expect(lastFetchURL()).toContain('hasParent=false')
  })

  it('serialises hasParent=true as a literal `true` string', async () => {
    const { result } = renderHook(
      () => useBacktestResults({ hasParent: true }),
      { wrapper: createWrapper() },
    )

    await waitFor(() => expect(result.current.isSuccess).toBe(true))

    expect(lastFetchURL()).toContain('hasParent=true')
  })

  it('skips undefined and empty-string filter values', async () => {
    const { result } = renderHook(
      () =>
        useBacktestResults({
          profileName: '',
          pdcaCycleId: undefined,
          parentResultId: '',
        }),
      { wrapper: createWrapper() },
    )

    await waitFor(() => expect(result.current.isSuccess).toBe(true))

    const url = lastFetchURL()
    expect(url).not.toContain('profileName=')
    expect(url).not.toContain('pdcaCycleId=')
    expect(url).not.toContain('parentResultId=')
  })

  it('URL-encodes filter values via URLSearchParams', async () => {
    // Use a value that requires escaping to prove we're not concatenating.
    const { result } = renderHook(
      () => useBacktestResults({ profileName: 'prod&dev' }),
      { wrapper: createWrapper() },
    )

    await waitFor(() => expect(result.current.isSuccess).toBe(true))

    const url = lastFetchURL()
    // URLSearchParams encodes `&` inside a value as `%26`.
    expect(url).toContain('profileName=prod%26dev')
  })
})
