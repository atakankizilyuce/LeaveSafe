/*
 * The opening: the mark, and everything that has to happen before the page
 * proper takes over.
 *
 * The page begins as one thing — the LeaveSafe shield, surfacing from below the
 * fold as a mosaic of cells — and hands over to the hero as you scroll. Three
 * jobs live here and nothing else does:
 *
 *   1. draw the mark   (a canvas, sampled from the logo's own two paths)
 *   2. run the opening (a reveal that travels upward, once, on load)
 *   3. hand over       (the mark comes apart as cells; the page rises through)
 *
 * The canvas has no background of its own. An earlier version painted the mark
 * on a tile a shade lighter than the page, which is exactly as obvious as it
 * sounds: a rounded square hanging in front of the site. Everything is drawn
 * per cell now — the tint included — so what sits between the cells is the
 * page's own background, and the two cannot disagree.
 *
 * The arithmetic all of this stands on is in ascii-mark.js, which is testable.
 * A canvas is not: jsdom has no 2d context, so a test here could only assert
 * that nothing threw. Rather than write that test and call it coverage, this
 * file is excluded from the coverage report and the numbers it depends on are
 * covered next door.
 */

import {
    animMod,
    clamp,
    dispersalLead,
    EYE_D,
    gateProgress,
    gridFalloff,
    handover,
    hash01,
    heroRise,
    introVeil,
    luminance,
    overlayMix,
    SHIELD_D,
    SHIELD_FILL,
    STROKE,
    silhouette,
    smoothstep,
    sobel,
    TAU,
    TILE_BG,
    TINT,
    textFade,
    tone,
} from './ascii-mark.js';

/*
 * The preset. These are the numbers the mark was tuned at, not defaults with a
 * dial hidden somewhere: 60 cells across is fine enough for the eye to survive,
 * a contrast under 100 keeps the grid visible around the shield, and the glitch
 * the reference preset ships with is off because a mark that tears is a mark
 * that looks broken on the one screen a visitor sees first.
 */
const P = {
    grid: 60,
    coverage: 1,
    edgeEmphasis: 0.11,
    style: 'flow',
    speed: 1.36,
    amp: 0.8,
    tint: 0.5,
    contrast: 0.79,
    bloom: true,
    scan: true,
    chromatic: true,
    grain: true,
};

const INTRO_MS = 2100;

/*
 * How much of the canvas the mark itself occupies. The rest is the room the
 * cells need to leave through — clipped at the canvas edge they would not
 * disperse, they would hit a wall.
 */
const MARK_BOX = 0.72;

/* Tint, once, as three numbers between 0 and 1. */
const TINT_RGB = [
    Number.parseInt(TINT.slice(1, 3), 16) / 255,
    Number.parseInt(TINT.slice(3, 5), 16) / 255,
    Number.parseInt(TINT.slice(5, 7), 16) / 255,
];

const el = (t) => document.createElement(t);

function makeCanvas(w, h) {
    const c = el('canvas');
    c.width = w;
    c.height = h;
    return c;
}

/* roundRect is recent enough to still be worth a floor under it. */
function roundRectPath(ctx, x, y, w, h, r) {
    if (ctx.roundRect) {
        ctx.beginPath();
        ctx.roundRect(x, y, w, h, r);
        return;
    }
    const rr = Math.min(r, w / 2, h / 2);
    ctx.beginPath();
    ctx.moveTo(x + rr, y);
    ctx.arcTo(x + w, y, x + w, y + h, rr);
    ctx.arcTo(x + w, y + h, x, y + h, rr);
    ctx.arcTo(x, y + h, x, y, rr);
    ctx.arcTo(x, y, x + w, y, rr);
    ctx.closePath();
}

/* ── the source layer ────────────────────────────────────────────────────
   Drawn once per size. Redrawing the logo every frame would be redrawing
   something that never changes sixty times a second; only the mosaic on top
   of it moves.
   ──────────────────────────────────────────────────────────────────────── */
