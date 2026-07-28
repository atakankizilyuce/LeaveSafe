import { useEffect, useRef, useState } from 'preact/hooks';
import { since } from '../lib/format';
import { activeSensorCount, armed, armedSince, link } from '../lib/store';
import { Trace } from './Trace';

interface Props {
    counting: number | null;
}

/**
 * The one thing you should be able to read from across the room. The word is
 * the interface; everything under it is detail.
 */
export function StateHeader({ counting }: Props) {
    const [, force] = useState(0);
    const tick = useRef<number>();

    // The elapsed readout has to move on its own or "armed 4m ago" quietly
    // becomes a lie.
    useEffect(() => {
        tick.current = window.setInterval(() => force((n) => n + 1), 1000);
        return () => window.clearInterval(tick.current);
    }, []);

    const word = counting !== null ? 'ARMING' : armed.value ? 'ARMED' : 'STANDBY';
    const count = activeSensorCount.value;

    return (
        <header class="state">
            <div class="state-word-wrap">
                <div class="state-word" data-counting={counting !== null}>
                    {word}
                </div>
                {counting !== null && <div class="state-count figure">{counting}</div>}
            </div>

            <Trace />

            <div class="state-detail readout">
                {link.value === 'lost' ? (
                    <span class="state-lost">Connection lost</span>
                ) : armed.value ? (
                    <span>
                        Watching {count} sensor{count === 1 ? '' : 's'} · {since(armedSince.value)}
                    </span>
                ) : (
                    <span>
                        {count} sensor{count === 1 ? '' : 's'} ready
                    </span>
                )}
            </div>
        </header>
    );
}
