import { afterEach, beforeEach, expect, it, vi } from 'vitest';

// siren.ts keeps the audio context, the oscillators and its timers in
// module-level state, because the page has one alarm and starting it twice
// should not leave the first one running. That state is also why every test
// here loads a fresh copy of the module instead of trying to unwind the
// previous one.

// FakeNode stands in for the node the siren is played through. connect()
// returns its target so a chained `source.connect(x).connect(y)` works, and
// every call is counted so a test can say what was wired to what.
class FakeNode {
    buffer: FakeBuffer | null = null;
    loop = false;
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

// FakeBuffer hands back the same array every time, so a test can read the
// samples the siren wrote into it.
class FakeBuffer {
    samples: Float32Array;

    constructor(
        readonly channels: number,
        readonly length: number,
        readonly sampleRate: number,
    ) {
        this.samples = new Float32Array(length);
    }

    getChannelData(): Float32Array {
        return this.samples;
    }
}

class FakeAudioContext {
    static built: FakeAudioContext[] = [];
    // A phone that has an audio context but will not give out memory for a
    // couple of seconds of sound.
    static refusesBuffers = false;

    state = 'running';
    destination = { name: 'destination' };
    // What a phone actually runs at, which is not the 44.1 kHz the laptop
    // generates at — the siren has to be rendered at whatever it is given.
    sampleRate = 48000;
    resumes = 0;
    closes = 0;
    sources: FakeNode[] = [];
    buffers: FakeBuffer[] = [];

    constructor() {
        FakeAudioContext.built.push(this);
    }

    createBuffer(channels: number, length: number, rate: number): FakeBuffer {
        if (FakeAudioContext.refusesBuffers) throw new Error('cannot allocate an audio buffer');
        const buffer = new FakeBuffer(channels, length, rate);
        this.buffers.push(buffer);
        return buffer;
    }