function paintSource(ctx, M) {
    const shield = new Path2D(SHIELD_D);
    const eye = new Path2D(EYE_D);

    /* A flat field rather than a rounded tile: the corners are taken off by the
       falloff at draw time, which dissolves rather than cuts. */
    ctx.clearRect(0, 0, M, M);
    ctx.fillStyle = TILE_BG;
    ctx.fillRect(0, 0, M, M);

    const sw = M * 0.615;
    const sc = sw / 120;
    const ox = (M - sw) / 2;
    const oy = (M - 140 * sc) / 2;

    ctx.save();
    ctx.translate(ox, oy);
    ctx.scale(sc, sc);
    ctx.fillStyle = SHIELD_FILL;
    ctx.fill(shield);
    ctx.lineWidth = 8;
    ctx.lineJoin = 'round';
    ctx.lineCap = 'round';
    ctx.strokeStyle = STROKE;
    ctx.stroke(shield);
    ctx.restore();

    /* The eye sits at the shield's visual centre, (60, 62) in its own space —
       not the box's centre, which is lower and reads as a droop. */
    const es = (M * 0.3) / 20;
    ctx.save();
    ctx.translate(M / 2, oy + 62 * sc);
    ctx.scale(es, es);
    ctx.translate(-12, -12);
    ctx.lineWidth = 2.1;
    ctx.lineJoin = 'round';
    ctx.lineCap = 'round';
    ctx.strokeStyle = STROKE;
    ctx.stroke(eye);
    ctx.beginPath();
    ctx.arc(12, 12, 2.6, 0, TAU);
    ctx.stroke();
    ctx.beginPath();
    ctx.arc(12, 12, 1.15, 0, TAU);
    ctx.fillStyle = STROKE;
    ctx.fill();
    ctx.restore();
}

/*
 * Per-cell averages, taken by drawing the source down to one pixel per cell.
 * The browser's own box filter is both faster and more correct than summing
 * pixels by hand.
 */
function sampleGrid(src, n) {
    const tiny = makeCanvas(n, n);
    const t = tiny.getContext('2d', { willReadFrequently: true });
    t.imageSmoothingEnabled = true;
    t.imageSmoothingQuality = 'high';
    t.drawImage(src, 0, 0, src.width, src.height, 0, 0, n, n);
    const d = t.getImageData(0, 0, n, n).data;

    const total = n * n;
    const r = new Float32Array(total);
    const g = new Float32Array(total);
    const b = new Float32Array(total);
    const lum = new Float32Array(total);
    for (let i = 0; i < total; i++) {
        r[i] = d[i * 4];
        g[i] = d[i * 4 + 1];
        b[i] = d[i * 4 + 2];
        lum[i] = luminance(r[i], g[i], b[i]);
    }
    return { r, g, b, lum, edge: sobel(lum, n, n) };
}

/* Grain generated per frame is grain generated 60 times a second. Four tiles,
   cycled, are indistinguishable and free. */
function noiseTiles() {
    const tiles = [];
    for (let k = 0; k < 4; k++) {
        const c = makeCanvas(128, 128);
        const x = c.getContext('2d');
        const img = x.createImageData(128, 128);
        for (let i = 0; i < 128 * 128; i++) {
            const v = 110 + Math.random() * 145;
            img.data[i * 4] = v;
            img.data[i * 4 + 1] = v;
            img.data[i * 4 + 2] = v;
            img.data[i * 4 + 3] = 255;
        }
        x.putImageData(img, 0, 0);
        tiles.push(c);
    }
    return tiles;
}

function scanTile() {
    const c = makeCanvas(1, 3);
    const x = c.getContext('2d');
    x.fillStyle = '#000';
    x.fillRect(0, 2, 1, 1);
    return c;
}

/* One channel of the picture, kept and the rest thrown away: the two legs of
   the chromatic split. The `destination-in` at the end is what keeps the fill
   from painting the transparent space between cells. Done at half size — the
   offset is a few pixels and nobody has ever seen the difference. */
function channel(ctx, src, w, colour) {
    ctx.globalCompositeOperation = 'source-over';
    ctx.clearRect(0, 0, w, w);
    ctx.drawImage(src, 0, 0, src.width, src.height, 0, 0, w, w);
    ctx.globalCompositeOperation = 'multiply';
    ctx.fillStyle = colour;
    ctx.fillRect(0, 0, w, w);
    ctx.globalCompositeOperation = 'destination-in';
    ctx.drawImage(src, 0, 0, src.width, src.height, 0, 0, w, w);
    ctx.globalCompositeOperation = 'source-over';
}

/* ────────────────────────────────────────────────────────────────────────
   The mark.
   ──────────────────────────────────────────────────────────────────────── */
