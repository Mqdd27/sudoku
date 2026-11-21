// Hand-written service worker. ASSETS and VERSION are filled in at build time (vite.config.ts).
const VERSION = '__VERSION__'
const ASSETS = ['/', ...__ASSETS__]

self.addEventListener('install', (e) => {
  e.waitUntil(caches.open(VERSION).then((c) => c.addAll(ASSETS)).then(() => self.skipWaiting()))
})

self.addEventListener('activate', (e) => {
  e.waitUntil(
    caches.keys()
      .then((keys) => Promise.all(keys.filter((k) => k !== VERSION).map((k) => caches.delete(k))))
      .then(() => self.clients.claim()),
  )
})

self.addEventListener('fetch', (e) => {
  const req = e.request
  const url = new URL(req.url)
  if (req.method !== 'GET' || url.origin !== location.origin) return
  // API and websocket traffic always goes straight to the network, never cached.
  if (url.pathname.startsWith('/api/') || url.pathname === '/api' || url.pathname === '/ws' || url.pathname.startsWith('/ws/')) return
  if (req.mode === 'navigate') {
    // network first so new deploys show up; offline → cached shell
    e.respondWith(fetch(req).catch(() => caches.match('/')))
    return
  }
  e.respondWith(caches.match(req).then((hit) => hit || fetch(req)))
})
