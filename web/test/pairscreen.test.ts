// @vitest-environment jsdom

import { h, render } from 'preact';
import { act } from 'preact/test-utils';
import { afterEach, beforeEach, expect, it, vi } from 'vitest';
import { PairScreen } from '../src/components/PairScreen';
import { pairError, pairing } from '../src/lib/store';

// The first screen, and for most people the only one they ever type into.
// There are exactly two ways off it — the button and the Enter key — and they
// have to be the same way, because a key typed on a phone ends with the
// keyboard already open and its return key under the thumb.

let host: HTMLDivElement;

beforeEach(() => {
    host = document.createElement('div');
    document.body.appendChild(host);
    pairing.value = false;
    pairError.value = null;
});

afterEach(() => {
    render(null, host);
    host.remove();
});

/** Mounts the screen and hands back what it calls when a key is offered. */
async function show(initialKey = ''): Promise<ReturnType<typeof vi.fn>> {
    const onPair = vi.fn();
    await act(async () => {
        render(h(PairScreen, { onPair, initialKey }), host);
    });
    return onPair;
}

/** Types into the key field the way a person does: one value, one event. */
async function type(value: string) {
    const field = host.querySelector('input') as HTMLInputElement;
    field.value = value;
    await act(async () => {
        field.dispatchEvent(new Event('input', { bubbles: true }));
    });
}

async function press(key: string) {
    const field = host.querySelector('input') as HTMLInputElement;
    await act(async () => {
        field.dispatchEvent(new KeyboardEvent('keydown', { key, bubbles: true }));
    });
}

async function connect() {
    const button = host.querySelector('button') as HTMLButtonElement;
    await act(async () => {
        button.click();
    });
}

it('hands over the digits, without the grouping it showed', async () => {
    // The field groups what it is given so it can be read back off a laptop
    // screen four digits at a time. What goes out is the number.
    const onPair = await show();
    await type('1111222233334448');

    await connect();

    expect(onPair).toHaveBeenCalledWith('1111222233334448');
});

it('and Enter is the same way off the screen as the button', async () => {
    // A key typed on a phone ends with the keyboard open and the return key
    // under the thumb. A screen that only answered the button would make
    // somebody dismiss the keyboard to reach it.
    const onPair = await show();
    await type('1111-2222-3333-4448');

    await press('Enter');

    expect(onPair).toHaveBeenCalledWith('1111222233334448');
});

it('and any other key is just a key', async () => {
    const onPair = await show();
    await type('1111222233334448');

    await press('a');

    expect(onPair).not.toHaveBeenCalled();
});

it('says what is wrong with half a key rather than sending it', async () => {
    // Sixteen digits is the whole of what a key is. Half of one cannot open
    // anything, and sending it would spend one of the laptop's few attempts to
    // be told what this screen already knows.
    const onPair = await show();
    await type('1111-2222');

    await press('Enter');

    expect(onPair).not.toHaveBeenCalled();
    expect(pairError.value).toBe('That key needs 16 digits.');
});

it('fills itself from a code that arrived after it was drawn', async () => {
    // A scan lands after this screen has mounted, so its initial state missed
    // it. Without this the field sits empty behind the auto-connect — fine
    // while that works, and a retype from scratch when it does not.
    const onPair = vi.fn();
    await act(async () => {
        render(h(PairScreen, { onPair, initialKey: '' }), host);
    });
    expect((host.querySelector('input') as HTMLInputElement).value).toBe('');

    await act(async () => {
        render(h(PairScreen, { onPair, initialKey: '1111222233334448' }), host);
    });

    expect((host.querySelector('input') as HTMLInputElement).value).toBe('1111-2222-3333-4448');
});

it('does not offer the button twice while the first press is in flight', async () => {
    // `pairing` is true from the moment a key goes out until the laptop
    // answers. A second press in that window opens a second socket to the same
    // machine, and the first one's answer arrives for a connection nothing is
    // listening to any more.
    await show('1111222233334448');
    pairing.value = true;
    await act(async () => {
        render(h(PairScreen, { onPair: vi.fn(), initialKey: '1111222233334448' }), host);
    });

    const button = host.querySelector('button') as HTMLButtonElement;
    expect(button.disabled).toBe(true);
    expect(button.textContent).toContain('Connecting');
});
