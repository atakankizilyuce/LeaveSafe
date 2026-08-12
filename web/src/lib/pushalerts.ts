// Being told about an alarm when this page is not open.
//
// Everything else here needs a live connection to the laptop, and the alarm
// that matters most is the one where there is not one: the laptop is behind
// carrier-grade NAT or a hotspot, or this phone is on mobile data, and nothing
// can be opened in either direction. The page then hears nothing, so it shows
// nothing, and the first the owner knows is when they go back to their desk.
//
// A push subscription is arranged while this phone is next to the laptop —
// which is now, because pairing is what just happened — and the laptop uses it
// later, over an outbound request that no NAT stands in the way of. What
// arrives is encrypted to a key that never left this phone; the push service
// carrying it is not trusted with the contents.

import { send } from './store';

/**
 * Offer the laptop somewhere to reach this phone.
 *
 * Called on every successful pairing, and cheap to call repeatedly: a browser
 * hands back the subscription it already has rather than making a second one,
 * and the laptop keys them by endpoint so re-sending the same one changes
 * nothing.
 *
 * Everything here is allowed to fail quietly. Push is the last resort for an
 * alarm nobody is watching for; a phone without it is the phone this app has
 * always been, and interrupting a successful pairing with a notification
 * permission prompt nobody asked for would be a worse trade.
 */
export async function offerPushSubscription(applicationServerKey?: string) {
    if (!applicationServerKey) return;

    try {
        const registration = await navigator.serviceWorker?.ready;
        if (!registration?.pushManager) return;

        // Only ask if there is nothing already: a phone that has subscribed
        // before must not be prompted again on every pairing, and one that
        // said no must not be nagged.
        const existing = await registration.pushManager.getSubscription();
        const subscription = existing ?? (await subscribe(registration, applicationServerKey));
        if (!subscription) return;

        const keys = extractKeys(subscription);
        if (!keys) return;

        send({ type: 'push_subscribe', push: { endpoint: subscription.endpoint, ...keys } });
    } catch {
        // No service worker, no push support, permission refused, or a push
        // service that would not issue a subscription. The alarm still reaches
        // this phone whenever it is connected.
    }
}

async function subscribe(
    registration: ServiceWorkerRegistration,
    applicationServerKey: string,
): Promise<PushSubscription | null> {
    // Notification permission is what a push subscription is really asking for,
    // and a browser will refuse the subscription without it.
    if (typeof Notification === 'undefined') return null;
    if (Notification.permission === 'denied') return null;
    if (Notification.permission === 'default') {
        const granted = await Notification.requestPermission();
        if (granted !== 'granted') return null;
    }

    return await registration.pushManager.subscribe({
        // Required by every browser: a subscription that would accept a message
        // from anyone is one that will wake this phone for anyone.
        userVisibleOnly: true,
        applicationServerKey: decodeKey(applicationServerKey),
    });
}

/** The two secrets the browser generated, as the laptop expects them. */
function extractKeys(subscription: PushSubscription): { key: string; auth: string } | null {
    const key = subscription.getKey?.('p256dh');
    const auth = subscription.getKey?.('auth');
    if (!key || !auth) return null;
    return { key: encodeKey(key), auth: encodeKey(auth) };
}

/**
 * base64url to the Uint8Array the Push API insists on.
 *
 * It will not take the string, and it will not take a string with the URL
 * alphabet's characters left in it either, so both are undone here.
 */
function decodeKey(value: string): Uint8Array<ArrayBuffer> {
    const padded = value.padEnd(value.length + ((4 - (value.length % 4)) % 4), '=');
    const binary = atob(padded.replaceAll('-', '+').replaceAll('_', '/'));
    const out = new Uint8Array(new ArrayBuffer(binary.length));
    for (let i = 0; i < binary.length; i++) out[i] = binary.codePointAt(i) ?? 0;
    return out;
}

/** And back the other way, unpadded, which is what the laptop reads. */
function encodeKey(buffer: ArrayBuffer): string {
    let binary = '';
    for (const byte of new Uint8Array(buffer)) binary += String.fromCodePoint(byte);
    // The padding is only ever trailing, so dropping every "=" is the same as
    // dropping the run at the end — and needs no expression to backtrack over.
    return btoa(binary).replaceAll('+', '-').replaceAll('/', '_').replaceAll('=', '');
}
