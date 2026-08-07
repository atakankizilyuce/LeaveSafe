// @vitest-environment jsdom

import { h, render } from 'preact';
import { act } from 'preact/test-utils';
import { afterEach, beforeEach, expect, it, vi } from 'vitest';
import { Scrim } from '../src/components/Scrim';

// Every overlay in the phone interface is wrapped in this, including the one
// that asks for a PIN before disarming — so the way out of it is not a detail.
// The checks below are about the two things a user would only discover at the
// worst moment: that Escape still works, and that opening settings cannot end
// up covering an alert.

let host: HTMLDivElement;
let closed: number;

/** Renders a scrim with some content inside it. */
function show(label = 'Settings') {
    act(() => {
        render(h(Scrim, { onClose: () => closed++, label }, h('div', { class: 'sheet' }, 'contents')), host);
    });
}

/** Presses a key on the document, the way a real keyboard would reach this. */
function press(key: string) {
    act(() => {
        document.dispatchEvent(new KeyboardEvent('keydown', { key }));
    });
}

beforeEach(() => {
    closed = 0;
    document.body.style.overflow = '';
    host = document.createElement('div');
    document.body.appendChild(host);
});

afterEach(() => {
    render(null, host);
    host.remove();
    vi.unstubAllGlobals();
});

// The rule this satisfies asks for the element rather than the role, and the
// element is the part assistive technology acts on.
it('is a real dialog element rather than a div wearing the role', () => {
    show();

    const dialog = host.querySelector('dialog');
    expect(dialog).not.toBeNull();
    expect(dialog?.hasAttribute('open')).toBe(true);
    expect(dialog?.getAttribute('role')).toBeNull();
    expect(host.querySelector('[role="dialog"]')).toBeNull();
});

it('names itself for a screen reader', () => {
    show('Enter your PIN to disarm');

    const dialog = host.querySelector('dialog');
    expect(dialog?.getAttribute('aria-label')).toBe('Enter your PIN to disarm');
    expect(dialog?.getAttribute('aria-modal')).toBe('true');
});

/*
 * This is the one that matters, and it is a decision rather than a preference.
 *
 * showModal() puts an element in the browser's top layer, which draws above
 * every z-index on the page — including the alert overlay. The alert and this
 * sheet are independent siblings: nothing closes an open settings sheet when an
 * alert arrives. A modal sheet would therefore hide the alert from the one user
 * who was in settings when someone touched their laptop.
 */
it('never asks the browser to make it modal', () => {
    const showModal = vi.fn();
    // jsdom has the interface but not the method, so it has to be planted before
    // it can be watched.
    (HTMLDialogElement.prototype as unknown as { showModal: () => void }).showModal = showModal;

    show();

    expect(showModal).not.toHaveBeenCalled();
});

it('closes on Escape', () => {
    show();

    press('Escape');

    expect(closed).toBe(1);
});

it('ignores keys that are not Escape', () => {
    show();

    press('Enter');
    press('a');

    expect(closed).toBe(0);
});

// The backdrop is a real button rather than a div with a click handler, so
// dismissing works for a keyboard and a screen reader as well as a thumb.
it('closes from the backdrop button', () => {
    show();

    act(() => {
        host.querySelector<HTMLButtonElement>('.scrim-close')?.click();
    });

    expect(closed).toBe(1);
});

it('stops the page behind from scrolling, and gives it back afterwards', () => {
    document.body.style.overflow = 'scroll';

    show();
    expect(document.body.style.overflow).toBe('hidden');

    act(() => {
        render(null, host);
    });
    expect(document.body.style.overflow).toBe('scroll');
});

// A listener left on the document after the sheet has gone would keep closing a
// sheet that is not there, and worse, would answer Escape meant for whatever is.
it('stops listening once it is off the screen', () => {
    show();
    act(() => {
        render(null, host);
    });

    press('Escape');

    expect(closed).toBe(0);
});

it('renders what it was given', () => {
    show();

    expect(host.querySelector('dialog .sheet')?.textContent).toBe('contents');
});
