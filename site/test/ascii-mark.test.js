import { describe, expect, it } from 'vitest';

import {
    animMod,
    clamp,
    dispersalLead,
    easeInOutCubic,
    easeOutCubic,
    gateProgress,
    gridFalloff,
    handover,
    hash01,
    heroRise,
    introVeil,
    luminance,
    overlayMix,
    PIVOT,
    silhouette,
    smoothstep,
    sobel,
    TAU,
    textFade,
    tone,
    VEIL_FEATHER,
} from '../ascii-mark.js';

/*
 * The mark's arithmetic. The canvas it ends up on cannot be exercised here —
 * jsdom has no 2d context — so what is worth pinning down is everything the
 * painting reads off: the reveal that has to finish, the animation that has to
 * stay bounded, and the two curves the handover rides on.
 */

describe('hash01', () => {
    it('is stable for an index', () => {
        expect(hash01(41)).toBe(hash01(41));
    });

    it('stays inside the unit interval', () => {
        for (let i = 0; i < 500; i++) {
            const v = hash01(i);
            expect(v).toBeGreaterThanOrEqual(0);
            expect(v).toBeLessThan(1);
        }
    });

    it('spreads across it rather than clumping', () => {
        const buckets = [0, 0, 0, 0];
        for (let i = 0; i < 4000; i++) buckets[Math.floor(hash01(i) * 4)]++;
        for (const n of buckets) expect(n).toBeGreaterThan(700);
    });
});

describe('clamp', () => {
    it('holds the ends', () => {
        expect(clamp(-3, 0, 1)).toBe(0);
        expect(clamp(9, 0, 1)).toBe(1);
        expect(clamp(0.4, 0, 1)).toBe(0.4);
    });
});

describe('luminance', () => {
    it('is 0 at black and 1 at white', () => {
        expect(luminance(0, 0, 0)).toBe(0);
        expect(luminance(255, 255, 255)).toBeCloseTo(1, 6);
    });

    it('weights green over blue, the way an eye does', () => {
        expect(luminance(0, 255, 0)).toBeGreaterThan(luminance(0, 0, 255));
    });
});

describe('sobel', () => {
    it('finds nothing in a flat field', () => {
        const flat = new Float32Array(16).fill(0.5);
        for (const v of sobel(flat, 4, 4)) expect(v).toBe(0);
    });

    it('answers along an edge and not beside it', () => {
        /* Left half dark, right half light: the response belongs at the seam. */
        const cols = 6;
        const rows = 3;
        const lum = new Float32Array(cols * rows);
        for (let y = 0; y < rows; y++) {
            for (let x = 0; x < cols; x++) lum[y * cols + x] = x < 3 ? 0 : 1;
        }
        const edge = sobel(lum, cols, rows);
        const middleRow = cols;
        expect(edge[middleRow + 2]).toBeGreaterThan(0.5);
        expect(edge[middleRow + 0]).toBe(0);
        expect(edge[middleRow + 5]).toBe(0);
    });

    it('never exceeds one', () => {
        const lum = new Float32Array(25);
        for (let i = 0; i < 25; i++) lum[i] = i % 2;
        for (const v of sobel(lum, 5, 5)) expect(v).toBeLessThanOrEqual(1);
    });
});

describe('tone', () => {
    it('leaves the pivot where it is', () => {
        expect(tone(PIVOT, 0, 0, 1.4)).toBeCloseTo(PIVOT, 6);
    });

    it('pushes light cells lighter and dark cells darker as contrast rises', () => {
        expect(tone(0.7, 0, 0, 1.5)).toBeGreaterThan(tone(0.7, 0, 0, 1));
        expect(tone(0.1, 0, 0, 1.5)).toBeLessThan(tone(0.1, 0, 0, 1));
    });

    it('lifts a cell that sits on an edge', () => {
        expect(tone(0.4, 1, 0.5, 1)).toBeGreaterThan(tone(0.4, 0, 0.5, 1));
    });

    it('stays within range however hard it is pushed', () => {
        expect(tone(1, 1, 1, 4)).toBe(1);
        expect(tone(0, 0, 0, 4)).toBe(0);
    });
});

