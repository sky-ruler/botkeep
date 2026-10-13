// BotKeep SRE Service Worker v2.1.0
// High-resilience PWA shell caching & Direct Web Push notification engine

const CACHE_NAME = 'botkeep-shell-v2';
const STATIC_ASSETS = [
  '/',
  '/status',
  '/manifest.json',
  '/static/css/output.css',
  '/static/js/htmx.min.js',
  '/static/js/webpush.js',
  '/static/icons/icon.svg',
  '/static/icons/icon-192.png',
  '/static/icons/icon-512.png'
];

// Install: Cache essential app shell
self.addEventListener('install', (event) => {
  event.waitUntil(
    caches.open(CACHE_NAME).then((cache) => {
      return cache.addAll(STATIC_ASSETS).catch((err) => {
        console.warn('[SW] Pre-caching non-fatal asset warning:', err);
      });
    }).then(() => self.skipWaiting())
  );
});

// Activate: Prune stale caches
self.addEventListener('activate', (event) => {
  event.waitUntil(
    caches.keys().then((keys) => {
      return Promise.all(
        keys.filter((key) => key !== CACHE_NAME).map((key) => caches.delete(key))
      );
    }).then(() => self.clients.claim())
  );
});

// Fetch: Network-first for dynamic live metrics, cache-fallback for offline reliability
self.addEventListener('fetch', (event) => {
  const req = event.request;
  const url = new URL(req.url);

  // Bypass non-GET, SSE streaming, and API POST endpoints
  if (req.method !== 'GET' || url.pathname.startsWith('/events') || url.pathname.startsWith('/api/')) {
    return;
  }

  // Static assets: Cache-first with network refresh
  if (url.pathname.startsWith('/static/') || url.pathname === '/manifest.json') {
    event.respondWith(
      caches.match(req).then((cached) => {
        if (cached) return cached;
        return fetch(req).then((resp) => {
          if (resp && resp.status === 200) {
            const clone = resp.clone();
            caches.open(CACHE_NAME).then((cache) => cache.put(req, clone));
          }
          return resp;
        });
      })
    );
    return;
  }

  // HTML navigation pages: Network-first with cache fallback
  if (req.mode === 'navigate') {
    event.respondWith(
      fetch(req).catch(() => {
        return caches.match(req).then((cached) => {
          if (cached) return cached;
          return caches.match('/status'); // Fallback to cached status page
        });
      })
    );
  }
});

// Push: Native Web Push Dispatch
self.addEventListener('push', (event) => {
  let data = {
    title: '🚨 BotKeep SRE Fleet Alert',
    body: 'Automated monitoring detected cluster status change.',
    url: '/status',
    tag: 'botkeep-incident-' + Date.now()
  };

  if (event.data) {
    try {
      const parsed = event.data.json();
      data = Object.assign(data, parsed);
    } catch (e) {
      data.body = event.data.text();
    }
  }

  const options = {
    body: data.body,
    icon: data.icon || '/static/icons/icon-192.png',
    badge: data.badge || '/static/icons/icon-192.png',
    vibrate: [200, 100, 200],
    tag: data.tag || 'botkeep-alert',
    renotify: true,
    requireInteraction: true,
    data: {
      url: data.url || '/status',
      timestamp: Date.now()
    },
    actions: [
      { action: 'open', title: 'Inspect Node / Status' },
      { action: 'dismiss', title: 'Dismiss' }
    ]
  };

  event.waitUntil(
    self.registration.showNotification(data.title, options)
  );
});

// Notification Click: Navigate or focus open window
self.addEventListener('notificationclick', (event) => {
  event.notification.close();
  if (event.action === 'dismiss') return;

  const targetUrl = (event.notification.data && event.notification.data.url) || '/';

  event.waitUntil(
    clients.matchAll({ type: 'window', includeUncontrolled: true }).then((clientList) => {
      // If a window is already open, focus and navigate it
      for (let i = 0; i < clientList.length; i++) {
        const client = clientList[i];
        if (client.url && 'focus' in client) {
          if ('navigate' in client) {
            client.navigate(targetUrl);
          }
          return client.focus();
        }
      }
      // Otherwise open new window
      if (clients.openWindow) {
        return clients.openWindow(targetUrl);
      }
    })
  );
});

// Client Message: Allow foreground pages or SSE listener to request notification display
self.addEventListener('message', (event) => {
  if (!event.data) return;

  if (event.data.type === 'SKIP_WAITING') {
    self.skipWaiting();
    return;
  }

  if (event.data.type === 'SHOW_NOTIFICATION') {
    const payload = event.data;
    const options = {
      body: payload.body || 'Alert triggered on cluster fleet.',
      icon: payload.icon || '/static/icons/icon-192.png',
      badge: payload.badge || '/static/icons/icon-192.png',
      tag: payload.tag || 'botkeep-sse-alert',
      vibrate: [150, 75, 150],
      renotify: true,
      data: {
        url: payload.url || '/status'
      },
      actions: [
        { action: 'open', title: 'View Telemetry' },
        { action: 'dismiss', title: 'Dismiss' }
      ]
    };

    self.registration.showNotification(payload.title || '🚨 BotKeep Alert', options);
  }
});
