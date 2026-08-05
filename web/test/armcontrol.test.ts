// @vitest-environment jsdom

import { signal } from '@preact/signals';
import { h, render } from 'preact';
import { afterEach, beforeEach, expect, it, vi } from 'vitest';

// A phone only lets a page open an audio context from inside a real gesture,
// and Arm is the last thing the user touches before the laptop is left alone.
// If the context is not opened there it is never opened at all, and the alarm
// runs in silence — which is why this belongs to the button rather than to the
// code that plays the tone.

const primeSiren = vi.fn();
const send = vi.fn();
const showToast = vi.fn();
const armed = signal(false);

vi.mock('../src/lib/siren', () => ({
    primeSiren: () => primeSiren(),
    startSiren: vi.fn(),
    stopSiren: vi.fn(),
    warnDisconnected: vi.fn(),
}));

vi.mock('../src/lib/geo', () => ({
    captureAnchor: vi.fn(),
}));

vi.mock('../src/lib/store', () => ({
    armed,
    send: (msg: unknown) => send(msg),
    showToast: (msg: unknown) => showToast(msg),
}));

let host: HTMLDivElement;

beforeEach(() => {
    vi.useFakeTimers();
    armed.value = false;
    primeSiren.mockClear();
    send.mockClear();
    host = document.createElement('div');
    document.body.appendChild(host);
});

afterEach(() => {
    render(null, host);
    host.remove();
    vi.useRealTimers();
});

// mountArmControl renders the button and hands it back.
async function mountArmControl(counting: number | null = null) {
    const { ArmControl } = await import('../src/components/ArmControl');
    render(h(ArmControl, { counting, onCountdown: () => {} }), host);
    const button = host.querySelector('button');
    if (!button) throw new Error('the arm control rendered no button');
    return button;
}

it('opens the audio context inside the tap that arms the system', async () => {
    const button = await mountArmControl();

    button.click();

    expect(primeSiren).toHaveBeenCalledTimes(1);
});

// The countdown gives the user three seconds to change their mind, and
// cancelling is a tap on the same button. The context still has to be opened:
// cancelling is a gesture like any other, and the next tap may be the one that
// arms for real.
it('opens the audio context on the tap that cancels an arming too', async () => {
    const button = await mountArmControl(3);

    button.click();

    expect(primeSiren).toHaveBeenCalledTimes(1);
});

// Priming is free after the first time, so every tap can do it — but a tap that
// disarms must still prime, because that is a gesture the phone will not offer
// again before the next arm.
it('opens the audio context on a tap while armed', async () => {
    armed.value = true;
    const button = await mountArmControl();

    button.click();

    expect(primeSiren).toHaveBeenCalledTimes(1);
});

// The label is the whole affordance: it is the only thing on the button, and it
// is what a screen reader announces. It has to name what the next press does,
// not what the system currently is.
it('offers to arm when nothing is being watched', async () => {
    const button = await mountArmControl();

    expect(button.textContent).toBe('Arm');
    expect(button.getAttribute('aria-label')).toBe('Arm');
});

it('asks for a hold to disarm, so a stray tap cannot switch the guard off', async () => {
    armed.value = true;
    const button = await mountArmControl();

    expect(button.textContent).toBe('Hold to disarm');
});

// During the countdown the armed state underneath has not changed yet, so
// offering to disarm would be a promise about something that is not armed. The
// only thing the press can do is call the arming off.
it('offers only to cancel while the countdown runs', async () => {
    armed.value = true;
    const button = await mountArmControl(3);

    expect(button.textContent).toBe('Cancel');
    expect(button.getAttribute('data-counting')).toBe('true');
});
