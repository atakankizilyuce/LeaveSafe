import { useEffect, useRef } from 'preact/hooks';

/**
 * Pulling a sheet down to put it away.
 *
 * The sheet already looked like something you could push down — it sits on the
 * bottom edge with a grip drawn across the top — and it was not. Dragging it
 * did nothing, so the only thing that ever closed it was a thumb that happened
 * to land on the dimmed backdrop above it. That is why it seemed to work
 * sometimes: the gesture was never the thing being read, only where it started.
 *
 * So the gesture is read here, on the sheet itself, and it either follows the
 * finger or it does not — no third outcome where the sheet ignores a pull and
 * closes anyway because the thumb drifted off its top edge.
 */

/** Past this far down, letting go puts the sheet away instead of springing back. */
const DISMISS_PX = 96;

/** Or a flick: this fast downwards at release closes it from anywhere. */
const DISMISS_VELOCITY = 0.5;

/** Under this much movement it is still a tap, and the sheet does not move. */
const CLAIM_PX = 8;

/** How long the sheet takes to clear the screen once the gesture has decided. */
const EXIT_MS = 190;

/** Where a drag may start even when the list underneath could scroll instead. */
const HANDLE_SELECTOR = '.sheet-handle, .sheet-head';

/**
 * Wires drag-to-dismiss onto a scrollable sheet and returns the ref to put on
 * it.
 *
 * A downward drag belongs to the sheet only when there is nothing left to
 * scroll up into — otherwise it is the list being scrolled, which is the more
 * common thing to want and the one that must not be stolen. Dragging the grip
 * or the header always belongs to the sheet, whatever the list is doing.
 */
export function useDragDismiss(onDismiss: () => void, mounted: boolean) {
    const ref = useRef<HTMLDivElement>(null);

    // Read at gesture end rather than captured at mount: `close` is rebuilt on
    // every render, and a stale one would close a sheet that has since been
    // replaced.
    const dismiss = useRef(onDismiss);
    dismiss.current = onDismiss;

    // `mounted` is what re-runs this. The component holding the sheet stays
    // alive while the sheet is shut and renders nothing, so an effect that only
    // ran once would look for an element that will not exist for another few
    // minutes, find nothing, and never look again.
    useEffect(() => {
        const el = mounted ? ref.current : null;
        if (!el) return;

        let startY = 0;
        let startX = 0;
        let lastY = 0;
        let lastAt = 0;
        let velocity = 0;
        let offset = 0;
        let fromHandle = false;
        /** The gesture is ours: the sheet is following the finger. */
        let claimed = false;
        /** Still deciding. Once false, the browser keeps the gesture. */
        let watching = false;
        let exitTimer: number | null = null;

        const settle = (px: number, ms: number) => {
            el.style.transition = ms ? `transform ${ms}ms cubic-bezier(0.32, 0.72, 0, 1)` : '';
            el.style.transform = px ? `translateY(${px}px)` : '';
        };

        const onStart = (e: TouchEvent) => {
            // A second finger mid-drag means a pinch or a stray palm, and
            // neither should be read as "close this".
            if (e.touches.length !== 1) {
                if (claimed) settle(0, EXIT_MS);
                claimed = false;
                watching = false;
                return;
            }
            const touch = e.touches[0];
            if (!touch) return;
            startY = touch.clientY;
            startX = touch.clientX;
            lastY = touch.clientY;
            lastAt = e.timeStamp;
            velocity = 0;
            offset = 0;
            claimed = false;
            watching = true;
            fromHandle = Boolean((e.target as Element | null)?.closest?.(HANDLE_SELECTOR));
            el.style.transition = '';
        };

        const onMove = (e: TouchEvent) => {
            if (!watching || e.touches.length !== 1) return;
            const touch = e.touches[0];
            if (!touch) return;
            const dy = touch.clientY - startY;
            const dx = touch.clientX - startX;

            if (!claimed) {
                // Sideways, or upwards, or the list has somewhere to scroll
                // back to — none of those are this gesture.
                if (Math.abs(dx) > Math.abs(dy)) {
                    watching = false;
                    return;
                }
                if (dy < -CLAIM_PX) {
                    watching = false;
                    return;
                }
                if (dy < CLAIM_PX) return;
                if (!fromHandle && el.scrollTop > 0) {
                    watching = false;
                    return;
                }
                claimed = true;
            }

            const elapsed = e.timeStamp - lastAt;
            if (elapsed > 0) velocity = (touch.clientY - lastY) / elapsed;
            lastY = touch.clientY;
            lastAt = e.timeStamp;

            // Start counting from where the finger crossed the threshold, so
            // the sheet does not jump the first eight pixels in one frame.
            offset = Math.max(0, dy - CLAIM_PX);
            // The listener is registered non-passive precisely for this: with
            // the list already at its top there is nothing to scroll, and
            // without it the browser spends the gesture on a rubber-band
            // bounce of the page behind.
            e.preventDefault();
            settle(offset, 0);
        };

        const onEnd = () => {
            if (!claimed) {
                watching = false;
                return;
            }
            claimed = false;
            watching = false;

            if (offset < DISMISS_PX && velocity < DISMISS_VELOCITY) {
                settle(0, EXIT_MS);
                return;
            }

            const reduced = window.matchMedia('(prefers-reduced-motion: reduce)').matches;
            if (reduced) {
                dismiss.current();
                return;
            }
            // Carry on in the direction the finger was going rather than
            // vanishing under it.
            el.style.transition = `transform ${EXIT_MS}ms cubic-bezier(0.32, 0.72, 0, 1)`;
            el.style.transform = `translateY(${el.offsetHeight}px)`;
            exitTimer = window.setTimeout(() => {
                exitTimer = null;
                dismiss.current();
            }, EXIT_MS);
        };

        el.addEventListener('touchstart', onStart, { passive: true });
        el.addEventListener('touchmove', onMove, { passive: false });
        el.addEventListener('touchend', onEnd);
        el.addEventListener('touchcancel', onEnd);

        return () => {
            el.removeEventListener('touchstart', onStart);
            el.removeEventListener('touchmove', onMove);
            el.removeEventListener('touchend', onEnd);
            el.removeEventListener('touchcancel', onEnd);
            if (exitTimer !== null) window.clearTimeout(exitTimer);
        };
    }, [mounted]);

    return ref;
}
