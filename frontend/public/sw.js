/* eslint-disable no-restricted-globals */
// Service worker for background notifications. Receives notification
// descriptors from the main thread via postMessage and calls
// self.registration.showNotification(). Lives outside the bundler so it
// can be registered at /sw.js without import-graph rewrites.
//
// Why postMessage instead of opening a WebSocket inside the SW? Browsers
// aggressively reclaim SW execution time; long-lived connections inside the
// SW thrash and eventually drop. The main thread (or page) is responsible
// for keeping the WS open while it can — the SW only handles the actual
// notification surface so notifications keep firing even after the page is
// hidden but before the OS reclaims the SW.

const NOTIFICATION_TAG_PREFIX = 'rakuten-bot-notify-'

self.addEventListener('install', (event) => {
  // Activate immediately so the first page load (with a freshly registered
  // SW) doesn't have to wait for the next reload to start receiving messages.
  event.waitUntil(self.skipWaiting())
})

self.addEventListener('activate', (event) => {
  event.waitUntil(self.clients.claim())
})

self.addEventListener('message', (event) => {
  const data = event.data
  if (!data || typeof data !== 'object') return
  if (data.type !== 'show-notification') return
  const desc = data.payload
  if (!desc || typeof desc !== 'object') return
  if (typeof desc.title !== 'string') return

  event.waitUntil(
    self.registration.showNotification(desc.title, {
      body: desc.body ?? '',
      tag: NOTIFICATION_TAG_PREFIX + (desc.tag ?? ''),
      icon: desc.icon ?? '/logo192.png',
      badge: '/logo192.png',
      silent: true, // we play our own beep on the main thread when audible
      requireInteraction: desc.requireInteraction === true,
      data: { url: desc.url ?? '/' },
    }),
  )
})

// Click on a notification → focus an existing tab if any, else open a new one.
// This is the "user clicks the OS notification" path; without it the
// notification body just disappears.
self.addEventListener('notificationclick', (event) => {
  event.notification.close()
  const targetUrl = (event.notification.data && event.notification.data.url) || '/'
  event.waitUntil(
    self.clients.matchAll({ type: 'window', includeUncontrolled: true }).then((clientList) => {
      for (const client of clientList) {
        if ('focus' in client) {
          if (client.url.endsWith(targetUrl) || targetUrl === '/') {
            return client.focus()
          }
        }
      }
      if (self.clients.openWindow) {
        return self.clients.openWindow(targetUrl)
      }
      return null
    }),
  )
})