function createMark(canvas) {
    const out = canvas.getContext('2d');
    if (!out) return null;

    const noise = noiseTiles();
    const scan = scanTile();

    let S = 0;
    let half = 0;
    let M = 0;
    let pad = 0;
    let cell = 0;
    let src = null;
    let fx = null;
    let fxCtx = null;
    let aux = null;
    let auxCtx = null;
    let cells = null;

    function resize(cssSize) {
        const dpr = Math.min(window.devicePixelRatio || 1, 1.4);
        const next = Math.max(120, Math.round(cssSize * dpr));
        if (next === S) return pad / dpr;
        S = next;
        half = Math.max(48, Math.round(S / 2));
        M = Math.round(S * MARK_BOX);
        pad = (S - M) / 2;
        cell = M / P.grid;

        canvas.width = S;
        canvas.height = S;
        canvas.style.width = `${cssSize}px`;
        canvas.style.height = `${cssSize}px`;

        src = makeCanvas(M, M);
        fx = makeCanvas(S, S);
        fxCtx = fx.getContext('2d');
        aux = makeCanvas(half, half);
        auxCtx = aux.getContext('2d');

        paintSource(src.getContext('2d'), M);
        cells = sampleGrid(src, P.grid);
        return pad / dpr;
    }

    /*
     * t      seconds since the first frame
     * intro  0..1, the opening reveal
     * q      0..1, the handover — 1 means the mark has left
     */
    function draw(t, intro, q, moving) {
        if (!cells) return;

        const sil = silhouette(intro);
        const tintAmt = P.tint * (1 - sil * 0.85);
        const n = P.grid;

        fxCtx.clearRect(0, 0, S, S);

        for (let y = 0; y < n; y++) {
            for (let x = 0; x < n; x++) {
                const i = y * n + x;
                const h = hash01(i);
                if (h > P.coverage) continue;

                const ny = (y + 0.5) / n;
                const veil = introVeil(intro, ny);
                if (veil <= 0) continue;

                const rest = tone(cells.lum[i], cells.edge[i], P.edgeEmphasis, P.contrast);
                if (rest < 0.012) continue;

                const cx = (x + 0.5) / n - 0.5;
                const cy = ny - 0.5;
                const rn = Math.hypot(cx, cy) * 2;

                let v = rest;
                if (moving) {
                    v *= animMod(P.style, {
                        i,
                        t,
                        speed: P.speed,
                        amp: P.amp,
                        d: Math.hypot(cx, cy - 0.02),
                        nx: cx,
                        ny: cy,
                        phase: hash01(i + 7777) * TAU,
                        h,
                    });
                }

                const vv = clamp(v * veil, 0, 1.25);
                if (vv < 0.015) continue;

                let a = clamp(0.3 + vv * 0.7, 0, 1) * gridFalloff(rn, rest);
                if (a < 0.01) continue;

                let w = cell * clamp(0.22 + 0.84 * vv, 0, 1);
                let px = pad + x * cell;
                let py = pad + y * cell;

                /* Leaving. Each cell is thrown outward from the centre with a
                   turn of its own, so the mark comes apart rather than sliding
                   away in one piece. */
                if (q > 0) {
                    const e = dispersalLead(q, rn / Math.SQRT2);
                    if (e >= 1) continue;
                    const away = hash01(i + 313) * TAU;
                    const reach = e * S * 0.42;
                    const r = rn || 0.001;
                    px += ((cx / r) * 0.75 + Math.cos(away) * 0.45) * reach;
                    py += ((cy / r) * 0.75 + Math.sin(away) * 0.45) * reach - e * S * 0.1;
                    /* A cell holds its light most of the way out and then goes
                       quickly, with a lift as it breaks away. Faded in a
                       straight line the mark stops being a mark almost at once,
                       and what is left is a grey drizzle. */
                    a *= (1 - e ** 1.7) * (1 + 0.4 * Math.sin(Math.PI * e));
                    w *= 1 - e * 0.35;
                    if (a < 0.01) continue;
                }

                const off = (cell - w) / 2;
                const k = clamp(0.7 + 0.45 * vv, 0, 1.3);

                let cr = (cells.r[i] * k) / 255;
                let cg = (cells.g[i] * k) / 255;
                let cb = (cells.b[i] * k * 1.06) / 255;

                /* On the way in the cells are a shape, not a colour: each is
                   pulled towards its own grey until the reveal is done. */
                if (sil > 0) {
                    const grey = ((cr + cg + cb) / 3) * 0.55;
                    cr += (grey - cr) * sil;
                    cg += (grey - cg) * sil;
                    cb += (grey - cb) * sil;
                }

                cr = overlayMix(cr, TINT_RGB[0], tintAmt);
                cg = overlayMix(cg, TINT_RGB[1], tintAmt);
                cb = overlayMix(cb, TINT_RGB[2], tintAmt);

                fxCtx.fillStyle = `rgba(${Math.round(cr * 255)},${Math.round(cg * 255)},${Math.round(cb * 255)},${a.toFixed(3)})`;
                roundRectPath(fxCtx, px + off, py + off, w, w, Math.min(w * 0.34, cell * 0.2));
                fxCtx.fill();
            }
        }

        out.clearRect(0, 0, S, S);
        out.save();
        out.drawImage(fx, 0, 0);

        if (P.chromatic) {
            const d = Math.max(1, S * 0.003);
            out.globalCompositeOperation = 'lighter';
            out.globalAlpha = 0.3;
            channel(auxCtx, fx, half, '#ff2200');
            out.drawImage(aux, -d, 0, S, S);
            channel(auxCtx, fx, half, '#0044ff');
            out.drawImage(aux, d, 0, S, S);
            out.globalCompositeOperation = 'source-over';
        }

        if (P.bloom) {
            auxCtx.globalCompositeOperation = 'source-over';
            auxCtx.clearRect(0, 0, half, half);
            auxCtx.filter = `blur(${(half * 0.022).toFixed(2)}px)`;
            auxCtx.drawImage(fx, 0, 0, S, S, 0, 0, half, half);
            auxCtx.filter = 'none';
            out.globalCompositeOperation = 'lighter';
            out.globalAlpha = 0.38;
            out.drawImage(aux, 0, 0, S, S);
            out.globalCompositeOperation = 'source-over';
        }

        /* Both of the last two work on what is already there and never on the
           space around it: scan lines take a bite out of the cells, and the
           grain sits on top of them. Painted flat they would be lines and
           noise across the page itself. */
        if (P.scan) {
            out.globalCompositeOperation = 'destination-out';
            out.globalAlpha = 0.26;
            out.fillStyle = out.createPattern(scan, 'repeat');
            out.fillRect(0, 0, S, S);
        }

        if (P.grain) {
            const tile = noise[Math.floor(t * 11) % noise.length];
            out.globalCompositeOperation = 'source-atop';
            out.globalAlpha = 0.13;
            for (let gy = 0; gy < S; gy += 128) {
                for (let gx = 0; gx < S; gx += 128) out.drawImage(tile, gx, gy);
            }
        }

        out.restore();
    }

    return { resize, draw };
}

