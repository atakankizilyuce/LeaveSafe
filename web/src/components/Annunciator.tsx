import { useState } from 'preact/hooks';
import { SENSOR_CAPTIONS, SENSOR_DESCRIPTIONS, type SensorInfo } from '../lib/protocol';
import { armed, send, sensors, setSensorEnabled, showToast, tripped } from '../lib/store';

/**
 * The sensor list as an annunciator panel.
 *
 * Aircraft caption lights sit dark and unremarkable until their condition
 * fires, and then they are the only thing you can look at. Sensors work exactly
 * that way, so they are drawn that way: a tile is dim when its sensor is quiet,
 * outlined when it is armed and watching, and lit when it has tripped.
 *
 * The tile is also the switch. Tapping it turns that sensor on or off there and
 * then, because the question a caption light answers — is this one watching? —
 * is the same one the user wants to change, and putting a panel in between made
 * a one-tap answer take three. The switch on the face of the tile says which
 * way the tap will go before it is made.
 *
 * The corner "i" opens what two words cannot say: what the sensor actually
 * watches, why it is unavailable on this machine, and the self-test.
 */
export function Annunciator() {
    const [open, setOpen] = useState<string | null>(null);
    const list = sensors.value;

    if (list.length === 0) {
        return null;
    }

    const toggle = (sensor: SensorInfo) => {
        if (!sensor.available) {
            showToast(`This machine has no ${sensor.display_name.toLowerCase()} to watch`);
            return;
        }
        if (armed.value) {
            showToast('Disarm before changing sensors');
            return;
        }
        const next = !sensor.enabled;
        setSensorEnabled(sensor.name, next);
        send({ type: 'configure', sensors: { [sensor.name]: next } });
    };

    return (
        <section class="card">
            <div class="card-head">
                <h2 class="readout">Sensors</h2>
                <span class="readout">{armed.value ? 'Disarm to change' : 'Tap to turn on or off'}</span>
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
                        <div
                            key={sensor.name}
                            class="cap-slot rise"
                            style={{ animationDelay: `${i * 38}ms` }}
                        >
                            <button
                                type="button"
                                class="cap"
                                role="switch"
                                aria-checked={sensor.enabled}
                                aria-disabled={off}
                                data-lit={lit}
                                data-off={off}
                                data-failed={failed}
                                data-watching={watching}
                                data-enabled={sensor.enabled}
                                onClick={() => toggle(sensor)}
                            >
                                <span class="cap-label">
                                    {SENSOR_CAPTIONS[sensor.name] ?? sensor.display_name}
                                </span>
                                <span class="cap-foot">
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
                                    <span class="cap-switch" aria-hidden="true">
                                        <span class="cap-knob" />
                                    </span>
                                </span>
                            </button>
                            <button
                                type="button"
                                class="cap-info"
                                aria-label={`What ${sensor.display_name} watches`}
                                aria-expanded={open === sensor.name}
                                onClick={() => setOpen(open === sensor.name ? null : sensor.name)}
                            >
                                <span aria-hidden="true">i</span>
                            </button>
                        </div>
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
            {/* No on/off button here. The tile above is the switch, and a second
                one for the same setting only raises the question of which one
                the sensor is actually following. */}
            <div class="cap-actions">
                <button
                    type="button"
                    class="chip"
                    disabled={!sensor.available}
                    onClick={() => send({ type: 'trigger_sensor', sensor: name })}
                >
                    Test it
                </button>
            </div>
        </div>
    );
}
