import { useEffect, useRef, useState } from 'preact/hooks';
import { captureAnchor } from '../lib/geo';
import { primeSiren } from '../lib/siren';
import { armed, send, showToast } from '../lib/store';

const HOLD_MS = 1500;

/**
 * What the button says it will do next, which is also what a screen reader
 * announces it as. During the countdown that is Cancel and nothing else: the
 * armed state underneath has not changed yet, and offering to disarm something
 * that is not yet armed would be the wrong promise.
 */
function buttonLabel(counting: number | null, isArmed: boolean): string {
    if (counting !== null) return 'Cancel';
    return isArmed ? 'Hold to disarm' : 'Arm';
}

interface Props {
    counting: number | null;
    onCountdown(value: number | null): void;
}

/**
 * Arm on a tap with a countdown; disarm on a deliberate hold.
 *
 * The asymmetry is the point. Arming by accident costs you nothing — you are
 * standing right there. Disarming by accident silently turns off the thing
 * guarding your laptop, so it asks for a second and a half of intent, drawn as
 * a ring that fills.
 */
export function ArmControl({ counting, onCountdown }: Props) {
    const [holding, setHolding] = useState(0);
    const countdown = useRef<number>();
    const holdTimer = useRef<number>();
    const holdFrame = useRef<number>();
    const justDisarmed = useRef(false);

    useEffect(() => () => stopAll(), []);

    function stopAll() {
        window.clearInterval(countdown.current);
        window.clearTimeout(holdTimer.current);
        if (holdFrame.current) cancelAnimationFrame(holdFrame.current);
    }

    function startArming() {
        if (counting !== null) return;
        let left = 3;
        onCountdown(left);
        countdown.current = window.setInterval(() => {
            left -= 1;
            if (left <= 0) {
                window.clearInterval(countdown.current);
                onCountdown(null);
                send({ type: 'arm' });
                captureAnchor();
                send({ type: 'get_location' });
                // Said here rather than pinned to the panel: it is true all the
                // time, but it only changes what someone does at the moment they
                // are about to put the phone away.
                showToast(
                    'Armed. If your phone sleeps the alert may not arrive — the laptop still sounds its own alarm.',
                );
            } else {
                onCountdown(left);
            }
        }, 1000);
    }

    function cancelArming() {
        window.clearInterval(countdown.current);
        onCountdown(null);
    }

    function startHold() {
        const began = Date.now();
        const step = () => {
            const progress = Math.min((Date.now() - began) / HOLD_MS, 1);
            setHolding(progress);
            if (progress < 1) holdFrame.current = requestAnimationFrame(step);
        };
        holdFrame.current = requestAnimationFrame(step);

        holdTimer.current = window.setTimeout(() => {
            justDisarmed.current = true;
            endHold();
            send({ type: 'disarm' });
        }, HOLD_MS);
    }

    function endHold() {
        window.clearTimeout(holdTimer.current);
        if (holdFrame.current) cancelAnimationFrame(holdFrame.current);
        setHolding(0);
    }

    function onPress() {
        if (armed.value) startHold();
    }

    function onRelease() {
        if (armed.value) endHold();
    }

    function onClick() {
        // Inside the tap, because a phone only lets a page open audio from one.
        // This is the last thing the user touches before walking away, so it is
        // the last chance to make the siren able to sound at all.
        primeSiren();
        if (justDisarmed.current) {
            justDisarmed.current = false;
            return;
        }
        if (counting !== null) {
            cancelArming();
            return;
        }
        if (!armed.value) startArming();
    }

    const label = buttonLabel(counting, armed.value);

    return (
        <div class="dock">
            <button
                type="button"
                class="arm"
                data-armed={armed.value}
                data-counting={counting !== null}
                aria-label={label}
                onClick={onClick}
                onMouseDown={onPress}
                onMouseUp={onRelease}
                onMouseLeave={onRelease}
                onTouchStart={(e) => {
                    if (armed.value) e.preventDefault();
                    onPress();
                }}
                onTouchEnd={(e) => {
                    if (armed.value) e.preventDefault();
                    onRelease();
                }}
                onTouchCancel={onRelease}
            >
                <span
                    class="arm-ring"
                    style={{ '--fill': holding } as unknown as Record<string, string>}
                    aria-hidden="true"
                />
                <span class="arm-label">{label}</span>
            </button>
        </div>
    );
}