describe('the opening', () => {
    it('shows nothing at the start and everything at the end', () => {
        expect(introVeil(0, 0)).toBe(0);
        expect(introVeil(0, 1)).toBe(0);
        for (const ny of [0, 0.25, 0.5, 0.75, 1]) expect(introVeil(1, ny)).toBe(1);
    });

    it('travels upward — the bottom of the mark arrives first', () => {
        expect(introVeil(0.4, 1)).toBeGreaterThan(introVeil(0.4, 0.2));
    });

    it('never goes backwards as it runs', () => {
        let last = -1;
        for (let p = 0; p <= 1.0001; p += 0.02) {
            const v = introVeil(p, 0.5);
            expect(v).toBeGreaterThanOrEqual(last);
            last = v;
        }
    });

    it('feathers rather than cutting a straight line', () => {
        /* Somewhere mid-run there is a row that is neither in nor out. */
        const partial = [];
        for (let ny = 0; ny <= 1; ny += 0.05) {
            const v = introVeil(0.5, ny);
            if (v > 0 && v < 1) partial.push(v);
        }
        expect(partial.length).toBeGreaterThan(1);
        expect(VEIL_FEATHER).toBeGreaterThan(0);
    });

    it('arrives as a silhouette and ends in colour', () => {
        expect(silhouette(0)).toBe(1);
        expect(silhouette(1)).toBe(0);
        expect(silhouette(0.3)).toBeGreaterThan(silhouette(0.8));
    });
});

describe('animMod', () => {
    const base = { i: 12, t: 1.7, speed: 1.36, amp: 0.8, d: 0.3, nx: 0.1, ny: -0.2, phase: 1.1, h: 0.42 };

    it('does not move a cell when the style is unknown', () => {
        expect(animMod('off', base)).toBe(1);
    });

    it('stays positive and bounded for every style it ships with', () => {
        for (const style of ['flow', 'ripple', 'wave', 'shimmer', 'pulse', 'flicker']) {
            for (let t = 0; t < 6; t += 0.037) {
                const m = animMod(style, { ...base, t });
                expect(m).toBeGreaterThan(0);
                expect(m).toBeLessThan(3);
            }
        }
    });

    it('holds still when the intensity is zero', () => {
        for (const style of ['flow', 'ripple', 'wave', 'shimmer', 'pulse', 'flicker']) {
            expect(animMod(style, { ...base, amp: 0 })).toBe(1);
        }
    });

    it('gives the same answer for the same instant', () => {
        expect(animMod('flow', base)).toBe(animMod('flow', base));
    });

    it('moves — a cell is not the same a moment later', () => {
        expect(animMod('flow', base)).not.toBe(animMod('flow', { ...base, t: base.t + 0.2 }));
    });

    it('is continuous, which is what separates flow from flicker', () => {
        const step = (style) => {
            let worst = 0;
            for (let t = 0; t < 4; t += 1 / 60) {
                const a = animMod(style, { ...base, t });
                const b = animMod(style, { ...base, t: t + 1 / 60 });
                worst = Math.max(worst, Math.abs(a - b));
            }
            return worst;
        };
        expect(step('ripple')).toBeLessThan(step('flicker'));
    });

    it('sparks: flow keeps one frame of the flicker it replaced', () => {
        /* The spark is the whole of what survived from the reference preset —
           a single bright frame on a cell, rarely. It is rare on purpose, so
           finding one means walking a few cells for a few seconds. */
        let jump = 0;
        for (let i = 0; i < 120 && jump < 0.5; i++) {
            let prev = animMod('flow', { ...base, i, t: 0 });
            for (let t = 1 / 60; t < 10; t += 1 / 60) {
                const now = animMod('flow', { ...base, i, t });
                jump = Math.max(jump, Math.abs(now - prev));
                prev = now;
            }
        }
        /* 0.9 x the intensity, well clear of anything the two sines can do
           between one frame and the next. */
        expect(jump).toBeGreaterThan(0.5);
    });

    it('rides the ring outward: distance from the eye shifts the phase', () => {
        expect(animMod('ripple', base)).not.toBe(animMod('ripple', { ...base, d: 0.45 }));
    });
});