/* ────────────────────────────────────────────────────────────────────────
   The opening, and the handover.
   ──────────────────────────────────────────────────────────────────────── */
export function initIntro() {
    const gate = document.querySelector('.gate');
    const canvas = document.getElementById('gate-mark');
    if (!gate || !canvas || !canvas.getContext) return;

    const mark = createMark(canvas);
    if (!mark) return;

    const root = document.documentElement;
    const stage = gate.querySelector('.gate-stage');
    const reduced = window.matchMedia('(prefers-reduced-motion: reduce)').matches;

    /* Nothing above shows the gate until it is known to work — with no script
       and no canvas the page is simply the page, which is the right fallback
       and not a degraded one. */
    root.dataset.intro = reduced ? 'still' : 'on';

    const sizeMark = () => {
        const vw = window.innerWidth;
        const vh = window.innerHeight;
        /* The canvas is bigger than the mark drawn inside it, so the padding it
           carries has to come back out of the gap under it. */
        const size = Math.round(Math.min(vw * 0.94, vh * 0.72, 660));
        const padding = mark.resize(size);
        stage.style.setProperty('--mark-pad', `${padding.toFixed(1)}px`);
    };
    sizeMark();
    window.addEventListener('resize', sizeMark);

    let t0 = 0;
    let raf = 0;
    let lastQ = -1;

    const wake = () => {
        if (!raf) raf = requestAnimationFrame(frame);
    };
    const sleep = () => {
        cancelAnimationFrame(raf);
        raf = 0;
    };

    function frame(ms) {
        raf = 0;
        if (!t0) t0 = ms;
        const t = (ms - t0) / 1000;
        const intro = reduced ? 1 : Math.min(1, (ms - t0) / INTRO_MS);

        /* One runway: how far you have to scroll before the mark is gone. The
           stage is stuck to the top across it, so the mark comes apart where it
           stands rather than sliding off. */
        const runway = Math.max(1, gate.offsetHeight - window.innerHeight);
        const p = gateProgress(window.scrollY || window.pageYOffset || 0, runway);
        const q = handover(p);
        const moved = Math.abs(q - lastQ) > 0.002;

        if (moved) {
            lastQ = q;
            stage.style.setProperty('--gate-text', textFade(p).toFixed(3));
            stage.style.setProperty('--gate-lift', `${(-p * 9).toFixed(2)}vh`);
            /* The hero rises off the same reading the mark comes apart on. Two
               numbers rather than one so the panel follows the words instead of
               landing beside them. */
            root.style.setProperty('--hero-in', heroRise(p).toFixed(3));
            root.style.setProperty('--hero-in-late', heroRise(p, 0.08).toFixed(3));
            root.dataset.gatePassed = p > 0.34 ? 'yes' : 'no';
        }

        /* Once the last cell is out there is nothing on screen to draw, and
           nothing to keep a frame loop running for either: the page below is
           HTML, and holding the compositor awake for it would cost a laptop
           battery all the way down a page the mark left minutes ago. Scrolling
           back wakes it. */
        if (q >= 1) {
            if (moved) mark.draw(t, intro, q, !reduced);
            return;
        }

        /* Asked not to animate, the mark is a still picture. It is redrawn when
           the scroll moves it and not otherwise, and the loop stops in between
           rather than spinning over a frame that would come out identical. */
        const still = reduced && intro >= 1;
        if (moved || !still) mark.draw(t, intro, q, !reduced);
        if (!still) wake();
    }

    wake();
    window.addEventListener('scroll', wake, { passive: true });
    window.addEventListener('resize', wake);

    document.addEventListener('visibilitychange', () => {
        if (document.hidden) sleep();
        else wake();
    });
}

