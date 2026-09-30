/* Notifications only: intentionally no fetch handler or asset cache. */
self.addEventListener('push', event => {
  let message
  try { message = event.data?.json() } catch { return }
  if (!message) return
  event.waitUntil(self.registration.showNotification(message.title || 'PCAS', {
    body: message.body,
    tag: message.noticeId,
    icon: '/icon-192.png',
    data: { url: message.url },
  }))
})

self.addEventListener('notificationclick', event => {
  event.notification.close()
  event.waitUntil((async () => {
    let url
    try { url = new URL(event.notification.data?.url || '/', self.location.origin) } catch { return }
    if (url.origin !== self.location.origin) return
    const windows = await self.clients.matchAll({ type: 'window', includeUncontrolled: true })
    for (const client of windows) {
      if (new URL(client.url).origin === self.location.origin) {
        const navigated = await client.navigate(url.href)
        await (navigated || client).focus()
        return
      }
    }
    await self.clients.openWindow(url.href)
  })())
})
