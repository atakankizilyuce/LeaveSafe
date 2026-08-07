/*
 * One icon set for the whole panel.
 *
 * Two rules govern everything in here.
 *
 * The first is that an icon rides *beside* a label and never instead of it. The
 * usability literature is unusually firm on this: people remember a button by
 * where it is rather than by what it depicts, most symbols are ambiguous across
 * products, and the change that made Outlook's icon-only toolbar usable again
 * was putting the words back. Icon-only is defensible for close, search and
 * play, and for nothing else on this screen.
 *
 * The second is that a sensor's icon says *what it is* and never what it is
 * doing. What it is doing is a separate mark in the corner — because using
 * colour alone for state fails the one man in twelve with red-green colour
 * blindness, and using the icon for it would mean six shapes to learn per
 * state instead of one.
 *
 * They are drawn here rather than pulled from a library because the binary
 * serves this page over a network with no internet.
 */

/**
 * A primitive in a 24×24 box: a path, a circle, or a rounded rect.
 *
 * Shapes rather than markup so an icon stays a piece of data — a list of these
 * is what a caller composes, reorders and reuses, and none of it needs an
 * escape hatch into raw HTML.
 */
type Shape =
    | { p: string }
    | { c: [cx: number, cy: number, r: number]; solid?: true }
    | { r: [x: number, y: number, w: number, h: number, rx: number] };

function draw(shapes: Shape[]) {
    return shapes.map((s, i) => {
        const key = `s${i}`;
        if ('p' in s) return <path key={key} d={s.p} />;
        if ('c' in s) {
            const [cx, cy, r] = s.c;
            return <circle key={key} cx={cx} cy={cy} r={r} fill={s.solid ? 'currentColor' : undefined} />;
        }
        const [x, y, w, h, rx] = s.r;
        return <rect key={key} x={x} y={y} width={w} height={h} rx={rx} />;
    });
}

/* ── what a button does ────────────────────────────────────────────────── */

const ACTION: Record<string, Shape[]> = {
    /* Arm and disarm are a padlock closing and opening. A shield would say
       "protection" in the abstract; a padlock says which way the switch is
       thrown right now, which is the thing the button changes. */
    lock: [{ r: [3, 11, 18, 11, 2] }, { p: 'M7 11V7a5 5 0 0 1 10 0v4' }],
    unlock: [{ r: [3, 11, 18, 11, 2] }, { p: 'M7 11V7a5 5 0 0 1 9.9-1' }],

    /* Cancel closes nothing and opens nothing — it takes the countdown back. */
    x: [{ p: 'M18 6 6 18' }, { p: 'm6 6 12 12' }],

    /* The field directly above the pair button is labelled "pairing key", so
       the button and the field say the same word twice: once in text, once in
       shape. */
    key: [{ c: [7.5, 15.5, 5.5] }, { p: 'm21 2-9.6 9.6' }, { p: 'm15.5 7.5 3 3L22 7l-3-3' }],
    bluetooth: [{ p: 'm7 7 10 10-5 5V2l5 5L7 17' }],

    /* Dismissing an alarm silences it. A tick would mean "yes, correct", which
       is not what the button does — it does not agree with the alert, it stops
       the noise. */
    bellOff: [
        { p: 'M8.7 3A6 6 0 0 1 18 8a21.3 21.3 0 0 0 .6 5' },
        { p: 'M17 17H3s3-2 3-9a4.67 4.67 0 0 1 .3-1.7' },
        { p: 'M10.3 21a1.94 1.94 0 0 0 3.4 0' },
        { p: 'm2 2 20 20' },
    ],
    pause: [{ r: [6, 4, 4, 16, 1] }, { r: [14, 4, 4, 16, 1] }],
    power: [{ p: 'M12 2v10' }, { p: 'M18.4 6.6a9 9 0 1 1-12.77.04' }],

    check: [{ p: 'M20 6 9 17l-5-5' }],

    /* Forgetting a phone breaks the pairing: a chain with its middle gone. */
    unlink: [{ p: 'M9 17H7A5 5 0 0 1 7 7h2' }, { p: 'M15 7h2a5 5 0 0 1 4 7.9' }, { p: 'm2 2 20 20' }],

    /* Resetting puts the settings back where they were, so the arrow turns
       backwards. The direction is the meaning. */
    undo: [{ p: 'M3 12a9 9 0 1 0 9-9 9.75 9.75 0 0 0-6.74 2.74L3 8' }, { p: 'M3 3v5h5' }],

    /* The core at rest and the core once it is watching. Arming opens the eye:
       the same object, no longer shut. */
    eye: [{ p: 'M2 12s3-7 10-7 10 7 10 7-3 7-10 7-10-7-10-7z' }, { c: [12, 12, 3] }],
    eyeOff: [
        { p: 'M10.7 5.1A11 11 0 0 1 12 5c7 0 10 7 10 7a18 18 0 0 1-2.2 3.2' },
        { p: 'M6.6 6.6A18 18 0 0 0 2 12s3 7 10 7a11 11 0 0 0 5.4-1.4' },
        { p: 'M9.9 4.2 14 14.1' },
        { p: 'm2 2 20 20' },
    ],
};

