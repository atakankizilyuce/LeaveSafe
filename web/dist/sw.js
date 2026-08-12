self.addEventListener('install', function () {
    self.skipWaiting();
});

self.addEventListener('activate', function (e) {
    e.waitUntil(self.clients.claim());
});

self.addEventListener('message', function (e) {
    // Only the pages this worker serves may raise an alarm notification. A
    // controlled client is same-origin by definition, so this rejects nothing
    // that legitimately arrives — it just stops the handler from trusting the
    // sender implicitly.
    if (e.origin && e.origin !== self.location.origin) return;
    if (!e.data) return;

    if (e.data.type === 'alarm') {
        self.registration.showNotification('LeaveSafe ALERT', {
            body: e.data.message || 'Security alarm triggered!',
            tag: 'leavesafe-alarm',
            requireInteraction: true,
            renotify: true,
            vibrate: [500, 200, 500, 200, 500, 200, 500, 200, 500],
            // Without an icon the notification carries whatever the browser
            // picks — often its own logo, which is the wrong thing to see on a
            // lock screen when your laptop is being taken.
            icon: '/icons/icon-192.png',
            badge: '/icons/icon-192.png',
        });
    }
});

// An alarm that arrived while nothing was open.
//
// The message came from the laptop over its browser's push service, encrypted
// on the way, and the browser has already decrypted it by the time it gets
// here: this is the one path that works when the page is closed, the phone is
// on mobile data and the laptop is behind a NAT nothing can reach. Showing a
// notification is not optional on it — the subscription was made with
// userVisibleOnly, and a browser that sees pushes arrive without one eventually
// takes the subscription away.
self.addEventListener('push', function (e) {
    let message = 'Security alarm triggered!';
    try {
        const text = e.data && e.data.text();
        if (text) message = text;
    } catch {
        // Whatever arrived was not text. Something still has to be shown, and
        // "the alarm went off" is the part that matters.
    }

    e.waitUntil(
        self.registration.showNotification('LeaveSafe ALERT', {
            body: message,
            tag: 'leavesafe-alarm',
            requireInteraction: true,
            renotify: true,
            vibrate: [500, 200, 500, 200, 500, 200, 500, 200, 500],
            icon: '/icons/icon-192.png',
            badge: '/icons/icon-192.png',
        }),
    );
});

self.addEventListener('notificationclick', function (e) {
    e.notification.close();
    e.waitUntil(
        self.clients.matchAll({ type: 'window' }).then(function (clientList) {
            for (const client of clientList) {
                if ('focus' in client) return client.focus();
            }
            if (self.clients.openWindow) return self.clients.openWindow('/');
        }),
    );
});
