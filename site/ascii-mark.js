/*
 * The arithmetic behind the opening mark.
 *
 * Everything here is a number in and a number out: no canvas, no document, no
 * clock. The painting and the scrolling live in intro.js, which is where the
 * browser is allowed to exist. Split this way because a canvas cannot be
 * exercised under jsdom, and a page whose only moving part is untestable is a
 * page nobody can change with confidence.
 *
 * The mark itself is not a picture file. The shield is the same path the panel
 * draws (viewBox 120x140) and the eye is the same one the sensor list uses
 * (viewBox 24), so what the mosaic samples is the product's own logo rather
 * than a redrawing of it that drifts the first time either is touched.
 */

export const SHIELD_D = 'M60 4 112 24v52c0 34-30 52-52 60C38 128 8 110 8 76V24Z';
export const EYE_D = 'M2 12s3.6-6.5 10-6.5S22 12 22 12s-3.6 6.5-10 6.5S2 12 2 12Z';

/* The tile is not black. The mosaic needs something to say outside the shield,
   or the mark hangs in a void with no grid around it. */
export const TILE_BG = '#0e1621';
export const GROUND = '#06080c';
export const STROKE = '#6aa8d4';
export const SHIELD_FILL = '#0d1622';
export const TINT = '#3d8fc4';

/* Contrast turns about this point rather than the usual 0.5. The picture's
   weight is in the dark end, and pivoting at the middle crushes the tile's
   texture and the shield's interior together into nothing. */
export const PIVOT = 0.3;

export const TAU = Math.PI * 2;

export const clamp = (v, lo, hi) => Math.min(Math.max(v, lo), hi);

/*
 * A cell's own constant randomness, from its index. The coverage gaps and the
 * shimmer phases have to be the same on every frame; drawn from Math.random
 * they move each frame and the mark boils.
 */
export function hash01(i) {
    /* Unsigned, deliberately. The golden-ratio constant is above 2^31, so `| 0`
       reads it back as a negative number — the same thirty-two bits, but a
       value the arithmetic around it does not claim to be working in. */
    let h = (i + 0x9e3779b9) >>> 0;
    h = Math.imul(h ^ (h >>> 16), 0x21f0aaad);
    h = Math.imul(h ^ (h >>> 15), 0x735a2d97);
    return ((h ^ (h >>> 15)) >>> 0) / 4294967296;
}

/*
 * Luminance gradient per cell. This is what keeps the shield's outline an
 * outline: with the edges left flat the mosaic reads as a smudge in the shape
 * of a shield rather than as the shield.
 */
export function sobel(lum, cols, rows) {
    const out = new Float32Array(cols * rows);
    const at = (x, y) => lum[clamp(y, 0, rows - 1) * cols + clamp(x, 0, cols - 1)];
    for (let y = 0; y < rows; y++) {
        for (let x = 0; x < cols; x++) {
            const gx =
                -at(x - 1, y - 1) -
                2 * at(x - 1, y) -
                at(x - 1, y + 1) +
                at(x + 1, y - 1) +
                2 * at(x + 1, y) +
                at(x + 1, y + 1);
            const gy =
                -at(x - 1, y - 1) -
                2 * at(x, y - 1) -
                at(x + 1, y - 1) +
                at(x - 1, y + 1) +
                2 * at(x, y + 1) +
                at(x + 1, y + 1);
            out[y * cols + x] = Math.min(1, Math.hypot(gx, gy) / 2.2);
        }
    }
    return out;
}

export const luminance = (r, g, b) => (0.2126 * r + 0.7152 * g + 0.0722 * b) / 255;

/* Edge emphasis first, then contrast about PIVOT. */
export function tone(lum, edge, edgeAmt, contrast) {
    const v = lum + edge * edgeAmt * 0.9;
    return clamp((v - PIVOT) * contrast + PIVOT, 0, 1);
}

export const easeOutCubic = (x) => 1 - (1 - clamp(x, 0, 1)) ** 3;
export const easeInOutCubic = (x) => {
    const t = clamp(x, 0, 1);
    return t < 0.5 ? 4 * t * t * t : 1 - (-2 * t + 2) ** 3 / 2;
};

export function smoothstep(a, b, x) {
    if (a === b) return x < a ? 0 : 1;
    const t = clamp((x - a) / (b - a), 0, 1);
    return t * t * (3 - 2 * t);
}

/*
 * How much a cell's brightness is multiplied by, this instant.
 *
 * `flow` is the one the page uses: a ring travelling outward from the eye, with
 * a low shimmer laid over it so neighbouring cells never move in lockstep. The
 * brief given for this was "smoother", and the flicker the reference preset
 * ships with is the opposite of smooth — it is kept here only so the two can be
 * put side by side.
 */
