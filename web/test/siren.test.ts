import { afterEach, beforeEach, expect, it, vi } from 'vitest';

// siren.ts keeps the audio context, the oscillators and its timers in
// module-level state, because the page has one alarm and starting it twice
// should not leave the first one running. That state is also why every test
// here loads a fresh copy of the module instead of trying to unwind the
// previous one.

// FakeNode stands in for both an oscillator and a gain node. connect() returns
// its target so the real code's `osc.connect(gain).connect(destination)` chain
// works, and every call is counted so a test can say what was wired to what.
class FakeNode {
    type = '';
    frequency = { value: 0 };
    gain = { value: 0 };
    started = 0;
    stopped = 0;
    disconnected = 0;
    connectedTo: unknown[] = [];

    connect(target: unknown): unknown {
        this.connectedTo.push(target);
        return target;
    }

    start() {
        this.started++;
    }

    stop() {
        this.stopped++;
    }

    disconnect() {
        this.disconnected++;
    }
}

class FakeAudioContext {
    static built: FakeAudioContext[] = [];

    state = 'running';
    destination = { name: 'destination' };
    resumes = 0;
    closes = 0;
    oscillators: FakeNode[] = [];

    constructor() {
        FakeAudioContext.built.push(this);
    }

    createOscillator(): FakeNode {
        const node = new FakeNode();
        this.oscillators.push(node);
        return node;
    }

    createGain(): FakeNode {
        return new FakeNode();
    }

    resume(): Promise<void> {
        this.resumes++;
        this.state = 'running';
        return Promise.resolve();
    }

    close(): Promise<void> {
        this.closes++;
        return Promise.resolve();
    }
}

type AudioSupport = 'standard' | 'prefixed' | 'none' | 'refused';

// installBrowser puts just enough of a phone's globals in place for siren.ts to
// run under vitest's node environment: the timer functions it reaches for
// through `window`, a title to flash, and a vibrator to buzz. Notification and
// serviceWorker are deliberately absent, so notify() takes its early return and
// the tests stay about sound.
function installBrowser(support: AudioSupport = 'standard') {
    FakeAudioContext.built = [];

    const refusing = () => {
        throw new Error('the browser refused to open an audio context');
    };
    const ctor = support === 'refused' ? refusing : FakeAudioContext;

    const win: Record<string, unknown> = {
        setInterval: (fn: () => void, ms: number) => globalThis.setInterval(fn, ms),
        clearInterval: (id: number) => globalThis.clearInterval(id),
        setTimeout: (fn: () => void, ms: number) => globalThis.setTimeout(fn, ms),
        clearTimeout: (id: number) => globalThis.clearTimeout(id),
    };
    if (support === 'standard') win.AudioContext = ctor;
    if (support === 'prefixed') win.webkitAudioContext = ctor;
    if (support === 'refused') win.AudioContext = ctor;

    const doc = { title: 'LeaveSafe' };
    const nav = { vibrate: vi.fn(() => true) };

    vi.stubGlobal('window', win);
    vi.stubGlobal('document', doc);
    vi.stubGlobal('navigator', nav);

    return { doc, nav };
}

// loadSiren gives each test its own copy of the module, so one test's open
// context is not the next test's starting point.
async function loadSiren() {
    vi.resetModules();
    return await import('../src/lib/siren');
}

beforeEach(() => {
    vi.useFakeTimers();
});

afterEach(() => {
    vi.useRealTimers();
    vi.unstubAllGlobals();
});

it('opens the audio context on the tap that arms the system', async () => {
    installBrowser();
    const siren = await loadSiren();

    siren.primeSiren();

    expect(FakeAudioContext.built).toHaveLength(1);
});

// A phone caps how many contexts a page may open, so priming on every render or
// every tap has to be free after the first one.
it('keeps the one context it opened however often it is primed', async () => {
    installBrowser();
    const siren = await loadSiren();

    siren.primeSiren();
    siren.primeSiren();
    siren.primeSiren();

    expect(FakeAudioContext.built).toHaveLength(1);
});

