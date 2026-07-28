import { useEffect, useRef, useState } from 'preact/hooks';
import { captureAnchor } from '../lib/geo';
import { armed, send } from '../lib/store';

const HOLD_MS = 1500;

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

    const label = counting !== null ? 'Cancel' : armed.value ? 'Hold to disarm' : 'Arm';

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
