import { stopSiren } from '../lib/siren';
import { alarm, send } from '../lib/store';
import { Icon } from './Icons';

/**
 * What you see when something touched your laptop.
 *
 * Three ways out, in the order you are likely to want them: it was nothing, it
 * was nothing and that sensor will keep firing for the next few seconds, or
 * that sensor is wrong and should stop.
 */
export function AlarmOverlay() {
    const active = alarm.value;
    if (!active) return null;

    const dismiss = (type: string, extra: Record<string, unknown> = {}) => {
        alarm.value = null;
        stopSiren();
        send({ type, sensor: active.sensor, ...extra });
    };

    return (
        <div class="alarm" role="alertdialog" aria-modal="true" aria-label="Security alert">
            <div class="alarm-field" aria-hidden="true" />
            <div class="alarm-body">
                <div class="alarm-eyebrow readout">Alert</div>
                <p class="alarm-msg">{active.message}</p>
                <div class="alarm-actions">
                    {/* A crossed-out bell, not a tick. This button does not
                        agree with the alert — it stops the noise. */}
                    <button type="button" class="alarm-primary" onClick={() => dismiss('dismiss_alarm')}>
                        <Icon name="bellOff" />
                        Dismiss
                    </button>
                    <button
                        type="button"
                        class="chip"
                        onClick={() => dismiss('dismiss_alarm_pause', { duration: 5 })}
                    >
                        <Icon name="pause" size={13} />
                        Pause this sensor 5s
                    </button>
                    <button type="button" class="chip" onClick={() => dismiss('dismiss_alarm_disable')}>
                        <Icon name="power" size={13} />
                        Stop using this sensor
                    </button>
                </div>
            </div>
        </div>
    );
}