export function animMod(style, o) {
    const { i, t, speed, amp, d, nx, ny, phase, h } = o;
    const ripple = Math.sin(TAU * t * speed * 0.55 - d * 13);
    const shim = Math.sin(TAU * t * speed * 0.9 + phase);
    let m = 1;
    switch (style) {
        case 'flow': {
            m += amp * (0.42 * ripple + 0.24 * shim);
            /* What is left of the reference preset's flicker: a rare one-frame
               spark, at a rate that reads as life rather than as noise. */
            const spark = Math.floor(t * 9 + h * 613);
            if (hash01(i * 17 + spark) > 0.994) m += 0.9 * amp;
            break;
        }
        case 'ripple':
            m += amp * 0.62 * ripple;
            break;
        case 'wave':
            m += amp * 0.58 * Math.sin(TAU * t * speed * 0.5 - (nx + ny) * 7);
            break;
        case 'shimmer':
            m += amp * 0.52 * shim;
            break;
        case 'pulse':
            m += amp * 0.45 * Math.sin(TAU * t * speed * 0.42);
            break;
        case 'flicker': {
            const step = Math.floor(t * speed * 14 + h * 97);
            m += amp * (hash01(i * 31 + step) - 0.45) * 1.5;
            break;
        }
        default:
            break;
    }
    return m;
}

/*
 * The opening: the mark surfaces from below.
 *
 * `ny` is 0 at the top of the mark and 1 at its bottom. The front starts under
 * the tile and travels up past its top edge, and the feather is what stops it
 * from looking like a window blind.
 */
export const VEIL_FEATHER = 0.3;

export function introVeil(progress, ny) {
    const p = clamp(progress, 0, 1);
    if (p >= 1) return 1;
    const front = 1 - p * (1 + VEIL_FEATHER);
    return clamp((ny - front) / VEIL_FEATHER, 0, 1);
}

/*
 * How much of a silhouette the mark still is. It arrives as a dark shape and
 * takes its colour only once it has finished arriving, which is the difference
 * between a logo that appears and a logo that switches on.
 */
export function silhouette(progress) {
    return 1 - easeOutCubic((clamp(progress, 0, 1) - 0.42) / 0.58);
}

/* Scroll position to a 0..1 reading of how far through the gate we are. */
export function gateProgress(scrollY, runway) {
    /* A gate shorter than the window has no runway to be partway through, and
       a runway that is not a number would divide the whole handover into NaN. */
    if (!Number.isFinite(runway) || runway <= 0) return 0;
    return clamp(scrollY / runway, 0, 1);
}

/*
 * The handover, as a 0..1 reading of its own.
 *
 * It starts a little after the first touch of the wheel — the mark should not
 * begin coming apart the instant a visitor breathes on the trackpad — and it
 * finishes before the runway does, so the page has a moment of clear air to
 * arrive into rather than racing the last cell out of frame.
 */
export function handover(p) {
    return clamp((clamp(p, 0, 1) - 0.08) / 0.78, 0, 1);
}

/*
 * The page coming up through the last of the cells.
 *
 * It used to wait for an observer to notice it had scrolled into view, which
 * put a screen of black between the mark leaving and the hero arriving — two
 * events with a pause in the middle instead of one handover. Driven off the
 * same scroll reading as the dispersal, the two are the same movement.
 */
export function heroRise(p, stagger = 0) {
    return smoothstep(0.38 + stagger, 0.86 + stagger, clamp(p, 0, 1));
}

/* The name and the invitation leave early and quickly. They are a caption, and
   a caption should not outlive what it captions. */
export function textFade(p) {
    return 1 - smoothstep(0.04, 0.28, clamp(p, 0, 1));
}

/*
 * How far one cell is through leaving, given how far the handover is and how
 * far that cell sits from the centre.
 *
 * The mark is made of cells, so it comes apart as cells: the outside goes
 * first and the middle goes last, which leaves the eye alone on the screen for
 * a moment before it goes too. Cubed, because something thrown does not travel
 * at a constant speed.
 */
export function dispersalLead(q, dist) {
    const lead = (clamp(q, 0, 1) - (1 - clamp(dist, 0, 1)) * 0.38) / 0.62;
    const t = clamp(lead, 0, 1);
    return t * t * t;
}

/*
 * One channel through an overlay blend, mixed back by `amount`.
 *
 * The tint used to be a rectangle laid over the whole canvas, which needed
 * something opaque underneath it and so needed the mark to sit on a plate of
 * its own colour. Per cell, it needs nothing underneath: the canvas can be
 * transparent and the page's own background is what shows between the cells,
 * which is the only way the two are ever exactly the same colour.
 */
export function overlayMix(base, blend, amount) {
    const b = clamp(base, 0, 1);
    const s = clamp(blend, 0, 1);
    const o = b < 0.5 ? 2 * b * s : 1 - 2 * (1 - b) * (1 - s);
    return clamp(b + (o - b) * clamp(amount, 0, 1), 0, 1);
}

/*
 * The grid around the shield, dissolving into the page.
 *
 * Without this the faint cells outside the mark stop dead at the edge of the
 * sampled square and draw a box nobody asked for — and they are not as faint as
 * they look: a contrast below 100 lifts the blacks, so the empty field carries
 * real cells rather than nothing.
 *
 * What is spared is decided by brightness, because the mark is the bright part.
 * The shield's top corners sit around 0.93 out, far enough that a falloff
 * applied by distance alone would eat them.
 *
 * `v` must be the cell's resting brightness, before the animation has had its
 * way with it. Taken after, a cell on the shield drops below the threshold
 * every time the wave troughs, and the outline blinks in and out of the
 * falloff instead of staying put.
 */
export function gridFalloff(rn, v) {
    const mark = smoothstep(0.22, 0.42, v);
    const dim = smoothstep(0.38, 0.95, rn) * (1 - mark);
    return 1 - dim;
}
