import { expect, it } from 'vitest';
import {
    checkFingerprint,
    formatFingerprint,
    normalizeFingerprint,
    shortFingerprint,
} from '../src/lib/fingerprint';

// A real SHA-256 certificate fingerprint: 32 bytes, 64 hex characters.
const FP = 'a1b2c3d4e5f60718293a4b5c6d7e8f90a1b2c3d4e5f60718293a4b5c6d7e8f90';

it('strips separators and case so two spellings of one fingerprint compare equal', () => {
    expect(normalizeFingerprint('A1:B2:C3')).toBe('a1b2c3');
    expect(normalizeFingerprint('a1 b2-c3')).toBe('a1b2c3');
    expect(normalizeFingerprint('A1B2C3')).toBe('a1b2c3');
});

it('drops anything that is not hex', () => {
    // A QR code read badly, or a value pasted with stray punctuation, must not
    // turn into a different-but-valid-looking fingerprint.
    expect(normalizeFingerprint('a1:zz:b2')).toBe('a1b2');
    expect(normalizeFingerprint('')).toBe('');
    // Letters that happen to be hex survive; the rest do not. Worth pinning,
    // because it is why a prose string cannot normalise to something empty and
    // then read downstream as "there was no certificate".
    expect(normalizeFingerprint('not a fingerprint')).toBe('afe');
    expect(normalizeFingerprint('xxyyzz')).toBe('');
});

it('displays a fingerprint as colon-separated uppercase octets', () => {
    expect(formatFingerprint('a1b2c3d4')).toBe('A1:B2:C3:D4');
});

it('formats a value that is already separated without doubling the separators', () => {
    expect(formatFingerprint('A1:B2:C3:D4')).toBe('A1:B2:C3:D4');
});

// Nothing to format is an empty string, not a crash and not a stray colon.
it('formats an empty value as empty', () => {
    expect(formatFingerprint('')).toBe('');
    expect(formatFingerprint('::::')).toBe('');
    expect(shortFingerprint('')).toBe('');
});

it('shortens to the first octets, which is what fits on a phone screen', () => {
    expect(shortFingerprint(FP)).toBe('A1:B2:C3:D4:E5:F6:07:18');
    expect(shortFingerprint(FP, 4)).toBe('A1:B2:C3:D4');
});

it('calls a fingerprint that matches the one the code carried a match', () => {
    expect(checkFingerprint(FP, FP)).toBe('match');
});

it('matches across differences in case and separators', () => {
    // The QR code and the server need not spell it the same way.
    expect(checkFingerprint(formatFingerprint(FP), FP)).toBe('match');
    expect(checkFingerprint(FP.toUpperCase(), FP)).toBe('match');
});

it('calls a different fingerprint a mismatch', () => {
    const other = FP.replace(/^a1/, 'ff');
    expect(checkFingerprint(FP, other)).toBe('mismatch');
});

// The code promised a certificate and the server produced none. That is not an
// absence of information — it is the wrong server.
it('treats a missing report against an expected fingerprint as a mismatch', () => {
    expect(checkFingerprint(FP, undefined)).toBe('mismatch');
    expect(checkFingerprint(FP, '')).toBe('mismatch');
    expect(checkFingerprint(FP, ':::')).toBe('mismatch');
});

// A key typed in by hand, or the plain-HTTP local path, has no certificate to
// check. Refusing there would break the ordinary local case for no gain.
it('reports unverified when there was nothing to compare against', () => {
    expect(checkFingerprint(null, FP)).toBe('unverified');
    expect(checkFingerprint('', FP)).toBe('unverified');
    expect(checkFingerprint(null, undefined)).toBe('unverified');
});

// An expected value made only of separators normalises to empty. It must read
// as "nothing to check" rather than accidentally matching an empty report.
it('reports unverified when the expected value holds no hex at all', () => {
    expect(checkFingerprint('::::', FP)).toBe('unverified');
    expect(checkFingerprint('::::', '')).toBe('unverified');
});
