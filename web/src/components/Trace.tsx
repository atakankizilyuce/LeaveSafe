import { useEffect, useRef } from 'preact/hooks';
import { armed, link, tripped } from '../lib/store';

const POINTS = 96;
const HEIGHT = 34;

// Stated here rather than read back from the stylesheet. The accent variable is
// mid-transition at exactly the moment the armed state flips, so
// getComputedStyle would sample whatever colour the fade happened to be passing
// through and leave the trace blue on a red page.
const STANDBY = '#6c8cff';
const ARMED = '#ff4a35';
const LOST = '#55637a';

/**
 * What colour the line is drawn in.
 *
 * A lost connection outranks the armed colour, because with the link down the
 * trace is no longer reporting on the laptop — it is only proof the page itself
 * is still running, and drawing that in the alarm colour would say otherwise.
 */
function traceColour(status: typeof link.value, isArmed: boolean): string {
    if (status === 'lost') return LOST;
    return isArmed ? ARMED : STANDBY;
}

/**
 * A baseline that advances on its own: flat while nothing is happening, with a
 * spike each time a sensor trips.
 *
 * It replaces the old "uptime / last check" card, and does that card's job
 * better. A static panel cannot tell you whether it is watching or has quietly
 * frozen; a line that keeps moving can. Flat is the reassuring state, which is
 * the right way round for a security tool — you want boring to look boring.
 */
export function Trace() {
    const canvas = useRef<HTMLCanvasElement>(null);
    const samples = useRef<number[]>(new Array(POINTS).fill(0));
    const frame = useRef<number>();
    const lastTrip = useRef(0);
    const pending = useRef(0);

    // Reading the signals here subscribes the effect, so it restarts when the
    // armed colour or the connection state changes.
    const isArmed = armed.value;
    const status = link.value;
    const trips = tripped.value;

    useEffect(() => {
        const newest = Object.values(trips).reduce((max, at) => Math.max(max, at), 0);
        if (newest > lastTrip.current) {
            lastTrip.current = newest;
            pending.current = 1;
        }
    }, [trips]);

    useEffect(() => {
        const el = canvas.current;
        if (!el) return;

        const reduced = window.matchMedia('(prefers-reduced-motion: reduce)').matches;
        const ctx = el.getContext('2d');
        if (!ctx) return;

        const stroke = traceColour(status, isArmed);

        const dpr = Math.min(window.devicePixelRatio || 1, 2);
        const resize = () => {
            const width = el.clientWidth;
            el.width = width * dpr;
            el.height = HEIGHT * dpr;
            ctx.scale(dpr, dpr);
        };
        resize();

        let phase = 0;

        const draw = () => {
            const width = el.clientWidth;
            ctx.clearRect(0, 0, width, HEIGHT);

            // Advance one sample. Idle noise is deliberately tiny: the line
            // should read as flat, not as activity.
            const idle = isArmed ? 0.06 : 0.03;
            let value = Math.sin(phase * 0.35) * idle + (Math.random() - 0.5) * idle;
            if (pending.current > 0) {
                value = pending.current * (Math.random() > 0.5 ? 1 : -1);
                pending.current *= 0.62;
                if (pending.current < 0.04) pending.current = 0;
            }
            samples.current.push(value);
            samples.current.shift();
            phase += 1;

            ctx.beginPath();
            ctx.lineWidth = 1.5;
            ctx.lineJoin = 'round';
            ctx.strokeStyle = stroke;
            ctx.globalAlpha = status === 'lost' ? 0.4 : 0.85;

            const step = width / (POINTS - 1);
            samples.current.forEach((sample, i) => {
                const x = i * step;
                const y = HEIGHT / 2 - sample * (HEIGHT / 2 - 3);
                if (i === 0) ctx.moveTo(x, y);
                else ctx.lineTo(x, y);
            });
            ctx.stroke();

            if (!reduced) frame.current = requestAnimationFrame(draw);
        };

        draw();
        window.addEventListener('resize', resize);
        return () => {
            if (frame.current) cancelAnimationFrame(frame.current);
            window.removeEventListener('resize', resize);
        };
    }, [isArmed, status]);

    // The canvas is decorative, and a canvas element can take focus — so the
    // wrapper carries aria-hidden rather than the canvas itself, which would
    // hide a focusable node from assistive technology while leaving it in the
    // tab order.
    return (
        <div class="trace-wrap" aria-hidden="true">
            <canvas ref={canvas} class="trace" height={HEIGHT} />
        </div>
    );
}