    createBufferSource(): FakeNode {
        const node = new FakeNode();
        this.sources.push(node);
        return node;
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
    FakeAudioContext.refusesBuffers = false;

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

    // Still one context, and it played a siren for both alarms.
    expect(FakeAudioContext.built).toHaveLength(1);
    expect(ctx.sources).toHaveLength(2);
    expect(ctx.sources.every((node) => node.started === 1)).toBe(true);
});

it('disconnects the siren it stops rather than only silencing it', async () => {
    installBrowser();
    const siren = await loadSiren();

    siren.startSiren('an alarm');
    const ctx = FakeAudioContext.built[0];
    if (!ctx) throw new Error('no context was opened');

    siren.stopSiren();

    expect(ctx.sources).toHaveLength(1);
    expect(ctx.sources.every((node) => node.stopped === 1)).toBe(true);
    expect(ctx.sources.every((node) => node.disconnected === 1)).toBe(true);
});

it('wires the siren to the output and leaves it looping', async () => {
    installBrowser();
    const siren = await loadSiren();

    siren.startSiren('an alarm');

    const ctx = FakeAudioContext.built[0];
    if (!ctx) throw new Error('no context was opened');
    const [source] = ctx.sources;
    const [wave] = ctx.buffers;
    if (!source || !wave) throw new Error('the siren built no sound');

    expect(source.buffer).toBe(wave);
    expect(source.connectedTo).toContain(ctx.destination);
    // Looping in the audio thread, not on a timer: a phone with the screen off
    // throttles setInterval to once a minute, which would leave the alarm
    // silent for the one stretch nobody is looking at the page.
    expect(source.loop).toBe(true);
    expect(source.started).toBe(1);
});

// ---- the sound itself -----------------------------------------------------

// The phone renders at whatever rate its hardware runs at, not the 44.1 kHz the
// laptop generates at, and the length has to come out right either way: ten
// notes of 190 ms, which is a whole number of notes and a whole number of 20 Hz
// roughness cycles, so the loop joins without the pairs or the wobble jumping.
it('renders a whole number of notes at the rate the phone runs at', async () => {
    installBrowser();
    const siren = await loadSiren();

    siren.startSiren('an alarm');

    const [wave] = FakeAudioContext.built[0]?.buffers ?? [];
    if (!wave) throw new Error('the siren built no sound');
    expect(wave.channels).toBe(1);
    expect(wave.sampleRate).toBe(48000);
    expect(wave.length).toBe(Math.round(48000 * 0.19) * 10);
});

// Both ends of the buffer are silent, so the loop joins where there is nothing
// playing to click — and so the alarm does not open with a click either, which
// reads as a fault in the phone rather than the start of the noise it is
// making.
it('starts and ends in silence, so the loop has no seam', async () => {
    installBrowser();
    const siren = await loadSiren();

    siren.startSiren('an alarm');

    const samples = sirenSamples();
    expect(samples[0]).toBe(0);
    expect(Math.abs(samples[samples.length - 1] ?? 1)).toBeLessThan(0.01);
});

// The point of the change this file documents. The phone used to play a square
// wave at gain 1 with a second one at 0.5 on top, which is half again as much
// as a speaker can produce: what came out was the phone's limiter, not the
// alarm. Half scale leaves it somewhere to go.
it('never asks the phone speaker for more than it has', async () => {
    installBrowser();
    const siren = await loadSiren();

    siren.startSiren('an alarm');

    let loudest = 0;
    for (const sample of sirenSamples()) loudest = Math.max(loudest, Math.abs(sample));
    // Loud enough to be an alarm: past the quietest the roughness takes it to.
    expect(loudest).toBeGreaterThan(0.5 * (1 - 0.3));
    expect(loudest).toBeLessThanOrEqual(0.5);
});

// A step in the waveform is a click, and there are ten note changes a second to
// put one at.
it('has no step in it anywhere', async () => {
    installBrowser();
    const siren = await loadSiren();

    siren.startSiren('an alarm');

    const samples = sirenSamples();
    let steepest = 0;
    for (let at = 1; at < samples.length; at++) {
        steepest = Math.max(steepest, Math.abs((samples[at] ?? 0) - (samples[at - 1] ?? 0)));
    }
    // Both notes rising together through a clipper that steepens them. A click
    // is many times this — a whole note's worth of level in one sample.
    expect(steepest).toBeLessThan(0.15);
});

// The two pairs have to actually alternate. Held on one, this is a chime with a
// wrong note in it; alternating, it is a sound that never settles — which is
// the whole reason there are two.
it('alternates between the two pairs of notes', async () => {
    installBrowser();
    const siren = await loadSiren();

    siren.startSiren('an alarm');

    const samples = sirenSamples();
    const note = Math.round(48000 * 0.19);
    const [first = 0, second = 0, third = 0, fourth = 0] = [0, 1, 2, 3].map((n) =>
        crossings(samples.subarray(n * note, (n + 1) * note)),
    );

    // The second pair sits well above the first, so it crosses zero markedly
    // more often.
    expect(second).toBeGreaterThan(first + 20);

    // And the third and fourth notes are those two again. Not to the crossing,
    // because the oscillators run continuously: a note starts wherever the last
    // one left the phase, which shifts a crossing or two either side of the
    // note's edge. Anything further apart than that is a different note.
    expect(Math.abs(third - first)).toBeLessThanOrEqual(4);
    expect(Math.abs(fourth - second)).toBeLessThanOrEqual(4);
});

// crossings counts how often a stretch of sound passes through zero, which
// rises and falls with the pitch of what is playing without this test having to
// know the frequencies.
function crossings(samples: Float32Array): number {
    let count = 0;
    for (let at = 1; at < samples.length; at++) {
        const before = samples[at - 1] ?? 0;
        const now = samples[at] ?? 0;
        if ((before < 0 && now >= 0) || (before >= 0 && now < 0)) count++;
    }
    return count;
}

// sirenSamples is what the siren wrote into the one buffer it built.
function sirenSamples(): Float32Array {
    const wave = FakeAudioContext.built[0]?.buffers[0];
    if (!wave) throw new Error('the siren built no sound');
    return wave.samples;
}

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

// A phone with an audio context it will not give memory to. Two seconds of
// sound is a third of a megabyte, and a phone under memory pressure — which is
// what a phone that has been in a pocket for hours is — can refuse it.
it('carries on silently when the phone will not give it a buffer', async () => {
    installBrowser();
    FakeAudioContext.refusesBuffers = true;
    const siren = await loadSiren();

    expect(() => siren.startSiren('an alarm')).not.toThrow();

    const ctx = FakeAudioContext.built[0];
    if (!ctx) throw new Error('no context was opened');
    expect(ctx.sources).toHaveLength(0);
    // The rest of the alarm carries on regardless.
    expect(navigator.vibrate).toHaveBeenCalled();
});

// Stopping a node the browser has already torn down throws, and the tearing
// down is the browser's business rather than this page's. The title, the
// buzzing and the notification still have to be put away afterwards.
it('finishes putting the alarm away even if the sound refuses to stop', async () => {
    const { doc, nav } = installBrowser();
    const siren = await loadSiren();

    siren.startSiren('a door opened');
    const source = FakeAudioContext.built[0]?.sources[0];
    if (!source) throw new Error('the siren built no sound');
    source.stop = () => {
        throw new Error('the node is already gone');
    };

    expect(() => siren.stopSiren()).not.toThrow();

    expect(doc.title).toBe('LeaveSafe');
    expect(nav.vibrate).toHaveBeenLastCalledWith(0);
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