export type ActionIcon = keyof typeof ACTION;

/** An icon for a button, sized to sit on the same line as its label. */
export function Icon({ name, size = 15 }: { name: string; size?: number }) {
    return (
        <svg
            class="btn-icon"
            aria-hidden="true"
            width={size}
            height={size}
            viewBox="0 0 24 24"
            fill="none"
            stroke="currentColor"
            stroke-width="2"
            stroke-linecap="round"
            stroke-linejoin="round"
        >
            {draw(ACTION[name] ?? [])}
        </svg>
    );
}

/* ── what a sensor is ──────────────────────────────────────────────────── */

const SENSOR: Record<string, Shape[]> = {
    /* The charger, drawn as the plug — the thing that gets pulled out. */
    power: [
        { p: 'M9 2v6' },
        { p: 'M15 2v6' },
        { p: 'M6 8h12v4a6 6 0 0 1-6 6 6 6 0 0 1-6-6z' },
        { p: 'M12 18v4' },
    ],
    /* The lid is the laptop itself, seen from the front. */
    lid: [{ p: 'M4 16V7a2 2 0 0 1 2-2h12a2 2 0 0 1 2 2v9' }, { p: 'M2.5 16h19l-1 3H3.5z' }],
    /* A USB connector, the flat kind everyone has fought with. */
    usb: [
        { c: [6, 18, 2] },
        { p: 'M7.4 16.6 18 6' },
        { p: 'M14 4h6v6' },
        { p: 'M11.5 12.5 15 16l-2 4' },
        { r: [9, 9, 3, 3, 0.5] },
    ],
    screen: [{ r: [2, 4, 20, 12, 2] }, { p: 'M8 20h8' }, { p: 'M12 16v4' }],
    /* Signal arcs rather than a cable: what is being watched is the address
       changing, not a plug moving. */
    network: [
        { p: 'M1.4 8.6a16 16 0 0 1 21.2 0' },
        { p: 'M5 12.3a11 11 0 0 1 14 0' },
        { p: 'M8.5 15.9a6 6 0 0 1 7 0' },
        { c: [12, 19.5, 1.2] },
    ],
    /* Keyboard and trackpad, drawn as the pointer — the thing that moves when
       somebody who is not you touches the machine. */
    input: [{ p: 'M4 3l7 17 2.2-6.8L20 11z' }],
};

/** The mark that says which sensor this is. It never changes with state. */
export function SensorIcon({ name, size = 19 }: { name: string; size?: number }) {
    return (
        <svg
            class="sensor-icon"
            aria-hidden="true"
            width={size}
            height={size}
            viewBox="0 0 24 24"
            fill="none"
            stroke="currentColor"
            stroke-width="1.7"
            stroke-linecap="round"
            stroke-linejoin="round"
        >
            {draw(SENSOR[name] ?? [])}
        </svg>
    );
}

/* ── what a sensor is doing ────────────────────────────────────────────── */

/*
 * Deliberately geometric rather than pictorial: these are read at nine pixels,
 * where a drawing turns to mush. Six silhouettes, all distinguishable with the
 * colour taken out — the triangle for a trip is what safety signage has used
 * for a century, and the diamond for a fault is what an instrument panel uses
 * for a reading it cannot vouch for.
 */
const BADGE: Record<string, Shape[]> = {
    ready: [{ c: [12, 12, 7] }],
    watching: [{ c: [12, 12, 7] }, { c: [12, 12, 2.6], solid: true }],
    tripped: [{ p: 'M12 4 22 20H2z' }],
    fault: [{ p: 'M12 3.5 20.5 12 12 20.5 3.5 12z' }],
    off: [{ c: [12, 12, 7] }, { p: 'm7 17 10-10' }],
    unavailable: [{ p: 'M18 6 6 18' }, { p: 'm6 6 12 12' }],
};

/** The mark in a sensor's corner that says what it is doing. */
export function StateBadge({ state, size = 9 }: { state: string; size?: number }) {
    return (
        <svg
            class="state-badge"
            aria-hidden="true"
            width={size}
            height={size}
            viewBox="0 0 24 24"
            fill="none"
            stroke="currentColor"
            stroke-width="3"
            stroke-linecap="round"
            stroke-linejoin="round"
        >
            {draw(BADGE[state] ?? [])}
        </svg>
    );
}
