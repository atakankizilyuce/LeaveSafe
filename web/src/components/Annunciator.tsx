import { useState } from 'preact/hooks';
import { SENSOR_CAPTIONS, SENSOR_DESCRIPTIONS, type SensorInfo } from '../lib/protocol';
import { armed, send, sensors, setSensorEnabled, showToast, tripped } from '../lib/store';
import { Icon, SensorIcon, StateBadge } from './Icons';

/**
 * The sensors, drawn as an orbit around the thing that watches with them.
 *
 * A grid of tiles could say which sensors were on, but it could not say what
 * arming *did* — six squares changing colour is a legend, not an event. Here
 * the geometry carries the claim: at rest the sensors stand apart around an
 * open ring with a shut eye at its centre, and arming pulls every sensor that
 * is actually covering you into that centre, which seals into a shield as they
 * arrive. "These four are now inside the thing that is watching" is a sentence
 * the shape says by itself.
 *
 * The ones that are not covering you travel the other way, out of the ring and
 * into a region of their own with a heading and a stated reason each. Dimming
 * them where they stood was the first attempt, and it read as "these are
 * somehow still involved". Opposite direction, separate place, reason in
 * words: three signals that all say the same thing.
 *
 * A sensor that has tripped goes nowhere. It stays on the ring in red, because
 * it is no longer one of the quiet ones doing their job — it is the thing you
 * came to look at.
 *
 * Each station is also its switch, so the question a caption light answers —
 * is this one watching? — is changed by tapping the answer. What the sensors
 * actually watch, and the self-test, live behind one disclosure under the ring
 * rather than behind six.
 */
export function Annunciator() {
    const [explaining, setExplaining] = useState(false);
    const list = sensors.value;

    if (list.length === 0) {
        return null;
    }

    const isArmed = armed.value;
    const trips = tripped.value;
    const placed = list.map((sensor, i) => ({
        sensor,
        state: capState(sensor, trips[sensor.name] !== undefined),
        station: station(i, list.length),
    }));

    // Where each one goes once the ring closes. Only meaningful while armed;
    // at rest every sensor stands at its own station whatever it is doing.
    const held = placed.filter((p) => p.state === 'watching' || p.state === 'ready');
    const outgoing = placed.filter((p) => place(p.state) === 'out');
    const outRows = Math.ceil(outgoing.length / PER_ROW);
    const slots = new Map(outgoing.map((p, j) => [p.sensor.name, outSlot(j, outgoing.length)]));

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
                <span class="readout">{isArmed ? 'Disarm to change' : 'Tap to turn on or off'}</span>
            </div>

            <div
                class="sring rise"
                data-gathered={isArmed}
                style={{ '--out-rows': outRows } as unknown as Record<string, string>}
            >
                <div class="sring-orbit">
                    {placed.map(({ sensor, state, station: at }, i) => {
                        const slot = slots.get(sensor.name);

                        return (
                            <button
                                key={sensor.name}
                                type="button"
                                class="snode"
                                role="switch"
                                aria-checked={sensor.enabled}
                                aria-disabled={!sensor.available}
                                aria-label={`${sensor.display_name} — ${state}`}
                                data-sensor={sensor.name}
                                data-state={state}
                                data-place={isArmed ? place(state) : 'station'}
                                style={
                                    {
                                        '--x': `${at.x}%`,
                                        '--y': `${at.y}%`,
                                        '--out-x': `${slot?.x ?? 50}%`,
                                        '--out-y': `${slot?.y ?? 118}%`,
                                        '--delay': `${i * 55}ms`,
                                    } as unknown as Record<string, string>
                                }
                                onClick={() => toggle(sensor)}
                            >
                                <SensorIcon name={sensor.name} />
                                <span class="snode-name">
                                    {SENSOR_CAPTIONS[sensor.name] ?? sensor.display_name}
                                </span>
                                <span class="snode-badge" aria-hidden="true">
                                    <StateBadge state={state} />
                                </span>
                            </button>
                        );
                    })}

                    {/* The stated reasons, which only exist once the ring has
                        closed and the excluded ones have somewhere to be. */}
                    {isArmed && outRows > 0 && (
                        <div class="sring-out" aria-hidden="true">
                            <div class="sring-out-head readout">Not watching</div>
                            {outgoing.map((p) => {
                                const slot = slots.get(p.sensor.name) ?? { x: 50, y: 118 };
                                return (
                                    <div
                                        key={p.sensor.name}
                                        class="sring-out-why"
                                        style={{ left: `${slot.x}%`, top: `${slot.y + 17}%` }}
                                    >
                                        <b>{SENSOR_CAPTIONS[p.sensor.name] ?? p.sensor.display_name}</b>
                                        <span>{whyNot(p.sensor)}</span>
                                    </div>
                                );
                            })}
                        </div>
                    )}

                    <div class="sring-core" data-tripped={placed.some((p) => p.state === 'tripped')}>
                        {/* Two outlines that hand over to each other rather than
                            stacking: at rest there is no ring at all, because a
                            closed shape would claim a completeness the state has
                            not got. Arming draws the shield in as the sensors
                            land inside it. */}
                        <svg class="core-shield" viewBox="0 0 100 100" aria-hidden="true">
                            <path d="M50 4 90 16v34c0 24-20 38-40 46C30 88 10 74 10 50V16z" />
                        </svg>

                        <span class="core-mark" aria-hidden="true">
                            <Icon name={isArmed ? 'eye' : 'eyeOff'} size={26} />
                        </span>
                        <span class="core-label readout">{isArmed ? 'watching' : 'not watching'}</span>
                        {!isArmed && <span class="core-hint readout">tap to arm</span>}

                        <span class="core-held" aria-hidden="true">
                            {held.map((p) => (
                                <span key={p.sensor.name} class="core-chip">
                                    <SensorIcon name={p.sensor.name} size={12} />
                                </span>
                            ))}
                        </span>
                    </div>
                </div>
            </div>

            <button
                type="button"
                class="sring-more readout"
                aria-expanded={explaining}
                onClick={() => setExplaining(!explaining)}
            >
                {explaining ? 'Hide what these watch' : 'What these sensors watch'}
            </button>

            {explaining && <Reference />}
        </section>
    );
}

