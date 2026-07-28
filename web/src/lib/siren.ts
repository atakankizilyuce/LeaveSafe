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
        } catch {
            // Already stopped.
        }
    }
    osc = null;
    harmonic = null;
    if (ctx) {
        void ctx.close().catch(() => {});
        ctx = null;
    }

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
        const AudioCtor =
            window.AudioContext ??
            (window as unknown as { webkitAudioContext?: typeof AudioContext }).webkitAudioContext;
        if (!AudioCtor) return;

        ctx = new AudioCtor();

        osc = ctx.createOscillator();
        const mainGain = ctx.createGain();
        osc.type = 'square';
        osc.frequency.value = 880;
        mainGain.gain.value = 1;
        osc.connect(mainGain).connect(ctx.destination);

        harmonic = ctx.createOscillator();
        const harmonicGain = ctx.createGain();
        harmonic.type = 'square';
        harmonic.frequency.value = 1760;
        harmonicGain.gain.value = 0.5;
        harmonic.connect(harmonicGain).connect(ctx.destination);

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