describe('the handover', () => {
    it('reads nothing at the top and everything at the end of the runway', () => {
        expect(gateProgress(0, 900)).toBe(0);
        expect(gateProgress(450, 900)).toBeCloseTo(0.5, 6);
        expect(gateProgress(4000, 900)).toBe(1);
    });

    it('survives a runway of zero rather than dividing by it', () => {
        expect(gateProgress(120, 0)).toBe(0);
        expect(gateProgress(120, -5)).toBe(0);
    });

    it('holds the mark for a beat before letting it go', () => {
        expect(handover(0)).toBe(0);
        expect(handover(0.05)).toBe(0);
        expect(handover(0.2)).toBeGreaterThan(0);
    });

    it('is finished before the runway runs out, so the page has air to arrive into', () => {
        expect(handover(0.86)).toBeCloseTo(1, 9);
        expect(handover(0.95)).toBe(1);
        expect(handover(1)).toBe(1);
    });

    it('only ever advances', () => {
        let last = -1;
        for (let p = 0; p <= 1.0001; p += 0.01) {
            const v = handover(p);
            expect(v).toBeGreaterThanOrEqual(last - 1e-9);
            last = v;
        }
    });

    it('brings the page up while the mark is still coming apart', () => {
        /* The overlap is the whole point: the hero has started before the
           dispersal has finished, so there is never a screen of nothing. */
        expect(heroRise(0.3)).toBe(0);
        expect(heroRise(0.6)).toBeGreaterThan(0);
        expect(heroRise(0.6)).toBeLessThan(1);
        expect(handover(0.6)).toBeLessThan(1);
        expect(heroRise(0.9)).toBe(1);
    });

    it('staggers the panel behind the words', () => {
        expect(heroRise(0.6, 0.08)).toBeLessThan(heroRise(0.6));
    });

    it('drops the caption first — it is gone well before the mark is', () => {
        expect(textFade(0)).toBe(1);
        expect(textFade(0.3)).toBe(0);
        expect(textFade(0.15)).toBeLessThan(1);
        expect(textFade(0.15)).toBeGreaterThan(0);
    });
});

describe('dispersal', () => {
    it('holds everything in place until the handover starts', () => {
        for (const dist of [0, 0.5, 1]) expect(dispersalLead(0, dist)).toBe(0);
    });

    it('has taken every cell by the end', () => {
        for (const dist of [0, 0.5, 1]) expect(dispersalLead(1, dist)).toBe(1);
    });

    it('takes the outside first and the middle last — the eye leaves alone', () => {
        expect(dispersalLead(0.4, 1)).toBeGreaterThan(dispersalLead(0.4, 0));
        /* Halfway through, the centre has not begun to move. */
        expect(dispersalLead(0.3, 0)).toBe(0);
    });

    it('accelerates rather than drifting at a constant speed', () => {
        /* Measured below the point where the outermost cells have already gone
           — past it every step is zero and the comparison means nothing. */
        const early = dispersalLead(0.2, 1) - dispersalLead(0.1, 1);
        const late = dispersalLead(0.5, 1) - dispersalLead(0.4, 1);
        expect(late).toBeGreaterThan(early);
    });

    it('never goes backwards, for any cell', () => {
        for (const dist of [0, 0.3, 0.7, 1]) {
            let last = -1;
            for (let q = 0; q <= 1.0001; q += 0.01) {
                const v = dispersalLead(q, dist);
                expect(v).toBeGreaterThanOrEqual(last - 1e-9);
                last = v;
            }
        }
    });
});

