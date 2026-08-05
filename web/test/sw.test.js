import { expect, it, vi } from 'vitest';

const ORIGIN = 'https://laptop.local';

// loadWorker runs the real public/sw.js against a stand-in for the worker
// global scope and hands back the listeners it registered. The file is executed
// rather than reimplemented, so a change to it that breaks the alarm shows up
// here instead of on somebody's phone.
async function loadWorker(windows = [], spies = {}) {
    const listeners = {};
    const notifications = [];
    const opened = [];

    const scope = {
        location: { origin: ORIGIN },
        registration: {
            showNotification(title, options) {
                notifications.push({ title, options });
            },
        },
        clients: {
            claim: spies.claim ?? (() => Promise.resolve()),
            matchAll: () => Promise.resolve(windows),
            openWindow(url) {
                opened.push(url);
                return Promise.resolve();
            },
        },
        skipWaiting: spies.skipWaiting ?? (() => {}),
        addEventListener(type, handler) {
            listeners[type] = handler;
        },
    };

    vi.stubGlobal('self', scope);
    vi.resetModules();
    await import('../public/sw.js');

    return { listeners, notifications, opened };
}

// messageEvent is what a paired page posts to the worker.
function messageEvent(origin, data) {
    return { origin, data };
}

it('takes over from any previous worker as soon as it installs', async () => {
    // A phone that reloads mid-alarm must get this worker, not the one it
    // replaced, so neither handler is allowed to wait its turn.
    let skipped = false;
    let claimed = false;

    const { listeners } = await loadWorker([], {
        skipWaiting: () => {
            skipped = true;
        },
        claim: () => {
            claimed = true;
            return Promise.resolve();
        },
    });

    listeners.install({});
    expect(skipped).toBe(true);

    let pending;
    listeners.activate({
        waitUntil: (p) => {
            pending = p;
        },
    });
    await pending;
    expect(claimed).toBe(true);
});

it('raises a notification for an alarm posted by its own page', async () => {
    const { listeners, notifications } = await loadWorker();

    listeners.message(messageEvent(ORIGIN, { type: 'alarm', message: 'Charger unplugged' }));

    expect(notifications).toHaveLength(1);
    expect(notifications[0].title).toBe('LeaveSafe ALERT');
    expect(notifications[0].options.body).toBe('Charger unplugged');
    // A lock screen showing the browser's own logo is the wrong thing to see
    // when the laptop is being taken.
    expect(notifications[0].options.icon).toBe('/icons/icon-192.png');
    expect(notifications[0].options.requireInteraction).toBe(true);
});

it('falls back to a generic body when the alarm carries no message', async () => {
    const { listeners, notifications } = await loadWorker();

    listeners.message(messageEvent(ORIGIN, { type: 'alarm' }));

    expect(notifications[0].options.body).toBe('Security alarm triggered!');
});

it('ignores a message that came from another origin', async () => {
    const { listeners, notifications } = await loadWorker();

    listeners.message(messageEvent('https://not-your-laptop.example', { type: 'alarm' }));

    expect(notifications).toHaveLength(0);
});

it('still accepts a message that carries no origin at all', async () => {
    const { listeners, notifications } = await loadWorker();

    // Some browsers leave origin empty on a client postMessage. Rejecting those
    // would silence the alarm on exactly the phones that need it.
    listeners.message(messageEvent(undefined, { type: 'alarm', message: 'Lid closed' }));

    expect(notifications).toHaveLength(1);
});

it('ignores a message with no payload', async () => {
    const { listeners, notifications } = await loadWorker();

    listeners.message(messageEvent(ORIGIN, null));

    expect(notifications).toHaveLength(0);
});

it('ignores a message that is not an alarm', async () => {
    const { listeners, notifications } = await loadWorker();

    listeners.message(messageEvent(ORIGIN, { type: 'ping' }));

    expect(notifications).toHaveLength(0);
});

it('focuses a page that is already open when the notification is tapped', async () => {
    let focused = false;
    const windows = [
        {
            focus: () => {
                focused = true;
                return Promise.resolve();
            },
        },
    ];
    const { listeners, opened } = await loadWorker(windows);

    let closed = false;
    let pending;
    listeners.notificationclick({
        notification: {
            close: () => {
                closed = true;
            },
        },
        waitUntil: (p) => {
            pending = p;
        },
    });
    await pending;

    expect(closed).toBe(true);
    expect(focused).toBe(true);
    expect(opened).toHaveLength(0);
});

it('opens a page when none is open', async () => {
    const { listeners, opened } = await loadWorker([]);

    let pending;
    listeners.notificationclick({
        notification: { close: () => {} },
        waitUntil: (p) => {
            pending = p;
        },
    });
    await pending;

    expect(opened).toEqual(['/']);
});
