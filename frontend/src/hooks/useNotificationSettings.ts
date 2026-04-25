import { useCallback, useEffect, useState } from 'react'
import { ensureServiceWorker } from '../lib/sw-register'

// Notification settings live in localStorage so the user's preference survives
// page reloads and isn't tied to React state alone. The hook is intentionally
// thin — it owns the storage shape but lets consumers (PR-4 useTradeNotifications,
// settings UI) decide what to do with the values.

const STORAGE_KEY = 'notif-settings:v1'

export type BrowserPermission = 'default' | 'granted' | 'denied' | 'unsupported'

export type NotificationSettings = {
  enabled: boolean
  soundEnabled: boolean
}

const DEFAULT_SETTINGS: NotificationSettings = {
  enabled: false, // off until the user explicitly opts in (per browser UX guidelines)
  soundEnabled: true,
}

function readStorage(): NotificationSettings {
  if (typeof window === 'undefined') return DEFAULT_SETTINGS
  try {
    const raw = window.localStorage.getItem(STORAGE_KEY)
    if (!raw) return DEFAULT_SETTINGS
    const parsed = JSON.parse(raw) as Partial<NotificationSettings>
    return {
      enabled: parsed.enabled ?? DEFAULT_SETTINGS.enabled,
      soundEnabled: parsed.soundEnabled ?? DEFAULT_SETTINGS.soundEnabled,
    }
  } catch {
    return DEFAULT_SETTINGS
  }
}

function writeStorage(s: NotificationSettings) {
  if (typeof window === 'undefined') return
  try {
    window.localStorage.setItem(STORAGE_KEY, JSON.stringify(s))
  } catch {
    // Quota errors are not fatal; the user can still toggle in-session.
  }
}

function readPermission(): BrowserPermission {
  if (typeof window === 'undefined' || !('Notification' in window)) return 'unsupported'
  return window.Notification.permission as BrowserPermission
}

export function useNotificationSettings() {
  const [settings, setSettings] = useState<NotificationSettings>(() => readStorage())
  const [permission, setPermission] = useState<BrowserPermission>(() => readPermission())

  useEffect(() => {
    writeStorage(settings)
  }, [settings])

  // Re-poll permission on mount + when the tab regains focus, since the user
  // may change it from the browser settings page outside our control.
  useEffect(() => {
    const refresh = () => setPermission(readPermission())
    refresh()
    window.addEventListener('focus', refresh)
    return () => window.removeEventListener('focus', refresh)
  }, [])

  const requestPermission = useCallback(async (): Promise<BrowserPermission> => {
    if (typeof window === 'undefined' || !('Notification' in window)) {
      setPermission('unsupported')
      return 'unsupported'
    }
    try {
      const result = (await window.Notification.requestPermission()) as BrowserPermission
      setPermission(result)
      return result
    } catch {
      setPermission('denied')
      return 'denied'
    }
  }, [])

  const setEnabled = useCallback(
    async (next: boolean) => {
      if (next) {
        // First-time enable triggers the browser permission prompt. If the
        // user denies, surface that state but still flip the toggle to "on"
        // so the disabled-state warning explains why nothing fires.
        const current = readPermission()
        if (current === 'default') {
          await requestPermission()
        }
        // Register the service worker the moment the user opts in so the
        // hidden-tab path is ready before the first event fires. Failure is
        // not fatal — the foreground path still works.
        void ensureServiceWorker()
      }
      setSettings((prev) => ({ ...prev, enabled: next }))
    },
    [requestPermission],
  )

  // Re-register on every page load when the user is already opted in, so a
  // browser update or cache eviction that dropped the SW heals itself.
  useEffect(() => {
    if (settings.enabled) {
      void ensureServiceWorker()
    }
  }, [settings.enabled])

  const setSoundEnabled = useCallback((next: boolean) => {
    setSettings((prev) => ({ ...prev, soundEnabled: next }))
  }, [])

  // shouldFire encapsulates "is everything wired up" so consumers don't repeat
  // the (enabled && permission === 'granted') guard.
  const shouldFire = settings.enabled && permission === 'granted'

  return {
    settings,
    permission,
    shouldFire,
    setEnabled,
    setSoundEnabled,
    requestPermission,
  }
}
