import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { act, renderHook } from '@testing-library/react'
import { useNotificationSettings } from '../useNotificationSettings'

// We stub Notification on the window before each test because vitest's jsdom
// environment does not ship with it. Each test customises permission +
// requestPermission() return values to drive the hook through its branches.

type MockNotification = {
  permission: 'default' | 'granted' | 'denied'
  requestPermission: ReturnType<typeof vi.fn>
}

function installNotification(perm: MockNotification['permission'], next: MockNotification['permission']) {
  const requestPermission = vi.fn().mockResolvedValue(next)
  Object.defineProperty(window, 'Notification', {
    configurable: true,
    writable: true,
    value: { permission: perm, requestPermission } as MockNotification,
  })
  return requestPermission
}

beforeEach(() => {
  window.localStorage.clear()
})

afterEach(() => {
  // Wipe so the next test sets it up cleanly.
  delete (window as unknown as { Notification?: unknown }).Notification
})

describe('useNotificationSettings', () => {
  it('starts disabled and persists toggles to localStorage', () => {
    installNotification('granted', 'granted')
    const { result } = renderHook(() => useNotificationSettings())
    expect(result.current.settings.enabled).toBe(false)
    expect(result.current.settings.soundEnabled).toBe(true)

    act(() => {
      void result.current.setEnabled(true)
    })
    expect(JSON.parse(window.localStorage.getItem('notif-settings:v1') ?? '{}')).toMatchObject({
      enabled: true,
    })
  })

  it('requests browser permission on first enable when state is default', async () => {
    const req = installNotification('default', 'granted')
    const { result } = renderHook(() => useNotificationSettings())
    await act(async () => {
      await result.current.setEnabled(true)
    })
    expect(req).toHaveBeenCalledOnce()
    expect(result.current.permission).toBe('granted')
    expect(result.current.shouldFire).toBe(true)
  })

  it('shouldFire stays false when permission is denied', async () => {
    installNotification('denied', 'denied')
    const { result } = renderHook(() => useNotificationSettings())
    await act(async () => {
      await result.current.setEnabled(true)
    })
    expect(result.current.permission).toBe('denied')
    expect(result.current.shouldFire).toBe(false)
  })

  it('reports unsupported when Notification API is missing', () => {
    // No installNotification() — Notification stays undefined.
    const { result } = renderHook(() => useNotificationSettings())
    expect(result.current.permission).toBe('unsupported')
    expect(result.current.shouldFire).toBe(false)
  })

  it('soundEnabled toggles independently of enabled', () => {
    installNotification('granted', 'granted')
    const { result } = renderHook(() => useNotificationSettings())
    act(() => {
      result.current.setSoundEnabled(false)
    })
    expect(result.current.settings.soundEnabled).toBe(false)
    expect(result.current.settings.enabled).toBe(false)
  })
})
