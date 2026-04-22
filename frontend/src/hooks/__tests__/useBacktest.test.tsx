import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { renderHook, waitFor } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import type { ReactNode } from 'react'
import { useBacktestResults, useProfile, useProfiles } from '../useBacktest'

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

describe('useProfiles', () => {
  it('fetches GET /profiles and returns the list envelope', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn().mockResolvedValue({
        ok: true,
        json: () =>
          Promise.resolve({
            profiles: [
              { name: 'production', description: 'v5', isRouter: false },
              { name: 'router_x', description: 'router', isRouter: true },
            ],
          }),
      } as Response),
    )

    const { result } = renderHook(() => useProfiles(), {
      wrapper: createWrapper(),
    })

    await waitFor(() => expect(result.current.isSuccess).toBe(true))

    const url = lastFetchURL()
    expect(url).toContain('/profiles')
    expect(result.current.data?.profiles).toHaveLength(2)
    expect(result.current.data?.profiles[1].isRouter).toBe(true)
  })
})

describe('useProfile', () => {
  it('is disabled for an empty name (no fetch)', async () => {
    // Default mock returns results:[]; the hook's `enabled` guard must
    // keep fetch from being called at all.
    renderHook(() => useProfile(''), { wrapper: createWrapper() })

    // Give React Query a tick to decide whether to fetch. If the guard
    // is broken this call count would be 1+.
    await new Promise((r) => setTimeout(r, 10))
    expect(vi.mocked(fetch).mock.calls.length).toBe(0)
  })

  it('fetches GET /profiles/:name when name is non-empty', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn().mockResolvedValue({
        ok: true,
        json: () =>
          Promise.resolve({
            name: 'production',
            description: 'v5',
            indicators: {},
            stance_rules: {},
            signal_rules: {},
            strategy_risk: {},
            htf_filter: {},
          }),
      } as Response),
    )

    const { result } = renderHook(() => useProfile('production'), {
      wrapper: createWrapper(),
    })

    await waitFor(() => expect(result.current.isSuccess).toBe(true))

    expect(lastFetchURL()).toContain('/profiles/production')
    expect(result.current.data?.name).toBe('production')
  })

  it('URL-encodes the profile name path segment', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn().mockResolvedValue({
        ok: true,
        json: () => Promise.resolve({}),
      } as Response),
    )

    // encodeURIComponent turns `&` into `%26` — prove the hook does that.
    const { result } = renderHook(() => useProfile('pr&dev'), {
      wrapper: createWrapper(),
    })

    await waitFor(() => expect(result.current.isSuccess).toBe(true))

    expect(lastFetchURL()).toContain('/profiles/pr%26dev')
  })
})