/** Six stations on a true circle, however many sensors the laptop reports. */
function station(i: number, total: number) {
    const angle = (i / total) * 2 * Math.PI - Math.PI / 2;
    const radius = 40.5;
    return { x: 50 + radius * Math.cos(angle), y: 50 + radius * Math.sin(angle) };
}

/** How many excluded sensors fit on one row under the orbit. */
const PER_ROW = 3;

/**
 * Where an excluded sensor stands once it has left the ring.
 *
 * Rows of three, each row centred on whatever it holds, so two look deliberate
 * rather than left-aligned and one sits under the middle of the shield.
 */
function outSlot(index: number, total: number) {
    const row = Math.floor(index / PER_ROW);
    const inRow = Math.min(PER_ROW, total - row * PER_ROW);
    const column = index % PER_ROW;
    return {
        x: 50 + (column - (inRow - 1) / 2) * 30,
        y: 118 + row * 46,
    };
}

/** Which of the three destinations this sensor has once the ring closes. */
function place(state: string): 'in' | 'trip' | 'out' {
    if (state === 'watching') return 'in';
    if (state === 'tripped') return 'trip';
    return 'out';
}

/**
 * The one word for a sensor.
 *
 * The order is what the panel owes the user, most urgent first, and each answer
 * is only meaningful once the ones above it have been ruled out. A machine with
 * no such sensor says so before anything else, because "ready" and "off" would
 * both be claims about a sensor that is not there. A sensor that has tripped
 * says that ahead of a fault, since the trip is the thing the user came to
 * read. A sensor that is enabled but whose driver failed says "fault" rather
 * than "ready" — claiming cover it has not got is the one lie an alarm cannot
 * afford.
 */
function capState(sensor: SensorInfo, lit: boolean): string {
    if (!sensor.available) return 'unavailable';
    if (lit) return 'tripped';
    if (sensor.failure) return 'fault';
    if (!sensor.enabled) return 'off';
    return armed.value ? 'watching' : 'ready';
}

/** Why this one is standing outside the shield, in the user's own terms. */
function whyNot(sensor: SensorInfo): string {
    if (!sensor.available) return 'no sensor on this machine';
    if (sensor.failure) return 'its driver stopped answering';
    return 'you switched it off';
}

/**
 * What the sensors actually watch, and the self-test.
 *
 * One disclosure for all of them rather than an "i" on every station. The ring
 * has no corner to put six of those in, and the question this answers — what
 * does "USB" mean here — is asked once, on the first visit, about all of them
 * at the same time.
 */
function Reference() {
    return (
        <div class="sref rise">
            {sensors.value.map((sensor) => {
                const lit = tripped.value[sensor.name] !== undefined;
                const state = capState(sensor, lit);
                return (
                    <div key={sensor.name} class="sref-row" data-state={state}>
                        <div class="sref-head">
                            <span class="sref-mark" aria-hidden="true">
                                <SensorIcon name={sensor.name} size={15} />
                            </span>
                            <strong>{sensor.display_name}</strong>
                            <span class="sref-state readout">{state}</span>
                            <button
                                type="button"
                                class="chip"
                                disabled={!sensor.available}
                                onClick={() => send({ type: 'trigger_sensor', sensor: sensor.name })}
                            >
                                Test it
                            </button>
                        </div>
                        <p class="sref-desc">{SENSOR_DESCRIPTIONS[sensor.name] ?? ''}</p>
                        {!sensor.available && (
                            <p class="sref-warn">
                                This machine has no sensor for that, so it cannot be used.
                            </p>
                        )}
                        {sensor.available && sensor.failure && (
                            <p class="sref-warn">
                                This sensor is not watching right now: {sensor.failure}. The laptop is
                                restarting it, and the station clears when it comes back.
                            </p>
                        )}
                    </div>
                );
            })}
        </div>
    );
}
