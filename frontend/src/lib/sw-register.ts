// Tiny façade over navigator.serviceWorker so the rest of the app doesn't
// repeat the feature-detect dance. The SW lives at /sw.js — the public/
// folder copy is served as-is by Vite + the production server.
//
// Registration is fire-and-forget; failures only matter when the user has
// notifications turned on, in which case useNotificationSettings will fall
// back to the foreground path.

let registration: ServiceWorkerRegistration | null = null
let registering: Promise<ServiceWorkerRegistration | null> | null = null

export function isServiceWorkerSupported(): boolean {
  return typeof navigator !== 'undefined' && 'serviceWorker' in navigator
}

export async function ensureServiceWorker(): Promise<ServiceWorkerRegistration | null> {
  if (!isServiceWorkerSupported()) return null
  if (registration) return registration
  if (registering) return registering
  registering = navigator.serviceWorker
    .register('/sw.js')
    .then((reg) => {
      registration = reg
      return reg
    })
    .catch((err) => {
      console.warn('[sw-register] registration failed', err)
      return null
    })
    .finally(() => {
      registering = null
    })
  return registering
}

export async function postToServiceWorker(message: unknown): Promise<boolean> {
  const reg = await ensureServiceWorker()
  if (!reg) return false
  // active is the controlling worker. waiting/installing are not yet ready
  // to accept messages, so we fall back to navigator.serviceWorker.controller
  // for the very first registration before activate fires.
  const target = reg.active || navigator.serviceWorker.controller
  if (!target) return false
  try {
    target.postMessage(message)
    return true
  } catch (err) {
    console.warn('[sw-register] postMessage failed', err)
    return false
  }
}