/*
 * Everything below the gate arrives the way the mark does: from underneath,
 * once, as it comes into view.
 *
 * What moves is listed here rather than marked up in the page. Twenty
 * `data-reveal` attributes scattered through the HTML say nothing about the
 * sequence they belong to; this list is the sequence. Rows stagger, headings do
 * not — six cards landing together reads as a repaint rather than an arrival.
 */
const REVEAL = [
    /* The hero is not here. It arrives on the gate's own scroll reading, in the
       same movement that takes the mark apart, rather than on an observer that
       fires once the mark has already gone. */
    { sel: '.band > .sec-title', stagger: false },
    { sel: '.steps > li', stagger: true },
    { sel: '.shot', stagger: false },
    { sel: '.sensors > li', stagger: true },
    { sel: '.caveat', stagger: false },
    { sel: '.tabs', stagger: false },
];

export function initReveals() {
    const items = [];
    for (const group of REVEAL) {
        const found = Array.from(document.querySelectorAll(group.sel));
        found.forEach((node, n) => {
            node.dataset.reveal = 'out';
            node.dataset.revealStep = group.stagger ? String(n) : '0';
            items.push(node);
        });
    }
    if (!items.length) return;

    /* Only now does the stylesheet get permission to hide any of it: with no
       script the page must not be a column of invisible sections. */
    document.documentElement.dataset.reveals = 'on';

    const reduced = window.matchMedia('(prefers-reduced-motion: reduce)').matches;
    if (reduced || !('IntersectionObserver' in window)) {
        for (const item of items) item.dataset.reveal = 'in';
        return;
    }

    const io = new IntersectionObserver(
        (entries) => {
            for (const entry of entries) {
                if (!entry.isIntersecting) continue;
                const delay = Number(entry.target.dataset.revealStep || 0) * 70;
                setTimeout(() => {
                    entry.target.dataset.reveal = 'in';
                }, delay);
                io.unobserve(entry.target);
            }
        },
        { rootMargin: '0px 0px -12% 0px', threshold: 0.08 },
    );

    for (const item of items) io.observe(item);
}

/* ────────────────────────────────────────────────────────────────────────
   What stands behind the page.
   ──────────────────────────────────────────────────────────────────────── */
/*
 * The wiring behind the page.
 *
 * Two earlier answers to the empty background were pictures — a field of cells,
 * then the shield at architectural scale — and a picture behind a page is
 * decoration however carefully it is drawn. This one is not a picture of the
 * product but the thing the product does: a sensor fires and a signal leaves
 * for the phone. The rails are where it travels.
 *
 * Almost all of it is CSS, including every moving part. The only thing left for
 * a script is knowing when the wiring is allowed to be seen: the opening screen
 * belongs to the mark, and nothing else may be on it.
 */
export function initBackdrop() {
    const backdrop = document.querySelector('.wires');
    if (!backdrop) return;

    const root = document.documentElement;
    const gate = document.querySelector('.gate');
    let queued = false;

    function update() {
        queued = false;
        const scrolled = window.scrollY || window.pageYOffset || 0;

        /* The opening owns its screen; the mark is the only thing on it. This
           arrives as that one leaves. */
        const runway = gate ? Math.max(1, gate.offsetHeight - window.innerHeight) : 0;
        const shown = gate ? smoothstep(0.45, 0.9, gateProgress(scrolled, runway)) : 1;

        root.style.setProperty('--wires-in', shown.toFixed(3));
    }

    const queue = () => {
        if (queued) return;
        queued = true;
        requestAnimationFrame(update);
    };

    update();
    window.addEventListener('scroll', queue, { passive: true });
    window.addEventListener('resize', queue);
}