// A context the browser paused while the tab was in the background is silent
// until something resumes it.
it('resumes a context the browser suspended', async () => {
    installBrowser();
    const siren = await loadSiren();

    siren.primeSiren();
    const ctx = FakeAudioContext.built[0];
    if (!ctx) throw new Error('no context was opened');
    const before = ctx.resumes;
    ctx.state = 'suspended';

    siren.primeSiren();

    expect(ctx.resumes).toBe(before + 1);
    expect(ctx.state).toBe('running');
});

// The regression this whole file exists for. The context used to be closed on
// dismissal, and the replacement built for the second alarm — minutes later,
// with no gesture behind it — stayed suspended and played nothing.
it('leaves the context open when the siren stops, so a second alarm is still audible', async () => {
    installBrowser();
    const siren = await loadSiren();

    siren.startSiren('first alarm');
    siren.stopSiren();

    const ctx = FakeAudioContext.built[0];
    if (!ctx) throw new Error('no context was opened');
    expect(ctx.closes).toBe(0);

    siren.startSiren('second alarm');

    // Still one context, and it produced oscillators for both alarms.
    expect(FakeAudioContext.built).toHaveLength(1);
    expect(ctx.oscillators).toHaveLength(4);
    expect(ctx.oscillators.slice(2).every((node) => node.started === 1)).toBe(true);
});

it('disconnects the oscillators it stops rather than only silencing them', async () => {
    installBrowser();
    const siren = await loadSiren();

    siren.startSiren('an alarm');
    const ctx = FakeAudioContext.built[0];
    if (!ctx) throw new Error('no context was opened');

    siren.stopSiren();

    expect(ctx.oscillators).toHaveLength(2);
    expect(ctx.oscillators.every((node) => node.stopped === 1)).toBe(true);
    expect(ctx.oscillators.every((node) => node.disconnected === 1)).toBe(true);
});

it('wires both tones through their own gain to the output', async () => {
    installBrowser();
    const siren = await loadSiren();

    siren.startSiren('an alarm');

    const ctx = FakeAudioContext.built[0];
    if (!ctx) throw new Error('no context was opened');
    const [fundamental, harmonic] = ctx.oscillators;
    if (!fundamental || !harmonic) throw new Error('the siren built no tones');

    expect(fundamental.frequency.value).toBe(880);
    expect(harmonic.frequency.value).toBe(1760);
    // Each oscillator reaches a gain node, and that gain node reaches the
    // destination — which is what makes the tone audible at all.
    expect(fundamental.connectedTo).toHaveLength(1);
    const gain = fundamental.connectedTo[0] as FakeNode;
    expect(gain.connectedTo).toContain(ctx.destination);
});

// Older iOS only ever exposed the prefixed constructor.
it('falls back to the prefixed constructor when that is all the phone offers', async () => {
    installBrowser('prefixed');
    const siren = await loadSiren();

    siren.primeSiren();

    expect(FakeAudioContext.built).toHaveLength(1);
});

// No audio device, or a browser that refuses. Vibration and the overlay remain,
// and the laptop is sounding its own siren regardless — so the phone must carry
// on rather than throw.
it('carries on silently when the phone has no audio at all', async () => {
    installBrowser('none');
    const siren = await loadSiren();

    expect(() => siren.primeSiren()).not.toThrow();
    expect(() => siren.startSiren('an alarm')).not.toThrow();
    expect(() => siren.stopSiren()).not.toThrow();
    expect(FakeAudioContext.built).toHaveLength(0);
});

it('carries on silently when the browser refuses to open a context', async () => {
    installBrowser('refused');
    const siren = await loadSiren();

    expect(() => siren.primeSiren()).not.toThrow();
    expect(() => siren.startSiren('an alarm')).not.toThrow();
    expect(FakeAudioContext.built).toHaveLength(0);
});

// Stopping the siren has to put the tab title back whatever else it did, or the
// phone keeps reading ALERT long after the alarm was answered.
it('restores the title and stops the buzzing when the siren stops', async () => {
    const { doc, nav } = installBrowser();
    const siren = await loadSiren();

    siren.startSiren('a door opened');
    vi.advanceTimersByTime(500);
    expect(doc.title).not.toBe('LeaveSafe');

    siren.stopSiren();

    expect(doc.title).toBe('LeaveSafe');
    expect(nav.vibrate).toHaveBeenLastCalledWith(0);
});
