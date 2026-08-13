import { useEffect, useState } from 'preact/hooks';
import { groupKey } from '../lib/format';
import { pairError, pairing } from '../lib/store';
import { bluetoothSupported } from '../lib/transport';
import { Icon } from './Icons';

interface Props {
    onPair(key: string, over: 'websocket' | 'bluetooth'): void;
    initialKey: string;
}

/**
 * The first screen, and for most people the only one they ever type into. The
 * key is the whole interaction, so it gets the whole screen.
 */
export function PairScreen({ onPair, initialKey }: Props) {
    const [key, setKey] = useState(groupKey(initialKey));
    const digits = key.replace(/\D/g, '');
    const ready = digits.length === 16;

    // A scanned code arrives after this screen has already mounted, so the
    // initial state missed it. Without this the field sits empty behind the
    // auto-connect — fine when it works, and a retype from scratch when it
    // does not.
    useEffect(() => {
        if (initialKey) setKey(groupKey(initialKey));
    }, [initialKey]);

    const submit = (over: 'websocket' | 'bluetooth') => {
        if (!ready) {
            pairError.value = 'That key needs 16 digits.';
            return;
        }
        onPair(digits, over);
    };

    return (
        <main class="pair">
            <div class="pair-inner rise">
                <div class="pair-mark readout">LeaveSafe</div>
                <h1 class="pair-title">Leave it. We'll watch it.</h1>
                <p class="pair-lead">Enter the key shown on your laptop, or scan its code to fill this in.</p>

                <label class="pair-label readout" htmlFor="key">
                    Pairing key
                </label>
                <input
                    id="key"
                    class="field key-field figure"
                    inputMode="numeric"
                    autoComplete="off"
                    spellcheck={false}
                    placeholder="0000-0000-0000-0000"
                    maxLength={19}
                    value={key}
                    onInput={(e) => setKey(groupKey((e.target as HTMLInputElement).value))}
                    onKeyDown={(e) => e.key === 'Enter' && submit('websocket')}
                />

                <div class="key-progress" aria-hidden="true">
                    {Array.from({ length: 16 }, (_, i) => (
                        <span key={i} data-filled={i < digits.length} />
                    ))}
                </div>

                <button
                    type="button"
                    class="pair-go"
                    disabled={pairing.value || !ready}
                    onClick={() => submit('websocket')}
                >
                    {/* The field above this one is labelled "pairing key", so
                        the button and the field say the same word twice: once
                        in text and once in shape. */}
                    <Icon name="key" />
                    {pairing.value ? 'Connecting…' : 'Connect'}
                </button>

                {bluetoothSupported() && (
                    <button type="button" class="pair-alt" onClick={() => submit('bluetooth')}>
                        <Icon name="bluetooth" />
                        Connect over Bluetooth instead
                    </button>
                )}

                {pairError.value && <p class="pair-error">{pairError.value}</p>}
            </div>
        </main>
    );
}
