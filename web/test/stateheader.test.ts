// @vitest-environment jsdom

import { h, render } from 'preact';
import { act } from 'preact/test-utils';
import { afterEach, beforeEach, expect, it, vi } from 'vitest';
import { StateHeader } from '../src/components/StateHeader';
import type { SensorInfo } from '../src/lib/protocol';
import { armed, armedSince, link, sensors } from '../src/lib/store';

// The one thing that should be readable from across the room, and the line
// under it that says how much of that word is still true. Three states can each
// be the answer — a countdown running, armed, neither — and the header has to
// pick the one the user is actually in.

let host: HTMLDivElement;

beforeEach(() => {
    // The activity trace inside the header asks whether the user wants reduced
    // motion before it draws, and jsdom implements no media queries.
    vi.stubGlobal('matchMedia', (query: string) => ({
        matches: true,
        media: query,
        addEventListener: () => {},
        removeEventListener: () => {},
    }));

    vi.useFakeTimers();
    sensors.value = [];
    armed.value = false;
    armedSince.value = null;
    link.value = 'live';
    host = document.createElement('div');
    document.body.appendChild(host);
});

afterEach(() => {
    render(null, host);
    host.remove();
    vi.useRealTimers();
    vi.unstubAllGlobals();
});

function watching(count: number): SensorInfo[] {
    return Array.from({ length: count }, (_, i) => ({
        name: `sensor-${i}`,
        display_name: `Sensor ${i}`,
        available: true,
        enabled: true,
    }));
}

function header(counting: number | null = null) {
    show(counting);
    return {
        word: host.querySelector('.state-word')?.textContent ?? '',
        detail: host.querySelector('.state-detail')?.textContent ?? '',
    };
}

/** Renders the header, flushing the effects a real paint would have flushed. */
function show(counting: number | null = null) {
    act(() => {
        render(h(StateHeader, { counting }), host);
    });
}

it('says STANDBY when nothing is being watched', () => {
    expect(header().word).toBe('STANDBY');
});

it('says ARMED once the laptop is being watched', () => {
    armed.value = true;

    expect(header().word).toBe('ARMED');
});

// The countdown is the state the user is in, and the only one of the three they
// can still change their mind about, so it is said ahead of the armed state
// underneath it.
it('says ARMING while the countdown runs, whatever is underneath', () => {
    armed.value = true;

    expect(header(3).word).toBe('ARMING');
});

it('shows the seconds left beside the word while arming', () => {
    header(2);

    expect(host.querySelector('.state-count')?.textContent).toBe('2');
});

it('shows no counter when there is no countdown', () => {
    header();

    expect(host.querySelector('.state-count')).toBeNull();
});

it('counts the sensors it is ready to watch', () => {
    sensors.value = watching(3);

    expect(header().detail).toBe('3 sensors ready');
});

it('says "sensor" rather than "sensors" when there is only one', () => {
    sensors.value = watching(1);

    expect(header().detail).toBe('1 sensor ready');
});

it('reports how many it is watching and for how long once armed', () => {
    sensors.value = watching(2);
    armed.value = true;
    armedSince.value = Date.now() - 4 * 60 * 1000;

    expect(header().detail).toContain('Watching 2 sensors');
    expect(header().detail).toContain('4m');
});

it('says "sensor" in the singular while armed too', () => {
    sensors.value = watching(1);
    armed.value = true;
    armedSince.value = Date.now();

    expect(header().detail).toContain('Watching 1 sensor ·');
});

// With the link down, the count and the elapsed time describe a laptop this
// phone is no longer hearing from — last known values dressed as current ones.
// So the lost connection is said on its own, and nothing else is said with it.
it('says only that the connection is lost, even while armed', () => {
    sensors.value = watching(3);
    armed.value = true;
    armedSince.value = Date.now();
    link.value = 'lost';

    const { detail } = header();
    expect(detail).toBe('Connection lost');
    expect(host.querySelector('.state-lost')).not.toBeNull();
});

// "armed 4m ago" quietly becomes a lie unless the readout moves on its own, so
// the header re-renders once a second whether or not anything changed.
it('keeps the elapsed time moving without being told to', () => {
    sensors.value = watching(1);
    armed.value = true;
    armedSince.value = Date.now();

    show();
    expect(host.querySelector('.state-detail')?.textContent).toContain('just now');

    act(() => {
        vi.advanceTimersByTime(90 * 1000);
    });
    expect(host.querySelector('.state-detail')?.textContent).toContain('1m');
});
