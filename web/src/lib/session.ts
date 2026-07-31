// The pairing key, kept so a phone that locked its screen does not have to walk
// back to the laptop and rescan the QR code.
//
// A backgrounded page is discarded by the phone's operating system — routinely
// on Android, almost always on iOS — and everything the page was holding goes
// with it. Without this the user reopens the tab to a pairing screen while their
// laptop sits armed on a café table.
//
// This does put the key on the phone's disk. That is a deliberate trade: the key
// is already printed on screen as a QR code, the phone is the trusted device by
// design, and `rotate-key` on the laptop invalidates it. Settings offers
// "Forget this phone" for anyone who would rather not keep it.

const SESSION_KEY = 'leavesafe_session_v1';

/** A SHA-256 fingerprint is 32 bytes, so 64 hex characters once separators go. */
const SHA256_HEX_LENGTH = 64;

export interface StoredSession {
    key: string;
    /** The certificate fingerprint this key was paired against, if there was one. */
    fingerprint: string | null;
}

export function loadSession(): StoredSession | null {
    try {
        const raw = window.localStorage.getItem(SESSION_KEY);
        if (!raw) return null;
        const parsed = JSON.parse(raw) as Partial<StoredSession>;
        if (typeof parsed.key !== 'string' || parsed.key === '') return null;

        // A stored fingerprint is held to the same standard as one out of a QR
        // code: 64 hex characters or nothing. Taking any string let a damaged
        // or hand-edited entry normalise to empty, which reads downstream as
        // "there was no certificate to check" — so the key would go out to
        // whatever answered, silently, on every visit from then on.
        const fp =
            typeof parsed.fingerprint === 'string' &&
            parsed.fingerprint.replace(/[^0-9a-fA-F]/g, '').length === SHA256_HEX_LENGTH
                ? parsed.fingerprint
                : null;

        // A page loaded over HTTPS was served by a certificate, so there is
        // always one to check. A session with no fingerprint recorded cannot
        // check it, and resuming anyway would hand the key to whichever machine
        // holds this address today. Ask for a rescan instead — on the plain
        // HTTP local path there is genuinely no certificate, and nothing is
        // being given up by carrying on.
        if (fp === null && window.location.protocol === 'https:') return null;

        return { key: parsed.key, fingerprint: fp };
    } catch {
        // A corrupt entry means pairing by hand, which is where a phone that
        // never had one starts anyway.
        return null;
    }
}

export function saveSession(key: string, fingerprint: string | null) {
    try {
        window.localStorage.setItem(SESSION_KEY, JSON.stringify({ key, fingerprint }));
    } catch {
        // Private browsing refuses writes. The session still works until the
        // page goes away, which is the behaviour this file exists to improve on
        // rather than depend on.
    }
}

export function clearSession() {
    try {
        window.localStorage.removeItem(SESSION_KEY);
    } catch {
        // Nothing to do: a storage that refuses removal refused the write too.
    }
}
