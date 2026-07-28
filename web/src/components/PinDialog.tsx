import { useState } from 'preact/hooks';
import { pinPrompt, send } from '../lib/store';
import { Scrim } from './Scrim';

/** Asked for when PIN protection is on and someone tries to disarm. */
export function PinDialog() {
    const [pin, setPin] = useState('');
    const [error, setError] = useState<string | null>(null);

    if (!pinPrompt.value) return null;

    const close = () => {
        setPin('');
        setError(null);
        pinPrompt.value = false;
    };

    const submit = () => {
        if (!pin.trim()) {
            setError('Enter your PIN to disarm.');
            return;
        }
        send({ type: 'disarm_with_pin', pin: pin.trim() });
        close();
    };

    return (
        <Scrim onClose={close} label="Enter your PIN to disarm">
            <div class="dialog rise">
                <h2 class="readout">Disarm</h2>
                <p class="dialog-lead">Enter your PIN.</p>
                <input
                    class="field figure"
                    type="password"
                    inputMode="numeric"
                    autoComplete="off"
                    maxLength={8}
                    value={pin}
                    // biome-ignore lint/a11y/noAutofocus: the dialog exists only to take this one value
                    autoFocus
                    onInput={(e) => setPin((e.target as HTMLInputElement).value)}
                    onKeyDown={(e) => e.key === 'Enter' && submit()}
                />
                {error && <p class="dialog-error">{error}</p>}
                <div class="dialog-actions">
                    <button type="button" class="chip" onClick={close}>
                        Cancel
                    </button>
                    <button type="button" class="alarm-primary" onClick={submit}>
                        Disarm
                    </button>
                </div>
            </div>
        </Scrim>
    );
}
