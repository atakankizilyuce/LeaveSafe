// @vitest-environment jsdom

import { h, render } from 'preact';
import { act } from 'preact/test-utils';
import { afterEach, beforeEach, expect, it, vi } from 'vitest';
import { Trace } from '../src/components/Trace';
import { armed, link, tripped } from '../src/lib/store';

// The trace is the only part of the panel that says "still running" rather than
// reporting a value, so what it is drawn in matters: the colour is the claim. A
// line drawn in the alarm colour while the phone cannot hear the laptop would
// be the panel asserting cover it no longer has.

const STANDBY = '#6c8cff';
const ARMED = '#ff4a35';
const LOST = '#55637a';

/**
 * Just enough of a 2D context to let the component draw, and to record what it
 * drew with.
 *
 * jsdom's canvas has no context at all without a native package, and the
 * component gives up quietly when getContext returns nothing — so without this
 * stand-in every assertion below would pass against a canvas that never drew.
 */
class FakeContext {
    strokeStyle = '';
    globalAlpha = 1;
    lineWidth = 0;
    lineJoin = '';
    /** One entry per stroke() call: what the line was drawn with at that moment. */
    strokes: Array<{ colour: string; alpha: number; points: number; newest: number }> = [];
    private points = 0;
    private newest = 0;

    scale() {}
    clearRect() {}
    beginPath() {
        this.points = 0;
        this.newest = 0;
    }
    moveTo(_x: number, y: number) {
        this.at(y);
    }
    lineTo(_x: number, y: number) {
        this.at(y);
    }
    stroke() {
        this.strokes.push({
            colour: String(this.strokeStyle),
            alpha: this.globalAlpha,
            points: this.points,
            newest: this.newest,
        });
    }

    // The newest sample is pushed onto the end of the window, so it is the last
    // point of the path — and how far it sits from the middle is the height of
    // whatever the trace is reporting this frame.
    private at(y: number) {
        this.points += 1;
        this.newest = Math.abs(17 - y);
    }
}

let host: HTMLDivElement;
let ctx: FakeContext;
let frames: Array<() => void>;
let cancelled: number;
let realGetContext: typeof HTMLCanvasElement.prototype.getContext;

/** Answers the reduced-motion question the component asks before it draws. */
function wantsReducedMotion(reduced: boolean) {
    vi.stubGlobal('matchMedia', (query: string) => ({
        matches: reduced,
        media: query,
        addEventListener: () => {},
        removeEventListener: () => {},
    }));
}

beforeEach(() => {
    ctx = new FakeContext();
    frames = [];
    cancelled = 0;

    // Reduced motion by default, so the component draws one frame instead of
    // scheduling itself forever. The tests that care about the animation ask
    // for it back.
    wantsReducedMotion(true);
    vi.stubGlobal('requestAnimationFrame', (fn: () => void) => frames.push(fn));
    vi.stubGlobal('cancelAnimationFrame', () => {
        cancelled += 1;
    });

    realGetContext = HTMLCanvasElement.prototype.getContext;
    HTMLCanvasElement.prototype.getContext = (() =>
        ctx) as unknown as typeof HTMLCanvasElement.prototype.getContext;

    tripped.value = {};
    armed.value = false;
    link.value = 'live';
    host = document.createElement('div');
    document.body.appendChild(host);
});

afterEach(() => {
    render(null, host);
    host.remove();
    HTMLCanvasElement.prototype.getContext = realGetContext;
    vi.unstubAllGlobals();
});

/** Mounts the trace and hands back the frame it drew. */
function draw() {
    show();
    const last = ctx.strokes.at(-1);
    if (!last) throw new Error('the trace drew nothing');
    return last;
}

/** Renders the trace, flushing the effects a real paint would have flushed. */
function show() {
    act(() => {
        render(h(Trace, {}), host);
    });
}

it('draws in the standby colour when nothing is armed', () => {
    expect(draw().colour).toBe(STANDBY);
});

it('draws in the armed colour once the laptop is being watched', () => {
    armed.value = true;

    expect(draw().colour).toBe(ARMED);
});

// The lost colour outranks the armed one. With the link down the trace is no
// longer reporting on the laptop at all — it is only proof this page is still
// running — and drawing that in the alarm colour would say otherwise.
it('draws in the lost colour even while armed', () => {
    armed.value = true;
    link.value = 'lost';

    expect(draw().colour).toBe(LOST);
});

it('fades the line when the connection is lost', () => {
    expect(draw().alpha).toBeCloseTo(0.85);

    render(null, host);
    link.value = 'lost';
    expect(draw().alpha).toBeCloseTo(0.4);
});

it('draws every point of the baseline', () => {
    expect(draw().points).toBe(96);
});

// Flat is the reassuring state, and it has to look flat. The idle wander is
// deliberately a fraction of the height, so a glance reads "nothing happening"
// rather than "activity".
it('keeps the idle line close to flat', () => {
    expect(draw().newest).toBeLessThan(1);
});

it('schedules the next frame when the user has not asked for less motion', () => {
    wantsReducedMotion(false);

    draw();

    expect(frames).toHaveLength(1);
});

it('stops the animation when it is taken off the screen', () => {
    wantsReducedMotion(false);
    draw();

    act(() => {
        render(null, host);
    });

    expect(cancelled).toBeGreaterThan(0);
});

// A sensor tripping is the one thing the line is there to show. It arrives as a
// timestamp in the store rather than as a call, so the component has to notice
// the newest one is newer than the last it drew.
it('spikes the line when a sensor trips, then settles back', () => {
    wantsReducedMotion(false);
    show();
    const idle = ctx.strokes.at(-1)?.newest ?? 0;

    tripped.value = { power: Date.now() };
    show();
    frames.pop()?.();
    const spike = ctx.strokes.at(-1)?.newest ?? 0;

    expect(spike).toBeGreaterThan(idle);
    expect(spike).toBeGreaterThan(1);

    // It decays rather than being cleared, so the line falls back to flat over
    // the frames after it instead of snapping.
    let height = spike;
    for (let i = 0; i < 12; i++) {
        frames.pop()?.();
        height = ctx.strokes.at(-1)?.newest ?? 0;
    }
    expect(height).toBeLessThan(spike);
});

it('redraws to the new size when the window changes', () => {
    show();
    const before = ctx.strokes.length;

    window.dispatchEvent(new Event('resize'));

    expect(ctx.strokes.length).toBeGreaterThanOrEqual(before);
});

// The canvas can take focus, so hiding the decoration from assistive technology
// has to happen on the wrapper. On the canvas itself it would leave a focusable
// node that a screen reader cannot see.
it('hides the decoration from assistive technology at the wrapper', () => {
    show();

    expect(host.querySelector('.trace-wrap')?.getAttribute('aria-hidden')).toBe('true');
    expect(host.querySelector('canvas')?.hasAttribute('aria-hidden')).toBe(false);
});
