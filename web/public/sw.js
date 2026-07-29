self.addEventListener('install', function () {
    self.skipWaiting();
});

self.addEventListener('activate', function (e) {
    e.waitUntil(self.clients.claim());
});

self.addEventListener('message', function (e) {
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

self.addEventListener('notificationclick', function (e) {
    e.notification.close();
    e.waitUntil(
        self.clients.matchAll({ type: 'window' }).then(function (clientList) {
            for (var i = 0; i < clientList.length; i++) {
                var client = clientList[i];
                if ('focus' in client) return client.focus();
            }
            if (self.clients.openWindow) return self.clients.openWindow('/');
        }),
    );
});
