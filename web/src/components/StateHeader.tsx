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

    return (
        <header class="state">
            <div class="state-word-wrap">
                <div class="state-word" data-counting={counting !== null}>
                    {stateWord(counting, armed.value)}
                </div>
                {counting !== null && <div class="state-count figure">{counting}</div>}
            </div>

            <Trace />

            <div class="state-detail readout">{stateDetail()}</div>
        </header>
    );
}

/**
 * The word itself. Arming wins over armed: the countdown is the state the user
 * is in, and it is the one they can still change their mind about.
 */
function stateWord(counting: number | null, isArmed: boolean): string {
    if (counting !== null) return 'ARMING';
    return isArmed ? 'ARMED' : 'STANDBY';
}

/**
 * The line under the word, saying how much of the claim above it is still true.
 *
 * A lost connection is said first and on its own. The sensor count and the
 * elapsed time both describe a laptop this phone is no longer hearing from, so
 * with the link down they are last known values dressed as current ones.
 *
 * Called during the header's own render rather than mounted as a component of
 * its own. As a component it would carry the signals optimisation, which skips
 * a re-render when no signal changed — and the elapsed time changes with the
 * clock, not with a signal, so the readout would sit at "just now" forever.
 */
function stateDetail() {
    if (link.value === 'lost') {
        return <span class="state-lost">Connection lost</span>;
    }

    const count = activeSensorCount.value;
    if (armed.value) {
        return (
            <span>
                Watching {count} sensor{count === 1 ? '' : 's'} · {since(armedSince.value)}
            </span>
        );
    }
    return (
        <span>
            {count} sensor{count === 1 ? '' : 's'} ready
        </span>
    );
}
