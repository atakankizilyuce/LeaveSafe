import { afterEach, beforeEach, expect, it, vi } from 'vitest';

// The subscription is arranged while the phone is next to the laptop — pairing
// is what has just happened — and used later, over a path no NAT stands in the
// way of. Everything here is allowed to fail quietly: push is the last resort
// for an alarm nobody is watching for, and a phone without it is the phone this
// app has always been.

const sent: unknown[] = [];

vi.mock('../src/lib/store', () => ({
    send: (msg: unknown) => sent.push(msg),
}));

// A stand-in for what the browser hands back: two keys, as ArrayBuffers.
function keyOf(byte: number, length: number): ArrayBuffer {
    return new Uint8Array(length).fill(byte).buffer;
}

type Options = {
    permission?: NotificationPermission;
    existing?: unknown;
    subscribeFails?: boolean;
    noPushManager?: boolean;
    noKeys?: boolean;
};

function installPhone(opts: Options = {}) {
    const asked = { permission: 0, subscribed: 0, applicationServerKey: null as unknown };

    const subscription = {
        endpoint: 'https://fcm.googleapis.com/fcm/send/abc',
        getKey: (name: string) =>
            opts.noKeys ? null : keyOf(name === 'p256dh' ? 1 : 2, name === 'p256dh' ? 65 : 16),
    };

    const registration = {
        pushManager: opts.noPushManager
            ? undefined
            : {
                  getSubscription: () => Promise.resolve(opts.existing ?? null),
                  subscribe: (options: { applicationServerKey: unknown }) => {
                      asked.subscribed++;
                      asked.applicationServerKey = options.applicationServerKey;
                      return opts.subscribeFails
                          ? Promise.reject(new Error('the push service refused'))
                          : Promise.resolve(subscription);
                  },
              },
    };

    vi.stubGlobal('navigator', { serviceWorker: { ready: Promise.resolve(registration) } });
    vi.stubGlobal('Notification', {
        permission: opts.permission ?? 'granted',
        requestPermission: () => {
            asked.permission++;
            return Promise.resolve(opts.permission === 'default' ? 'denied' : 'granted');
        },
    });
    // atob/btoa exist in browsers and in vitest's node environment alike.
    return { asked, subscription };
}

async function loadPush() {
    vi.resetModules();
    return await import('../src/lib/pushalerts');
}

beforeEach(() => {
    sent.length = 0;
});

afterEach(() => {
    vi.unstubAllGlobals();
});

const VAPID = 'BP4z9KsN6nGRTbVYI_c7VJSPQTBtkgcy27mlmlMoZIIgDll6e3vCYLocInmYWAmS6TlzAC8wEqKK6PBru3jl7A8';

it('hands the laptop somewhere to reach this phone', async () => {
    const { asked } = installPhone();
    const push = await loadPush();

    await push.offerPushSubscription(VAPID);

    expect(sent).toHaveLength(1);
    const msg = sent[0] as { type: string; push: { endpoint: string; key: string; auth: string } };
    expect(msg.type).toBe('push_subscribe');
    expect(msg.push.endpoint).toBe('https://fcm.googleapis.com/fcm/send/abc');
    // Unpadded base64url, which is what the laptop reads.
    expect(msg.push.key).not.toContain('=');
    expect(msg.push.key).not.toContain('+');
    expect(msg.push.auth).not.toContain('=');

    // The Push API will not take the key as a string.
    expect(asked.applicationServerKey).toBeInstanceOf(Uint8Array);
    expect(asked.applicationServerKey as Uint8Array).toHaveLength(65);
});

// A phone that has subscribed before must not be prompted again on every
// pairing — and the laptop keys subscriptions by endpoint, so re-sending the
// one it already has changes nothing there either.
it('reuses the subscription it already has rather than asking again', async () => {
    const { asked, subscription } = installPhone({ existing: undefined });
    const push = await loadPush();
    await push.offerPushSubscription(VAPID);
    expect(asked.subscribed).toBe(1);

    const second = installPhone({ existing: subscription });
    const pushAgain = await loadPush();
    await pushAgain.offerPushSubscription(VAPID);

    expect(second.asked.subscribed).toBe(0);
    expect(second.asked.permission).toBe(0);
    expect(sent).toHaveLength(2);
});

// A phone that said no must not be nagged on every pairing.
it('does not ask again once permission has been refused', async () => {
    const { asked } = installPhone({ permission: 'denied' });
    const push = await loadPush();

    await push.offerPushSubscription(VAPID);

    expect(asked.permission).toBe(0);
    expect(asked.subscribed).toBe(0);
    expect(sent).toHaveLength(0);
});

it('asks for permission when it has never been answered', async () => {
    const { asked } = installPhone({ permission: 'default' });
    const push = await loadPush();

    await push.offerPushSubscription(VAPID);

    expect(asked.permission).toBe(1);
    // Refused at the prompt, so nothing was subscribed and nothing was sent.
    expect(asked.subscribed).toBe(0);
    expect(sent).toHaveLength(0);
});

// Without the laptop's key there is nothing to subscribe to. An older laptop
// that does not send one is not a reason to prompt anybody for anything.
it('does nothing at all without a key from the laptop', async () => {
    const { asked } = installPhone();
    const push = await loadPush();

    await push.offerPushSubscription(undefined);

    expect(asked.permission).toBe(0);
    expect(asked.subscribed).toBe(0);
    expect(sent).toHaveLength(0);
});

// Every one of these is a phone that carries on working exactly as it did
// before: the alarm still reaches it whenever it is connected.
it('carries on quietly when the phone cannot do push at all', async () => {
    for (const opts of [{ noPushManager: true }, { subscribeFails: true }, { noKeys: true }]) {
        sent.length = 0;
        installPhone(opts);
        const push = await loadPush();

        await expect(push.offerPushSubscription(VAPID)).resolves.toBeUndefined();

        expect(sent).toHaveLength(0);
    }
});

it('carries on quietly when there is no service worker', async () => {
    vi.stubGlobal('navigator', {});
    const push = await loadPush();

    await expect(push.offerPushSubscription(VAPID)).resolves.toBeUndefined();

    expect(sent).toHaveLength(0);
});
