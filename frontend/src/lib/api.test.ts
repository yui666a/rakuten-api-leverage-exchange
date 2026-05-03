import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import {
  fetchApi,
  sendApi,
  buildRealtimeWebSocketUrl,
  closePosition,
} from './api'

beforeEach(() => {
  vi.stubGlobal('fetch', vi.fn())
})

afterEach(() => {
  vi.restoreAllMocks()
})

describe('fetchApi', () => {
  it('returns parsed JSON on success', async () => {
    const mockData = { balance: 1000, dailyLoss: 0 }
    vi.mocked(fetch).mockResolvedValue({
      ok: true,
      json: () => Promise.resolve(mockData),
    } as Response)

    const result = await fetchApi('/status')

    expect(fetch).toHaveBeenCalledWith(
      expect.stringContaining('/api/v1/status'),
    )
    expect(result).toEqual(mockData)
  })

  it('throws on non-ok response', async () => {
    vi.mocked(fetch).mockResolvedValue({
      ok: false,
      status: 500,
      statusText: 'Internal Server Error',
    } as Response)

    await expect(fetchApi('/status')).rejects.toThrow(
      'API error: 500 Internal Server Error',
    )
  })

  it('calls the correct URL with API_BASE prefix', async () => {
    vi.mocked(fetch).mockResolvedValue({
      ok: true,
      json: () => Promise.resolve([]),
    } as Response)

    await fetchApi('/positions?symbolId=1')

    expect(fetch).toHaveBeenCalledWith(
      expect.stringContaining('/api/v1/positions?symbolId=1'),
    )
  })
})

describe('sendApi', () => {
  it('sends POST request without body', async () => {
    const mockResponse = { status: 'running' }
    vi.mocked(fetch).mockResolvedValue({
      ok: true,
      json: () => Promise.resolve(mockResponse),
    } as Response)

    const result = await sendApi('/start', 'POST')

    expect(fetch).toHaveBeenCalledWith(
      expect.stringContaining('/api/v1/start'),
      expect.objectContaining({ method: 'POST' }),
    )
    expect(result).toEqual(mockResponse)
  })

  it('sends PUT request with JSON body', async () => {
    const body = { maxDailyLoss: 5000 }
    const mockResponse = { ...body }
    vi.mocked(fetch).mockResolvedValue({
      ok: true,
      json: () => Promise.resolve(mockResponse),
    } as Response)

    const result = await sendApi('/config', 'PUT', body)

    expect(fetch).toHaveBeenCalledWith(
      expect.stringContaining('/api/v1/config'),
      expect.objectContaining({
        method: 'PUT',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(body),
      }),
    )
    expect(result).toEqual(mockResponse)
  })

  it('does not set headers or body when body is undefined', async () => {
    vi.mocked(fetch).mockResolvedValue({
      ok: true,
      json: () => Promise.resolve({}),
    } as Response)

    await sendApi('/stop', 'POST')

    expect(fetch).toHaveBeenCalledWith(
      expect.stringContaining('/api/v1/stop'),
      expect.objectContaining({
        method: 'POST',
        headers: undefined,
        body: undefined,
      }),
    )
  })

  it('throws on non-ok response', async () => {
    vi.mocked(fetch).mockResolvedValue({
      ok: false,
      status: 400,
      statusText: 'Bad Request',
    } as Response)

    await expect(sendApi('/config', 'PUT', {})).rejects.toThrow(
      'API error: 400 Bad Request',
    )
  })
})

describe('closePosition', () => {
  it('POSTs to /positions/:id/close with the provided body and returns parsed JSON', async () => {
    const mockResponse = {
      clientOrderId: 'manual-close-42-xyz',
      executed: true,
      orderId: 99,
    }
    vi.mocked(fetch).mockResolvedValue({
      ok: true,
      json: () => Promise.resolve(mockResponse),
    } as Response)

    const result = await closePosition(42, {
      symbolId: 7,
      clientOrderId: 'manual-close-42-xyz',
    })

    expect(fetch).toHaveBeenCalledWith(
      expect.stringContaining('/api/v1/positions/42/close'),
      expect.objectContaining({
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          symbolId: 7,
          clientOrderId: 'manual-close-42-xyz',
        }),
      }),
    )
    expect(result).toEqual(mockResponse)
  })

  it('throws on non-ok response', async () => {
    vi.mocked(fetch).mockResolvedValue({
      ok: false,
      status: 404,
      statusText: 'Not Found',
    } as Response)

    await expect(
      closePosition(999, { symbolId: 7, clientOrderId: 'x' }),
    ).rejects.toThrow('API error: 404 Not Found')
  })
})

describe('buildRealtimeWebSocketUrl', () => {
  it('returns URL with symbolId query parameter', () => {
    const url = buildRealtimeWebSocketUrl(1)
    expect(url).toContain('symbolId=1')
  })

  it('uses ws protocol for http pages', () => {
    // jsdom defaults window.location.protocol to 'http:'
    const url = buildRealtimeWebSocketUrl(5)
    expect(url).toMatch(/^ws:\/\//)
    expect(url).toContain('symbolId=5')
  })
})
