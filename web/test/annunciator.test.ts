// @vitest-environment jsdom

import { h, render } from 'preact';
import { act } from 'preact/test-utils';
import { afterEach, beforeEach, expect, it, vi } from 'vitest';
import { Annunciator } from '../src/components/Annunciator';
import type { SensorInfo } from '../src/lib/protocol';
import { armed, sensors, toast, tripped } from '../src/lib/store';

// The word for each sensor is the whole panel in miniature: it is what decides
// where that sensor stands, what its badge is, and whether the user reads the
// laptop as covered. Five conditions can be true of one station at once — no
// such sensor, tripped, faulted, switched off, watching — and only one word
// fits, so which one wins is a decision, not a detail.

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

/** Renders the panel for one sensor and hands back the word for its station. */
function stateOf(over: Partial<SensorInfo> = {}): string {
    sensors.value = [sensor(over)];
    show();
    return host.querySelector('.snode')?.getAttribute('data-state') ?? '';
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

it('reads "ready" for a sensor that is on and working but not yet watching', () => {
    expect(stateOf()).toBe('ready');
});

it('reads "watching" once the laptop is armed', () => {
    armed.value = true;

    expect(stateOf()).toBe('watching');
});

it('reads "off" for a sensor the user switched off', () => {
    expect(stateOf({ enabled: false })).toBe('off');
});

it('reads "fault" when the driver failed, rather than claiming cover it has not got', () => {
    expect(stateOf({ failure: 'the charger driver stopped answering' })).toBe('fault');
});

it('reads "unavailable" on a machine that has no such sensor', () => {
    expect(stateOf({ available: false })).toBe('unavailable');
});

// A sensor the machine does not have cannot have tripped or faulted, so
// "unavailable" is said first — the other words would all be claims about
// something that is not there.
it('says "unavailable" ahead of everything else', () => {
    expect(stateOf({ available: false, enabled: false, failure: 'this never applies' })).toBe('unavailable');
});

// A trip is what the user opened the panel to read. A sensor that tripped and
// then faulted still says it tripped, because that is the event; the fault is
// about the laptop's own health and can wait for the reference list.
it('says a trip ahead of a fault', () => {
    tripped.value = { power: Date.now() };

    expect(stateOf({ failure: 'and then the driver died' })).toBe('tripped');
});

// A faulted sensor is enabled and available and still not covering anything,
// so it must never read as ready — that is the one lie an alarm cannot afford.
it('never reads a faulted sensor as watching, even while armed', () => {
    armed.value = true;

    expect(stateOf({ failure: 'the driver stopped answering' })).toBe('fault');
});

// ── where each station goes once the ring closes ────────────────────────────

/** Renders the given sensors while armed and hands back the stations. */
function armedWith(list: SensorInfo[]): HTMLElement[] {
    armed.value = true;
    sensors.value = list;
    show();
    return Array.from(host.querySelectorAll<HTMLElement>('.snode'));
}

it('leaves every station where it stands while nothing is armed', () => {
    sensors.value = [sensor(), sensor({ name: 'lid', display_name: 'Lid', available: false })];
    show();

    for (const node of host.querySelectorAll('.snode')) {
        expect(node.getAttribute('data-place')).toBe('station');
    }
});

it('pulls the sensors that are actually covering you into the shield', () => {
    const [node] = armedWith([sensor()]);

    expect(node.getAttribute('data-place')).toBe('in');
});

it('sends the ones that are not covering you out, with a reason each', () => {
    armedWith([
        sensor(),
        sensor({ name: 'lid', display_name: 'Lid', available: false }),
        sensor({ name: 'usb', display_name: 'USB', enabled: false }),
        sensor({ name: 'input', display_name: 'Input', failure: 'the driver stopped answering' }),
    ]);

    const out = Array.from(host.querySelectorAll('.snode[data-place="out"]'));
    expect(out).toHaveLength(3);

    const reasons = Array.from(host.querySelectorAll('.ring-out-why span')).map((n) => n.textContent);
    expect(reasons).toEqual([
        'no sensor on this machine',
        'you switched it off',
        'its driver stopped answering',
    ]);
});

// A sensor that has fired is not one of the quiet ones doing its job. Gathering
// it into the shield would hide the only thing on the screen worth looking at.
it('keeps a tripped sensor on the ring rather than inside the shield', () => {
    tripped.value = { power: Date.now() };
    const [node] = armedWith([sensor()]);

    expect(node.getAttribute('data-place')).toBe('trip');
    expect(host.querySelector('.ring-core')?.getAttribute('data-tripped')).toBe('true');
});

// The reserved room under the orbit is the only thing holding the excluded
// stations on screen, and it has to grow a row at a time rather than being
// fixed at two.
it('reserves a row of room under the orbit for every row of excluded sensors', () => {
    armedWith([sensor(), sensor({ name: 'lid', display_name: 'Lid', enabled: false })]);
    expect(host.querySelector<HTMLElement>('.ring')?.style.getPropertyValue('--out-rows')).toBe('1');

    render(null, host);
    armedWith([
        sensor(),
        sensor({ name: 'lid', display_name: 'Lid', enabled: false }),
        sensor({ name: 'usb', display_name: 'USB', enabled: false }),
        sensor({ name: 'screen', display_name: 'Screen', enabled: false }),
        sensor({ name: 'input', display_name: 'Input', enabled: false }),
    ]);
    expect(host.querySelector<HTMLElement>('.ring')?.style.getPropertyValue('--out-rows')).toBe('2');
});

it('holds nothing back from the shield when nothing has been excluded', () => {
    armedWith([sensor()]);

    expect(host.querySelector<HTMLElement>('.ring')?.style.getPropertyValue('--out-rows')).toBe('0');
    expect(host.querySelector('.ring-out')).toBeNull();
});

// The chips inside the shield are the count made concrete: one per sensor it is
// actually holding.
it('puts one chip inside the shield for each sensor it holds', () => {
    armedWith([sensor(), sensor({ name: 'lid', display_name: 'Lid', available: false })]);

    expect(host.querySelectorAll('.core-chip')).toHaveLength(1);
});

// ── the switch ──────────────────────────────────────────────────────────────

it('refuses to change a sensor while the system is armed, and says why', () => {
    armed.value = true;
    stateOf();

    tap('.snode');

    expect(toast.value).toBe('Disarm before changing sensors');
    expect(sensors.value[0].enabled).toBe(true);
});

it('refuses to turn on a sensor the machine has not got, and says why', () => {
    stateOf({ available: false, enabled: false });

    tap('.snode');

    expect(toast.value).toBe('This machine has no charger to watch');
    expect(sensors.value[0].enabled).toBe(false);
});

it('flips the switch on the station the user tapped', () => {
    stateOf();

    tap('.snode');

    expect(sensors.value[0].enabled).toBe(false);
});

// ── what the sensors actually watch ─────────────────────────────────────────

it('opens and closes the reference under the ring', () => {
    stateOf();

    tap('.ring-more');
    expect(host.querySelector('.sref')).not.toBeNull();

    tap('.ring-more');
    expect(host.querySelector('.sref')).toBeNull();
});

it('says in the reference that a faulted sensor is not watching right now', () => {
    stateOf({ failure: 'the charger driver stopped answering' });

    tap('.ring-more');

    expect(host.querySelector('.sref-warn')?.textContent).toContain('the charger driver stopped answering');
});

it('says in the reference that the machine has no such sensor', () => {
    stateOf({ available: false });

    tap('.ring-more');

    expect(host.querySelector('.sref-warn')?.textContent).toContain('has no sensor for that');
});

// The self-test is the one thing in the reference that can be pressed, and
// there is nothing to test on a machine without the sensor.
it('offers the self-test only where there is a sensor to test', () => {
    stateOf();
    tap('.ring-more');
    expect(host.querySelector<HTMLButtonElement>('.sref .chip')?.disabled).toBe(false);

    render(null, host);
    stateOf({ available: false });
    tap('.ring-more');
    expect(host.querySelector<HTMLButtonElement>('.sref .chip')?.disabled).toBe(true);
});

// The reference reads the sensors back out of the store, so a laptop that
// resends a shorter list drops the rows it no longer has rather than
// describing sensors that are gone.
it('drops a row when the sensor it describes goes away', () => {
    stateOf();
    tap('.ring-more');
    expect(host.querySelectorAll('.sref-row')).toHaveLength(1);

    sensors.value = [];
    show();

    expect(host.querySelector('.sref-row')).toBeNull();
});

it('tells the user which way the tap will go', () => {
    stateOf();
    expect(host.querySelector('.card-head .readout:last-child')?.textContent).toBe('Tap to turn on or off');

    armed.value = true;
    show();
    expect(host.querySelector('.card-head .readout:last-child')?.textContent).toBe('Disarm to change');
});
