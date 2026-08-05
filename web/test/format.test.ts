import { afterEach, beforeEach, expect, it, vi } from 'vitest';
import { ago, clock, distance, groupKey, since } from '../src/lib/format';

const NOW = new Date('2026-08-05T12:00:00Z').getTime();

beforeEach(() => {
    vi.useFakeTimers();
    vi.setSystemTime(NOW);
});

afterEach(() => {
    vi.useRealTimers();
});

it('writes short distances in metres and longer ones in kilometres', () => {
    expect(distance(0)).toBe('0 m');
    expect(distance(42.4)).toBe('42 m');
    expect(distance(999)).toBe('999 m');
    expect(distance(1000)).toBe('1.0 km');
    expect(distance(2500)).toBe('2.5 km');
});

// Past ten kilometres the tenth of a kilometre is noise on a phone screen.
it('drops the decimal once the distance is large', () => {
    expect(distance(9999)).toBe('10.0 km');
    expect(distance(10000)).toBe('10 km');
    expect(distance(12345)).toBe('12 km');
});

// A sensor that has never reported has no distance, and a dash says so more
// honestly than a zero would.
it('shows a dash rather than a number it does not have', () => {
    expect(distance(Number.NaN)).toBe('—');
    expect(distance(Number.POSITIVE_INFINITY)).toBe('—');
    expect(ago(undefined)).toBe('—');
    expect(ago(0)).toBe('—');
    expect(since(null)).toBe('—');
});

it('rounds the last few seconds to just now', () => {
    expect(since(NOW)).toBe('just now');
    expect(since(NOW - 9_000)).toBe('just now');
});

it('counts up through seconds, minutes, hours and days', () => {
    expect(since(NOW - 10_000)).toBe('10s');
    expect(since(NOW - 59_000)).toBe('59s');
    expect(since(NOW - 60_000)).toBe('1m');
    expect(since(NOW - 59 * 60_000)).toBe('59m');
    expect(since(NOW - 3_600_000)).toBe('1h 0m');
    expect(since(NOW - 5_400_000)).toBe('1h 30m');
    expect(since(NOW - 86_400_000)).toBe('1d 0h');
    expect(since(NOW - 90_000_000)).toBe('1d 1h');
});

// A clock skew between phone and laptop can put an event marginally in the
// future. Counting backwards would read as a fault in the monitor.
it('never counts into the future when the clocks disagree', () => {
    expect(since(NOW + 30_000)).toBe('just now');
});

it('reads a unix timestamp in seconds, not milliseconds', () => {
    expect(ago(NOW / 1000 - 120)).toBe('2m');
});

// The exact rendering follows the phone's locale and time zone, so the shape is
// what can be asserted on: a 24-hour clock, zero-padded, with seconds.
it('writes the log clock as a padded 24-hour time', () => {
    expect(clock(NOW)).toMatch(/^\d{2}:\d{2}:\d{2}$/);
});

it('groups a pairing key into blocks of four as it is typed', () => {
    expect(groupKey('1234567890123456')).toBe('1234-5678-9012-3456');
    expect(groupKey('1234')).toBe('1234');
    expect(groupKey('12345')).toBe('1234-5');
    expect(groupKey('')).toBe('');
});

// The field is filled by a QR scan as well as by hand, and either can arrive
// carrying separators or more digits than a key has.
it('ignores non-digits and stops at sixteen digits', () => {
    expect(groupKey('1234-5678-9012-3456')).toBe('1234-5678-9012-3456');
    expect(groupKey('12 34 56')).toBe('1234-56');
    expect(groupKey('12345678901234567890')).toBe('1234-5678-9012-3456');
    expect(groupKey('abcd')).toBe('');
});
