// @vitest-environment jsdom

import { h, render } from 'preact';
import { act } from 'preact/test-utils';
import { afterEach, beforeEach, expect, it, vi } from 'vitest';
import { Annunciator } from '../src/components/Annunciator';
import type { SensorInfo } from '../src/lib/protocol';
import { armed, sensors, toast, tripped } from '../src/lib/store';

// The word under each caption is the whole panel in miniature: it is what the
// user reads to decide whether the laptop is covered. Four conditions can be
// true of one tile at once — no such sensor, tripped, faulted, switched off —
// and only one word fits, so which one wins is a decision, not a detail.

function sensor(over: Partial<SensorInfo> = {}): SensorInfo {
    return {
        name: 'power',
        display_name: 'Charger',
        available: true,
        enabled: true,
        ...over,
    };
}

let host: HTMLDivElement;

beforeEach(() => {
    vi.useFakeTimers();
    sensors.value = [];
    tripped.value = {};
    armed.value = false;
    toast.value = null;
    host = document.createElement('div');
    document.body.appendChild(host);
});

afterEach(() => {
    render(null, host);
    host.remove();
    vi.useRealTimers();
});

/** Renders the panel for one sensor and hands back the word under its caption. */
function stateOf(over: Partial<SensorInfo> = {}): string {
    sensors.value = [sensor(over)];
    show();
    return host.querySelector('.cap-state')?.textContent ?? '';
}

/** Renders the panel, flushing the effects a real paint would have flushed. */
function show() {
    act(() => {
        render(h(Annunciator, {}), host);
    });
}

/** Presses something on the panel and lets the panel answer. */
function tap(selector: string) {
    const button = host.querySelector<HTMLButtonElement>(selector);
    if (!button) throw new Error(`nothing matching ${selector} to press`);
    act(() => {
        button.click();
    });
}

it('says nothing at all when the laptop has not sent its sensors yet', () => {
    show();

    expect(host.innerHTML).toBe('');
});

it('reads "ready" for a sensor that is on and working', () => {
    expect(stateOf()).toBe('ready');
});

it('reads "off" for a sensor the user switched off', () => {
    expect(stateOf({ enabled: false })).toBe('off');
});

it('reads "FAULT" when the driver failed, rather than claiming cover it has not got', () => {
    expect(stateOf({ failure: 'the charger driver stopped answering' })).toBe('FAULT');
});

it('reads "n/a" on a machine that has no such sensor', () => {
    expect(stateOf({ available: false })).toBe('n/a');
});

// A sensor the machine does not have cannot have tripped or faulted, so "n/a"
// is said first — the other words would all be claims about something that is
// not there.
it('says "n/a" ahead of everything else', () => {
    expect(stateOf({ available: false, enabled: false, failure: 'this never applies' })).toBe('n/a');
});

// A trip is what the user opened the panel to read. A sensor that tripped and
// then faulted still says it tripped, because that is the event; the fault is
// about the laptop's own health and can wait for the "i".
it('says a trip ahead of a fault', () => {
    tripped.value = { power: Date.now() };

    expect(stateOf({ failure: 'and then the driver died' })).toBe('tripped');
});

it('lights the tile it is showing as tripped', () => {
    tripped.value = { power: Date.now() };
    stateOf();

    expect(host.querySelector('.cap')?.getAttribute('data-lit')).toBe('true');
});

// data-watching is what turns the outline on, and it is the only attribute that
// asks all four questions at once.
it('outlines a tile only when its sensor is armed and actually watching', () => {
    armed.value = true;
    stateOf();
    expect(host.querySelector('.cap')?.getAttribute('data-watching')).toBe('true');

    stateOf({ failure: 'the driver stopped answering' });
    expect(host.querySelector('.cap')?.getAttribute('data-watching')).toBe('false');
});

it('refuses to change a sensor while the system is armed, and says why', () => {
    armed.value = true;
    stateOf();

    tap('.cap');

    expect(toast.value).toBe('Disarm before changing sensors');
    expect(sensors.value[0].enabled).toBe(true);
});

it('refuses to turn on a sensor the machine has not got, and says why', () => {
    stateOf({ available: false, enabled: false });

    tap('.cap');

    expect(toast.value).toBe('This machine has no charger to watch');
    expect(sensors.value[0].enabled).toBe(false);
});

it('flips the switch on the tile the user tapped', () => {
    stateOf();

    tap('.cap');

    expect(sensors.value[0].enabled).toBe(false);
});

it('opens and closes the detail behind the corner "i"', () => {
    stateOf();

    tap('.cap-info');
    expect(host.querySelector('.cap-detail')).not.toBeNull();

    tap('.cap-info');
    expect(host.querySelector('.cap-detail')).toBeNull();
});

it('warns in the detail that a faulted sensor is not watching right now', () => {
    stateOf({ failure: 'the charger driver stopped answering' });

    tap('.cap-info');

    expect(host.querySelector('.cap-warn')?.textContent).toContain('the charger driver stopped answering');
});

it('warns in the detail that the machine has no such sensor', () => {
    stateOf({ available: false });

    tap('.cap-info');

    expect(host.querySelector('.cap-warn')?.textContent).toContain('has no sensor for that');
});

// The self-test is the one thing in the detail that can be pressed, and there
// is nothing to test on a machine without the sensor.
it('offers the self-test only where there is a sensor to test', () => {
    stateOf();
    tap('.cap-info');
    expect(host.querySelector<HTMLButtonElement>('.cap-actions button')?.disabled).toBe(false);

    render(null, host);
    stateOf({ available: false });
    tap('.cap-info');
    expect(host.querySelector<HTMLButtonElement>('.cap-actions button')?.disabled).toBe(true);
});

it('closes the detail from the button inside it', () => {
    stateOf();
    tap('.cap-info');

    tap('.cap-detail .chip');

    expect(host.querySelector('.cap-detail')).toBeNull();
});

// The detail reads the sensor back out of the store by name. A sensor that goes
// away while its detail is open — the laptop resending a shorter list — must
// close it rather than render a panel about nothing.
it('drops the detail when the sensor it describes goes away', () => {
    stateOf();
    tap('.cap-info');

    sensors.value = [sensor({ name: 'lid', display_name: 'Lid' })];
    show();

    expect(host.querySelector('.cap-detail')).toBeNull();
});

it('tells the user which way the tap will go', () => {
    stateOf();
    expect(host.querySelector('.card-head .readout:last-child')?.textContent).toBe('Tap to turn on or off');

    armed.value = true;
    show();
    expect(host.querySelector('.card-head .readout:last-child')?.textContent).toBe('Disarm to change');
});
