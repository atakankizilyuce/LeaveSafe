// The phone's own alarm: a two-tone square wave, plus vibration and a
// notification. Kept apart from the UI so starting and stopping it is one call
// rather than four intervals to remember to clear.

let ctx: AudioContext | null = null;
let osc: OscillatorNode | null = null;
let harmonic: OscillatorNode | null = null;
let warble: number | null = null;
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
    if (warble !== null) {
        window.clearInterval(warble);
        warble = null;
    }
    for (const node of [osc, harmonic]) {
        try {
            node?.stop();
            node?.disconnect();
        } catch {
            // Already stopped.
        }
    }
    osc = null;
    harmonic = null;
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

function startTone() {
    try {
        const audio = audioContext();
        if (!audio) return;

        osc = audio.createOscillator();
        const mainGain = audio.createGain();
        osc.type = 'square';
        osc.frequency.value = 880;
        mainGain.gain.value = 1;
        osc.connect(mainGain).connect(audio.destination);

        harmonic = audio.createOscillator();
        const harmonicGain = audio.createGain();
        harmonic.type = 'square';
        harmonic.frequency.value = 1760;
        harmonicGain.gain.value = 0.5;
        harmonic.connect(harmonicGain).connect(audio.destination);

        osc.start();
        harmonic.start();

        let high = true;
        warble = window.setInterval(() => {
            if (!osc || !harmonic) return;
            osc.frequency.value = high ? 880 : 660;
            harmonic.frequency.value = high ? 1760 : 1320;
            high = !high;
        }, 400);
    } catch {
        // Autoplay policy or no audio device. Vibration and the overlay remain.
    }
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
