// The phone's half of the alarm: the same siren the laptop is making, plus
// vibration, a notification and a flashing title. Kept apart from the UI so
// starting and stopping it is one call rather than four intervals to remember
// to clear.
//
// The phone is the second voice here, not the first. The laptop is the one in
// the room being walked out of, and it is the one that has to be heard by
// whoever is in that room; the phone is in a pocket a few inches from the
// person who needs telling, and what reaches them there is the buzzing as much
// as the noise. So this plays the laptop's siren rather than a warble of its
// own — one alarm with one voice, recognisable as the same thing in both
// places — at a level that leaves the phone's speaker some headroom instead of
// asking it for everything it has.

let ctx: AudioContext | null = null;
let siren: AudioBufferSourceNode | null = null;
let buzz: number | null = null;
let titleFlash: number | null = null;
let titleStop: number | null = null;

const BASE_TITLE = 'LeaveSafe';

/**
 * Open the audio context while the user is touching the screen.
 *
 * A phone will not let a page make noise it did not ask for, so a context
 * created on its own starts suspended and every note played into it is silent.
 * The only reliable moment to open one is inside a real tap — and the tap that
 * matters is Arm, because it is the last one before the phone goes in a pocket
 * and the alarm becomes the only thing that speaks.
 *
 * Safe to call as often as you like; it does nothing once a context is open.
 */
export function primeSiren() {
    audioContext();
}

/**
 * The page's one audio context, opened on demand and then kept.
 *
 * It used to be created for each alarm and closed on dismissal. That is what
 * made the second alarm silent: a phone caps how many contexts a page may open
 * and only lets one start off the back of a gesture, so the replacement made
 * for the second alarm — minutes later, with the phone in a pocket and no
 * gesture in sight — stayed suspended and played nothing. The siren was
 * running; nobody could hear it.
 */
function audioContext(): AudioContext | null {
    if (ctx) {
        // Suspended is the normal state for a context the browser paused while
        // the tab was in the background, and it is silent until resumed.
        if (ctx.state === 'suspended') void ctx.resume().catch(() => {});
        return ctx;
    }
    const AudioCtor =
        window.AudioContext ??
        (window as unknown as { webkitAudioContext?: typeof AudioContext }).webkitAudioContext;
    if (!AudioCtor) return null;
    try {
        ctx = new AudioCtor();
        void ctx.resume().catch(() => {});
        return ctx;
    } catch {
        // No audio device, or the browser refused. Vibration and the overlay
        // remain, and the laptop is sounding its own siren regardless.
        return null;
    }
}

export function startSiren(message: string) {
    stopSiren();
    startTone();
    startVibration();
    startTitleFlash(message);
    notify(message);
}

export function stopSiren() {
    try {
        siren?.stop();
        siren?.disconnect();
    } catch {
        // Already stopped.
    }
    siren = null;
    // The context itself is deliberately left open. See audioContext.

    if (buzz !== null) {
        window.clearInterval(buzz);
        buzz = null;
    }
    navigator.vibrate?.(0);

    if (titleFlash !== null) {
        window.clearInterval(titleFlash);
        titleFlash = null;
    }
    if (titleStop !== null) {
        window.clearTimeout(titleStop);
        titleStop = null;
    }
    document.title = BASE_TITLE;
}

// The siren, which is internal/audio/siren.go written out again in a language
// with a different way of reaching a speaker. Every number below is the same
// number as there, and the reason for each of them is written there too: two
// pairs of notes a tritone apart, alternating five times a second, roughened at
// twenty hertz and driven into a soft clipper so it has an edge on it.
//
// It is deliberately not a sweep. A tone gliding up and down is what every
// emergency vehicle in the world does, so a phone making that sound reads as an
// ambulance somewhere outside rather than as this alarm, now.
const LOW_A = 622;
const HIGH_A = 880;
const LOW_B = 740;
const HIGH_B = 1046;
const NOTE_MS = 190;
const EDGE_MS = 4;
const ROUGH_HZ = 20;
const ROUGH_DEPTH = 0.3;
const DRIVE = 1.8;

// The one number that differs from the laptop's, which is at 0.9. A phone
// speaker asked for everything it has answers with distortion rather than
// volume, and this one is a few inches from the person it is talking to.
const AMPLITUDE = 0.5;

