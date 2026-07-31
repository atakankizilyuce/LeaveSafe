import { useState } from 'preact/hooks';
import { SENSOR_CAPTIONS, SENSOR_DESCRIPTIONS } from '../lib/protocol';
import { armed, send, sensors, showToast, tripped } from '../lib/store';

/**
 * The sensor list as an annunciator panel.
 *
 * Aircraft caption lights sit dark and unremarkable until their condition
 * fires, and then they are the only thing you can look at. Sensors work exactly
 * that way, so they are drawn that way: a tile is dim when its sensor is quiet,
 * outlined when it is armed and watching, and lit when it has tripped.
 *
 * Tapping a tile expands it for the toggle and the self-test, which keeps the
 * resting state to six words instead of six rows of controls.
 */
export function Annunciator() {
    const [open, setOpen] = useState<string | null>(null);
    const list = sensors.value;

    if (list.length === 0) {
        return null;
    }

    return (
        <section class="card">
            <div class="card-head">
                <h2 class="readout">Sensors</h2>
                <span class="readout">Tap to configure</span>
            </div>

            <div class="grid">
                {list.map((sensor, i) => {
                    const lit = tripped.value[sensor.name] !== undefined;
                    const off = !sensor.available;
                    // Enabled and available but not running: the driver failed
                    // and the laptop is restarting it. This used to read as
                    // "ready", which is the panel claiming cover it does not
                    // have — the one lie an alarm cannot afford.
                    const failed = !off && Boolean(sensor.failure);
                    const watching = sensor.enabled && sensor.available && !failed && armed.value;

                    return (
                        <button
                            key={sensor.name}
                            type="button"
                            class="cap rise"
                            style={{ animationDelay: `${i * 38}ms` }}
                            data-lit={lit}
                            data-off={off}
                            data-failed={failed}
                            data-watching={watching}
                            data-enabled={sensor.enabled}
                            aria-expanded={open === sensor.name}
                            onClick={() => setOpen(open === sensor.name ? null : sensor.name)}
                        >
                            <span class="cap-label">
                                {SENSOR_CAPTIONS[sensor.name] ?? sensor.display_name}
                            </span>
                            <span class="cap-state readout">
                                {off
                                    ? 'n/a'
                                    : lit
                                      ? 'tripped'
                                      : failed
                                        ? 'FAULT'
                                        : sensor.enabled
                                          ? 'ready'
                                          : 'off'}
                            </span>
                        </button>
                    );
                })}
            </div>

            {open !== null && <Detail name={open} onClose={() => setOpen(null)} />}
        </section>
    );
}

function Detail({ name, onClose }: { name: string; onClose(): void }) {
    const sensor = sensors.value.find((s) => s.name === name);
    if (!sensor) return null;

    const toggle = () => {
        if (armed.value) {
            showToast('Disarm before changing sensors');
            return;
        }
        send({ type: 'configure', sensors: { [name]: !sensor.enabled } });
    };

    return (
        <div class="cap-detail rise">
            <div class="cap-detail-head">
                <strong>{sensor.display_name}</strong>
                <button type="button" class="chip" onClick={onClose}>
                    Close
                </button>
            </div>
            <p class="cap-desc">{SENSOR_DESCRIPTIONS[name] ?? ''}</p>
            {!sensor.available && (
                <p class="cap-warn">This machine has no sensor for that, so it cannot be used.</p>
            )}
            {sensor.available && sensor.failure && (
                <p class="cap-warn">
                    This sensor is not watching right now: {sensor.failure}. The laptop is restarting it, and
                    the tile clears when it comes back.
                </p>
            )}
            <div class="cap-actions">
                <button
                    type="button"
                    class="chip"
                    disabled={!sensor.available}
                    onClick={() => send({ type: 'trigger_sensor', sensor: name })}
                >
                    Test it
                </button>
                <button
                    type="button"
                    class={sensor.enabled ? 'chip on' : 'chip'}
                    disabled={!sensor.available}
                    aria-pressed={sensor.enabled}
                    onClick={toggle}
                >
                    {sensor.enabled ? 'Watching' : 'Not watching'}
                </button>
            </div>
        </div>
    );
}
