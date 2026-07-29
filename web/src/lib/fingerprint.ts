// Checking which server the phone actually reached, before the pairing key
// leaves it.
//
// The certificate is self-signed — there is no authority that could vouch for a
// laptop's address on somebody's LAN — so the browser warns on first connection
// and the user is asked to accept it. Accepting a warning you cannot check is
// not a security decision, it is a habit. So the QR code carries the
// fingerprint of the certificate the laptop is actually serving, and this
// module compares it against what the server says about itself, and puts the
// value on the phone's own screen next to the warning it belongs to.
//
// What this catches: a connection that landed somewhere other than intended —
// a stale port forward, a recycled address, a certificate that changed since
// the code was printed.
//
// What it does not catch: a proxy that terminates TLS with a certificate the
// user accepted and relays honestly to the real laptop. Page JavaScript cannot
// see the certificate of its own connection, so this comparison is on what the
// server reports, not on what the browser verified. Closing that needs a
// password-authenticated key exchange so the key never leaves the phone at all.
// See SECURITY.md.

/** Normalises a fingerprint for comparison: lowercase hex, no separators. */
export function normalizeFingerprint(value: string): string {
    return value.replace(/[^0-9a-fA-F]/g, '').toLowerCase();
}

/** Formats a fingerprint as colon-separated uppercase octets, for display. */
export function formatFingerprint(value: string): string {
    const hex = normalizeFingerprint(value).toUpperCase();
    return (hex.match(/.{1,2}/g) ?? []).join(':');
}

/**
 * The first few octets, which is what fits on a phone screen and is enough to
 * eyeball against a browser warning. The comparison that decides anything uses
 * the whole value.
 */
export function shortFingerprint(value: string, octets = 8): string {
    return formatFingerprint(value).split(':').slice(0, octets).join(':');
}

export type FingerprintVerdict = 'match' | 'mismatch' | 'unverified';

/**
 * Compares the fingerprint the QR code carried against the one the server
 * reports.
 *
 * Returns 'unverified' when there is nothing to compare — a key typed in by
 * hand, or the plain-HTTP local path where there is no certificate at all.
 * That is not a failure: it is the same position every previous version was in,
 * and refusing to pair there would break the ordinary local case for no gain.
 */
export function checkFingerprint(expected: string | null, reported: string | undefined): FingerprintVerdict {
    if (!expected) return 'unverified';
    const want = normalizeFingerprint(expected);
    if (!want) return 'unverified';

    const got = normalizeFingerprint(reported ?? '');
    // The code promised a certificate and the server has none, or a different
    // one. Both mean this is not the server the code was printed for.
    if (!got) return 'mismatch';
    return got === want ? 'match' : 'mismatch';
}
