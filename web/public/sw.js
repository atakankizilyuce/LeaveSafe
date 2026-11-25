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

    if (e.data.type === 'locale') {
        e.waitUntil(
            caches.open('leavesafe-language-v1').then(function (cache) {
                return cache.put(
                    '/notification-language',
                    new Response(JSON.stringify(e.data), {
                        headers: { 'Content-Type': 'application/json' },
                    }),
                );
            }),
        );
        return;
    }

    if (e.data.type === 'alarm') {
        self.registration.showNotification(e.data.title || 'LeaveSafe', {
            body: e.data.message || '',
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
async function notificationLanguage() {
    try {
        const cache = await caches.open('leavesafe-language-v1');
        const stored = await cache.match('/notification-language');
        if (stored) return await stored.json();
    } catch {
        // Older installations or storage restrictions use the source locale.
    }
    return { title: 'LeaveSafe ALERT', fallback: 'Security alarm triggered!', messages: {} };
}

function translatedAlert(message, copy) {
    if (typeof copy.messages?.[message] === 'string') return copy.messages[message];
    const prefixes = [
        'USB removed: ',
        'USB connected: ',
        'USB device removed: ',
        'USB device connected: ',
        'IP address changed (possible network switch). IPs: ',
    ];
    for (const prefix of prefixes) {
        const template = copy.messages?.[prefix + '{device}'];
        if (message.startsWith(prefix) && typeof template === 'string') {
            return template.replace('{device}', function () {
                return message.slice(prefix.length);
            });
        }
    }
    return message || copy.fallback;
}

self.addEventListener('push', function (e) {
    e.waitUntil(
        notificationLanguage().then(function (copy) {
            let message = '';
            try {
                message = e.data?.text() || '';
            } catch {
                /* Still show a generic alarm. */
            }
            return self.registration.showNotification(copy.title, {
                body: translatedAlert(message, copy),
                tag: 'leavesafe-alarm',
                requireInteraction: true,
                renotify: true,
                vibrate: [500, 200, 500, 200, 500, 200, 500, 200, 500],
                icon: '/icons/icon-192.png',
                badge: '/icons/icon-192.png',
            });
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