describe('smoothstep', () => {
    it('holds at the ends and passes through the middle', () => {
        expect(smoothstep(0.2, 0.8, 0)).toBe(0);
        expect(smoothstep(0.2, 0.8, 1)).toBe(1);
        expect(smoothstep(0.2, 0.8, 0.5)).toBeCloseTo(0.5, 6);
    });

    it('is a step when the two edges meet, rather than a division by zero', () => {
        expect(smoothstep(0.5, 0.5, 0.4)).toBe(0);
        expect(smoothstep(0.5, 0.5, 0.6)).toBe(1);
    });
});

describe('overlayMix', () => {
    it('changes nothing at zero', () => {
        expect(overlayMix(0.4, 0.9, 0)).toBeCloseTo(0.4, 6);
    });

    it('deepens the blue of a dark cell and holds back its red', () => {
        /* The tint is #3d8fc4: little red, a lot of blue. Over a dark cell,
           overlay doubles the blend, so the two channels part company. */
        const dark = 0.2;
        expect(overlayMix(dark, 0.769, 0.5)).toBeGreaterThan(dark);
        expect(overlayMix(dark, 0.239, 0.5)).toBeLessThan(dark);
    });

    it('leaves black black — there is nothing to tint', () => {
        expect(overlayMix(0, 0.769, 1)).toBe(0);
    });

    it('stays in range whatever it is given', () => {
        for (const base of [-1, 0, 0.5, 1, 2]) {
            for (const amt of [-1, 0.5, 3]) {
                const v = overlayMix(base, 0.7, amt);
                expect(v).toBeGreaterThanOrEqual(0);
                expect(v).toBeLessThanOrEqual(1);
            }
        }
    });
});

describe('gridFalloff', () => {
    it('leaves the middle of the field alone', () => {
        expect(gridFalloff(0, 0.02)).toBe(1);
    });

    it('dissolves the dim grid before it can draw a square edge', () => {
        /* A contrast below 100 lifts the blacks, so the empty field sits around
           0.13 rather than at nothing — the number that matters is that one,
           not zero. */
        expect(gridFalloff(1.3, 0.13)).toBe(0);
        expect(gridFalloff(0.7, 0.13)).toBeLessThan(1);
        expect(gridFalloff(0.7, 0.13)).toBeGreaterThan(0);
    });

    it("keeps the mark's own cells, which reach further out than the grid does", () => {
        /* The shield's stroke rests around 0.55 and its top corners sit around
           0.93 out. A falloff that went by distance alone would eat them. */
        expect(gridFalloff(0.93, 0.55)).toBe(1);
        expect(gridFalloff(1.3, 0.55)).toBe(1);
    });
});

describe('easings', () => {
    it('run from 0 to 1', () => {
        expect(easeOutCubic(0)).toBe(0);
        expect(easeOutCubic(1)).toBe(1);
        expect(easeInOutCubic(0)).toBe(0);
        expect(easeInOutCubic(1)).toBe(1);
    });

    it('clamp what is handed to them out of range', () => {
        expect(easeOutCubic(-2)).toBe(0);
        expect(easeInOutCubic(3)).toBe(1);
    });

    it('ease out fast and ease in-out slow, at the quarter mark', () => {
        expect(easeOutCubic(0.25)).toBeGreaterThan(0.25);
        expect(easeInOutCubic(0.25)).toBeLessThan(0.25);
    });

    it('passes through its own middle', () => {
        expect(easeInOutCubic(0.5)).toBeCloseTo(0.5, 6);
    });
});

describe('TAU', () => {
    it('is one turn', () => {
        expect(Math.sin(TAU)).toBeCloseTo(0, 12);
    });
});