// LOOP_NOTES is how much siren is rendered before it repeats. Ten notes is 1.9
// seconds, and that length is not arbitrary: it is a whole number of notes, so
// the two pairs still alternate across the join, and a whole number of 20 Hz
// roughness cycles, so the wobble does not jump either. The join itself lands
// in the silence at the end of a note, where nothing is playing to click.
const LOOP_NOTES = 10;

function startTone() {
    try {
        const audio = audioContext();
        if (!audio) return;

        const rate = audio.sampleRate;
        const note = Math.round((rate * NOTE_MS) / 1000);
        const wave = audio.createBuffer(1, note * LOOP_NOTES, rate);
        renderSiren(wave.getChannelData(0), rate, note);

        siren = audio.createBufferSource();
        siren.buffer = wave;
        // Looping in the audio thread rather than on a timer. A setInterval on
        // a phone with the screen off is throttled to once a minute or stopped
        // outright, which would have made the alarm a sound that played for two
        // seconds and then waited — the one moment nobody is looking at the
        // page is the one moment it has to keep going.
        siren.loop = true;
        siren.connect(audio.destination);
        siren.start();
    } catch {
        // Autoplay policy or no audio device. Vibration and the overlay remain,
        // and the laptop is sounding its own siren regardless.
    }
}

/** Write one loop of the siren into a buffer of samples. */
function renderSiren(out: Float32Array, rate: number, note: number) {
    const edge = Math.round((rate * EDGE_MS) / 1000);
    let phaseLow = 0;
    let phaseHigh = 0;

    for (let at = 0; at < out.length; at++) {
        // The oscillators run continuously, whether or not the note they are
        // playing is the one being heard. Restarting them at each change would
        // put a step in the waveform at every note edge, and a step is a click.
        const [low, high] = notes(Math.floor(at / note));
        phaseLow += (2 * Math.PI * low) / rate;
        phaseHigh += (2 * Math.PI * high) / rate;

        const mix = (Math.sin(phaseLow) + Math.sin(phaseHigh)) / 2;
        out[at] = softClip(mix) * AMPLITUDE * ramp(at % note, note, edge) * rough(at, rate);
    }
}

/** The pair being played, which alternates every note. */
function notes(index: number): [number, number] {
    return index % 2 === 1 ? [LOW_B, HIGH_B] : [LOW_A, HIGH_A];
}

/** A linear fade at both ends of a note, so a note starting is not a click. */
function ramp(at: number, length: number, over: number): number {
    if (at < over) return at / over;
    if (at > length - over) return (length - at) / over;
    return 1;
}

/** The amplitude wobble that makes it harsh rather than merely loud. */
function rough(at: number, rate: number): number {
    const phase = (2 * Math.PI * ROUGH_HZ * at) / rate;
    return 1 - (ROUGH_DEPTH * (1 - Math.cos(phase))) / 2;
}

/** Pushes the waveform towards a square without ever passing full scale. */
function softClip(level: number): number {
    return Math.tanh(DRIVE * level) / Math.tanh(DRIVE);
}

function startVibration() {
    if (!navigator.vibrate) return;
    const pattern = [500, 200, 500, 200, 500];
    navigator.vibrate(pattern);
    buzz = window.setInterval(() => navigator.vibrate?.(pattern), 2000);
}

function startTitleFlash(message: string) {
    let on = true;
    titleFlash = window.setInterval(() => {
        document.title = on ? `ALERT — ${message}` : BASE_TITLE;
        on = !on;
    }, 500);
    titleStop = window.setTimeout(() => {
        if (titleFlash !== null) window.clearInterval(titleFlash);
        titleFlash = null;
        document.title = BASE_TITLE;
    }, 30000);
}

function notify(message: string) {
    const worker = navigator.serviceWorker?.controller;
    if (worker) {
        worker.postMessage({ type: 'alarm', message });
        return;
    }
    if (!('Notification' in window)) return;
    if (Notification.permission === 'granted') {
        new Notification('LeaveSafe alert', {
            body: message,
            tag: 'leavesafe-alert',
            requireInteraction: true,
        });
    } else if (Notification.permission !== 'denied') {
        void Notification.requestPermission();
    }
}

export function warnDisconnected() {
    navigator.vibrate?.([300, 100, 300, 100, 300]);
    if ('Notification' in window && Notification.permission === 'granted') {
        new Notification('LeaveSafe', {
            body: 'Connection to your laptop was lost.',
            tag: 'leavesafe-disconnect',
        });
    }
}
