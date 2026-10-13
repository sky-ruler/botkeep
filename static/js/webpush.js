// BotKeep Web Push & PWA Notification Controller
// Handles Service Worker registration, Web Push subscription, and real-time desktop/mobile alerts.

(function () {
  'use strict';

  let swRegistration = null;

  const BotKeepPush = {
    isSupported: function () {
      return ('serviceWorker' in navigator && 'Notification' in window);
    },

    getPermissionState: function () {
      if (!('Notification' in window)) return 'unsupported';
      return Notification.permission; // 'granted', 'denied', or 'default'
    },

    init: async function () {
      if (!this.isSupported()) {
        this.updateUI('unsupported');
        return;
      }

      try {
        swRegistration = await navigator.serviceWorker.register('/sw.js', { scope: '/' });
        console.log('[BotKeep PWA] Service Worker registered:', swRegistration.scope);

        // Check current subscription
        await this.syncSubscriptionStatus();
      } catch (err) {
        console.warn('[BotKeep PWA] Service Worker registration failed:', err);
      }

      this.updateUI();
      this.attachSSEListener();
    },

    syncSubscriptionStatus: async function () {
      if (!swRegistration) return;
      try {
        let subscription = null;
        if ('pushManager' in swRegistration) {
          subscription = await swRegistration.pushManager.getSubscription();
        }
        this.updateUI(null, !!subscription);
      } catch (e) {
        console.warn('[BotKeep PWA] Error checking push manager:', e);
      }
    },

    togglePush: async function () {
      if (!this.isSupported()) {
        if (window.showDevOpsToast) {
          window.showDevOpsToast('Browser notifications are not supported by this browser.', 'error');
        }
        return;
      }

      const current = Notification.permission;
      if (current === 'denied') {
        alert('Notifications have been blocked in your browser settings. Please click the lock icon in the address bar to allow notifications for BotKeep.');
        return;
      }

      if (current === 'default') {
        const result = await Notification.requestPermission();
        if (result === 'granted') {
          await this.subscribe();
        } else {
          this.updateUI();
          if (window.showDevOpsToast) {
            window.showDevOpsToast('Notification permissions were not granted.', 'error');
          }
        }
      } else if (current === 'granted') {
        // Toggle: check if already subscribed, if so prompt or test
        const reg = await navigator.serviceWorker.ready;
        let sub = null;
        if ('pushManager' in reg) {
          sub = await reg.pushManager.getSubscription();
        }

        if (sub) {
          await this.unsubscribe();
        } else {
          await this.subscribe();
        }
      }
    },

    subscribe: async function () {
      try {
        const reg = await navigator.serviceWorker.ready;
        let endpoint = '';
        let p256dh = '';
        let auth = '';

        if ('pushManager' in reg) {
          try {
            // Attempt to subscribe with userVisibleOnly
            const sub = await reg.pushManager.subscribe({
              userVisibleOnly: true,
              applicationServerKey: null
            });
            if (sub) {
              endpoint = sub.endpoint;
              const rawKey = sub.getKey('p256dh');
              const rawAuth = sub.getKey('auth');
              if (rawKey) p256dh = btoa(String.fromCharCode.apply(null, new Uint8Array(rawKey)));
              if (rawAuth) auth = btoa(String.fromCharCode.apply(null, new Uint8Array(rawAuth)));
            }
          } catch (pushErr) {
            console.log('[BotKeep PWA] Native PushManager subscribe fallback:', pushErr);
          }
        }

        // Fallback endpoint identifier if browser push service requires standard keys
        if (!endpoint) {
          let clientId = localStorage.getItem('botkeep_push_client_id');
          if (!clientId) {
            clientId = 'client-' + Math.random().toString(36).substring(2, 15) + '-' + Date.now();
            localStorage.setItem('botkeep_push_client_id', clientId);
          }
          endpoint = window.location.origin + '/api/push/client/' + clientId;
          p256dh = 'local-client-key';
          auth = 'local-client-auth';
        }

        // Send subscription to BotKeep server
        const resp = await fetch('/api/push/subscribe', {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({
            endpoint: endpoint,
            p256dh: p256dh,
            auth: auth,
            user_agent: navigator.userAgent
          })
        });

        if (resp.ok) {
          localStorage.setItem('botkeep_push_active', 'true');
          this.updateUI('granted', true);
          if (window.showDevOpsToast) {
            window.showDevOpsToast('✅ Direct Browser Push Notifications active! Alerts will trigger on incidents.');
          }

          // Trigger immediate welcome notification
          reg.showNotification('🔔 BotKeep Alerts Active', {
            body: 'Direct browser and OS alerts are now enabled for this cluster fleet.',
            icon: '/static/icons/icon-192.png',
            badge: '/static/icons/icon-192.png',
            tag: 'botkeep-welcome',
            data: { url: '/notifications' }
          });
        }
      } catch (err) {
        console.error('[BotKeep PWA] Subscribe error:', err);
        if (window.showDevOpsToast) {
          window.showDevOpsToast('Failed to register push subscription: ' + err.message, 'error');
        }
      }
    },

    unsubscribe: async function () {
      try {
        const reg = await navigator.serviceWorker.ready;
        if ('pushManager' in reg) {
          const sub = await reg.pushManager.getSubscription();
          if (sub) {
            await sub.unsubscribe();
            await fetch('/api/push/unsubscribe', {
              method: 'POST',
              headers: { 'Content-Type': 'application/json' },
              body: JSON.stringify({ endpoint: sub.endpoint })
            });
          }
        }

        const clientId = localStorage.getItem('botkeep_push_client_id');
        if (clientId) {
          await fetch('/api/push/unsubscribe', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ endpoint: window.location.origin + '/api/push/client/' + clientId })
          });
        }

        localStorage.removeItem('botkeep_push_active');
        this.updateUI('granted', false);
        if (window.showDevOpsToast) {
          window.showDevOpsToast('Browser push notifications disabled.');
        }
      } catch (err) {
        console.error('[BotKeep PWA] Unsubscribe error:', err);
      }
    },

    testPush: async function () {
      if (Notification.permission !== 'granted') {
        await this.togglePush();
        return;
      }

      try {
        const reg = await navigator.serviceWorker.ready;
        reg.showNotification('🚨 [TEST] Node Anomaly Detected', {
          body: 'github-api-gateway latency spiked to 480ms. Auto-healing runbook standby.',
          icon: '/static/icons/icon-192.png',
          badge: '/static/icons/icon-192.png',
          vibrate: [200, 100, 200],
          tag: 'botkeep-test-' + Date.now(),
          data: { url: '/status' },
          actions: [
            { action: 'open', title: 'Inspect Status' },
            { action: 'dismiss', title: 'Dismiss' }
          ]
        });

        // Also trigger backend broadcast test
        fetch('/api/push/test', { method: 'POST' });

        if (window.showDevOpsToast) {
          window.showDevOpsToast('Sent desktop verification alert!');
        }
      } catch (err) {
        console.error('[BotKeep PWA] Test alert error:', err);
      }
    },

    updateUI: function (permOverride, isSubscribed) {
      const perm = permOverride || this.getPermissionState();
      const isActive = isSubscribed !== undefined ? isSubscribed : (localStorage.getItem('botkeep_push_active') === 'true' && perm === 'granted');

      // 1. Top navigation button / bell
      const navBtn = document.getElementById('pwa-push-toggle');
      if (navBtn) {
        if (isActive) {
          navBtn.classList.add('text-emerald-400', 'border-emerald-500/30');
          navBtn.classList.remove('text-zinc-300', 'border-zinc-800');
          navBtn.title = 'Web Push Active: Click to manage';
          const dot = navBtn.querySelector('.push-indicator-dot');
          if (dot) {
            dot.className = 'push-indicator-dot w-1.5 h-1.5 rounded-full bg-emerald-400 animate-pulse';
          }
        } else {
          navBtn.classList.remove('text-emerald-400', 'border-emerald-500/30');
          navBtn.classList.add('text-zinc-300', 'border-zinc-800');
          navBtn.title = 'Enable Direct Browser Push Alerts';
          const dot = navBtn.querySelector('.push-indicator-dot');
          if (dot) {
            dot.className = 'push-indicator-dot w-1.5 h-1.5 rounded-full bg-zinc-500';
          }
        }
      }

      // 2. Notifications page card status
      const statusBadge = document.getElementById('push-permission-badge');
      if (statusBadge) {
        if (perm === 'granted' && isActive) {
          statusBadge.className = 'inline-flex items-center gap-1.5 px-2.5 py-1 rounded-full text-xs font-medium bg-emerald-500/10 text-emerald-400 border border-emerald-500/20';
          statusBadge.innerHTML = '<span class="w-1.5 h-1.5 rounded-full bg-emerald-400 animate-pulse"></span> Active & Subscribed';
        } else if (perm === 'granted') {
          statusBadge.className = 'inline-flex items-center gap-1.5 px-2.5 py-1 rounded-full text-xs font-medium bg-amber-500/10 text-amber-400 border border-amber-500/20';
          statusBadge.innerHTML = '<span class="w-1.5 h-1.5 rounded-full bg-amber-400"></span> Permission Granted (Inactive)';
        } else if (perm === 'denied') {
          statusBadge.className = 'inline-flex items-center gap-1.5 px-2.5 py-1 rounded-full text-xs font-medium bg-rose-500/10 text-rose-400 border border-rose-500/20';
          statusBadge.innerHTML = '<span class="w-1.5 h-1.5 rounded-full bg-rose-400"></span> Blocked in Browser';
        } else {
          statusBadge.className = 'inline-flex items-center gap-1.5 px-2.5 py-1 rounded-full text-xs font-medium bg-zinc-800 text-zinc-400 border border-zinc-700';
          statusBadge.innerHTML = '<span class="w-1.5 h-1.5 rounded-full bg-zinc-500"></span> Not Configured';
        }
      }

      // 3. Action button text
      const actionBtn = document.getElementById('push-action-btn');
      if (actionBtn) {
        if (isActive) {
          actionBtn.textContent = 'Pause Browser Alerts';
          actionBtn.className = 'px-3 py-1.5 rounded-lg text-xs font-medium bg-zinc-800 text-zinc-300 border border-zinc-700 hover:bg-zinc-700 hover:text-white transition-colors cursor-pointer';
        } else {
          actionBtn.textContent = 'Enable Browser Push';
          actionBtn.className = 'px-3 py-1.5 rounded-lg text-xs font-medium bg-emerald-600 hover:bg-emerald-500 text-white shadow-sm transition-colors cursor-pointer';
        }
      }
    },

    attachSSEListener: function () {
      // Connect to existing live event stream if open
      if (!window.EventSource) return;

      try {
        const es = new EventSource('/events');
        es.addEventListener('incident', (e) => {
          if (Notification.permission === 'granted' && localStorage.getItem('botkeep_push_active') === 'true') {
            try {
              const data = JSON.parse(e.data);
              navigator.serviceWorker.ready.then((reg) => {
                reg.showNotification('🚨 Incident: ' + (data.title || 'Cluster Outage'), {
                  body: data.message || 'Node failure detected by BotKeep consensus probers.',
                  icon: '/static/icons/icon-192.png',
                  badge: '/static/icons/icon-192.png',
                  tag: 'botkeep-incident-' + (data.id || Date.now()),
                  data: { url: '/status' }
                });
              });
            } catch (err) {}
          }
        });
      } catch (e) {}
    }
  };

  // Expose to window
  window.BotKeepPush = BotKeepPush;
  window.toggleWebPush = function () { BotKeepPush.togglePush(); };
  window.testWebPush = function () { BotKeepPush.testPush(); };

  // Initialize on DOM load
  if (document.readyState === 'loading') {
    document.addEventListener('DOMContentLoaded', function () { BotKeepPush.init(); });
  } else {
    BotKeepPush.init();
  }
})();
