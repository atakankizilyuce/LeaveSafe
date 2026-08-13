// @vitest-environment jsdom

import { h, render } from 'preact';
import { act } from 'preact/test-utils';
import { afterEach, beforeEach, expect, it, vi } from 'vitest';
import { PairScreen } from '../src/components/PairScreen';
import { pairError, pairing } from '../src/lib/store';

// The key field is the whole of the first screen, and its label is the only
// thing naming it. A label that has come loose from its input still looks
// correct on screen — the text is right where it was — so nothing but an
// assertion catches it, and what a screen reader announces over the one field
// this app asks anyone to fill in is worth an assertion.

let host: HTMLDivElement;

beforeEach(() => {
    pairError.value = null;
    pairing.value = false;
    host = document.createElement('div');
    document.body.appendChild(host);
});

afterEach(() => {
    render(null, host);
    host.remove();
    vi.unstubAllGlobals();
});

/** Renders the pairing screen with nothing scanned yet. */
function show() {
    act(() => {
        render(h(PairScreen, { onPair: () => {}, initialKey: '' }), host);
    });
}

// `control` is the association itself rather than a spelling of it: the browser
// resolves the label's target to the element it actually points at, so this
// fails both when the attribute goes missing and when it names an id that is not
// there any more.
it('names the key field with a label that resolves to it', () => {
    show();

    const label = host.querySelector('label');
    const field = host.querySelector('input');

    expect(label).not.toBeNull();
    expect(field).not.toBeNull();
    expect(label?.control).toBe(field);
    expect(field?.labels?.length).toBe(1);
});

// Preact accepts `htmlFor` and `for` alike and writes `for` to the DOM either
// way. The JSX says `htmlFor` because that is the spelling static analysis
// recognises as an association; this checks the DOM did not change with it.
it('writes the association out as a plain for attribute', () => {
    show();

    expect(host.querySelector('label')?.getAttribute('for')).toBe('key');
    expect(host.querySelector('input')?.id).toBe('key');
});
