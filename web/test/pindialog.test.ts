// @vitest-environment jsdom

import { h, render } from 'preact';
import { act } from 'preact/test-utils';
import { afterEach, beforeEach, expect, it, vi } from 'vitest';
import { PinDialog } from '../src/components/PinDialog';
import { pinPrompt, send } from '../src/lib/store';

// This is the only thing standing between someone holding the phone and turning
// the alarm off. What it must not do is send a disarm the user did not complete
// — and what it must not do either is keep a typed PIN around after the dialog
// has gone.

vi.mock('../src/lib/store', async (importOriginal) => {
    const actual = await importOriginal<typeof import('../src/lib/store')>();
    return { ...actual, send: vi.fn() };
});

const sent = vi.mocked(send);

let host: HTMLDivElement;

/** Renders the dialog in whatever state the store is currently in. */
function show() {
    act(() => {
        render(h(PinDialog, {}), host);
    });
}

/** Types into the PIN field the way a thumb would. */
function type(value: string) {
    const field = host.querySelector<HTMLInputElement>('input');
    if (!field) throw new Error('the dialog has no field to type into');
    act(() => {
        field.value = value;
        field.dispatchEvent(new Event('input', { bubbles: true }));
    });
}

/** Clicks the button whose label reads exactly this. */
function click(label: string) {
    const button = [...host.querySelectorAll('button')].find((b) => b.textContent === label);
    if (!button) throw new Error(`no button labelled ${label}`);
    act(() => {
        button.click();
    });
}

beforeEach(() => {
    sent.mockClear();
    pinPrompt.value = false;
    host = document.createElement('div');
    document.body.appendChild(host);
});

afterEach(() => {
    render(null, host);
    host.remove();
    pinPrompt.value = false;
});

it('is not on the screen until something asks for a PIN', () => {
    show();

    expect(host.querySelector('dialog')).toBeNull();
});

it('appears when a disarm needs a PIN', () => {
    pinPrompt.value = true;

    show();

    expect(host.querySelector('dialog')).not.toBeNull();
    expect(host.querySelector('input')?.getAttribute('type')).toBe('password');
});

// An empty field is a slip, not an instruction. Sending it would spend one of
// the attempts the laptop is counting.
it('refuses an empty PIN without sending anything', () => {
    pinPrompt.value = true;
    show();

    click('Disarm');

    expect(sent).not.toHaveBeenCalled();
    expect(host.textContent).toContain('Enter your PIN to disarm.');
    expect(pinPrompt.value).toBe(true);
});

it('refuses a PIN that is only spaces', () => {
    pinPrompt.value = true;
    show();

    type('   ');
    click('Disarm');

    expect(sent).not.toHaveBeenCalled();
});

it('sends the PIN and closes', () => {
    pinPrompt.value = true;
    show();

    type('1234');
    click('Disarm');

    expect(sent).toHaveBeenCalledWith({ type: 'disarm_with_pin', pin: '1234' });
    expect(pinPrompt.value).toBe(false);
});

// Stray whitespace comes free with a phone keyboard's autocorrect, and the
// laptop compares what it is given.
it('trims the PIN before sending it', () => {
    pinPrompt.value = true;
    show();

    type('  4321  ');
    click('Disarm');

    expect(sent).toHaveBeenCalledWith({ type: 'disarm_with_pin', pin: '4321' });
});

it('submits on Enter as well as on the button', () => {
    pinPrompt.value = true;
    show();
    type('1234');

    const field = host.querySelector<HTMLInputElement>('input');
    act(() => {
        field?.dispatchEvent(new KeyboardEvent('keydown', { key: 'Enter', bubbles: true }));
    });

    expect(sent).toHaveBeenCalledWith({ type: 'disarm_with_pin', pin: '1234' });
});

it('cancels without disarming', () => {
    pinPrompt.value = true;
    show();
    type('1234');

    click('Cancel');

    expect(sent).not.toHaveBeenCalled();
    expect(pinPrompt.value).toBe(false);
});

// Closing has to forget the digits. A PIN left in component state would be
// waiting in the field the next time the dialog opened, for whoever opened it.
it('forgets a typed PIN when it closes', () => {
    pinPrompt.value = true;
    show();
    type('1234');
    click('Cancel');

    pinPrompt.value = true;
    show();

    expect(host.querySelector<HTMLInputElement>('input')?.value).toBe('');
});

// The error belongs to the attempt that caused it, not to the dialog.
it('clears a previous error when it is opened again', () => {
    pinPrompt.value = true;
    show();
    click('Disarm');
    expect(host.textContent).toContain('Enter your PIN to disarm.');

    click('Cancel');
    pinPrompt.value = true;
    show();

    expect(host.textContent).not.toContain('Enter your PIN to disarm.');
});
