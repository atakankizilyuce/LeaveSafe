import { useCallback, useEffect, useId, useState } from 'preact/hooks';
import { useDragDismiss } from '../lib/dragDismiss';
import { t } from '../lib/i18n';
import type { AppConfig, ClientMessage } from '../lib/protocol';
import { clearSession } from '../lib/session';
import {
    closeTransport,
    config,
    screen,
    send,
    serverVersion,
    setToken,
    settingsOpen,
    showToast,
    updateAvailable,
} from '../lib/store';
import { Icon } from './Icons';
import { LanguagePicker } from './LanguagePicker';
import { Scrim } from './Scrim';

/**
 * Settings as a sheet you pull up and dismiss, rather than an accordion that
 * pushes the panel off screen.
 *
 * Grouped by the question each group answers, and every field that only takes
 * effect after a restart says so on the field rather than in a legend.
 */
export function SettingsSheet() {
    const [draft, setDraft] = useState<AppConfig | null>(null);
    const [pin, setPin] = useState('');
    const [geoKey, setGeoKey] = useState('');
    const [saved, setSaved] = useState(false);

    const open = settingsOpen.value;
    const current = config.value;

    /**
     * Edits the user has made and not saved.
     *
     * Closing the sheet has always thrown these away. It matters more now that
     * the sheet can be flicked shut with a thumb, because that is a far easier
     * thing to do by accident than pressing Done — so the sheet says what it
     * just discarded rather than letting the settings quietly snap back.
     */
    // `saved` covers the gap between sending the change and the laptop echoing
    // its new config back, during which the draft still differs from what the
    // sheet has been told is current.
    const unsaved =
        !saved &&
        draft !== null &&
        current !== null &&
        (pin !== '' || geoKey !== '' || JSON.stringify(draft) !== JSON.stringify(current));

    const close = useCallback(() => {
        if (unsaved) showToast(t('Closed without saving'));
        settingsOpen.value = false;
    }, [unsaved]);

    const sheetRef = useDragDismiss(close, open);

    useEffect(() => {
        if (open) {
            send({ type: 'get_config' });
            setPin('');
            setGeoKey('');
            setSaved(false);
        }
    }, [open]);

    useEffect(() => {
        if (current) setDraft(structuredClone(current));
    }, [current]);

    if (!open) return null;

    const patch = (changes: Partial<AppConfig>) => {
        if (!draft) return;
        setDraft({ ...draft, ...changes });
    };

    const save = () => {
        if (!draft) return;

        const payload: Partial<AppConfig> = {
            ...draft,
            pin_protection: { enabled: draft.pin_protection.enabled, pin },
            location: { ...draft.location, geolocate_key: geoKey },
        };

        const message: ClientMessage = { type: 'update_config', config: payload };

        // Changing PIN protection while it is on needs the current PIN. Asking
        // only when it is actually required keeps the common case to one tap.
        const pinWasOn = current?.pin_protection.enabled ?? false;
        if (pinWasOn && (!draft.pin_protection.enabled || pin !== '')) {
            const confirmPin = window.prompt(t('Enter your current PIN to change PIN settings:'));
            if (!confirmPin) return;
            message.pin = confirmPin;
        }

        send(message);
        setSaved(true);
        showToast(t('Settings saved'));
        window.setTimeout(() => setSaved(false), 2000);
    };

    const reset = () => {
        if (!window.confirm(t('Put every setting back to its default?'))) return;
        if (current?.pin_protection.enabled) {
            const confirmPin = window.prompt(t('Enter your current PIN to confirm:'));
            if (!confirmPin) return;
            send({ type: 'reset_config', pin: confirmPin });
        } else {
            send({ type: 'reset_config' });
        }
        showToast(t('Settings reset'));
    };

    return (
        <Scrim onClose={close} label={t('Settings')}>
            <div class="sheet" ref={sheetRef}>
                {/* Grip and title in one strip, which stays put while the list
                    scrolls under it: the way out of a sheet this long should
                    not be something you have to scroll back to find. It is also
                    the part you can always drag, whatever the list is doing. */}
                <div class="sheet-handle">
                    <div class="sheet-grip" aria-hidden="true" />
                    <div class="sheet-head">
                        <h2 class="readout">{t('Settings')}</h2>
                        <button type="button" class="chip" onClick={close}>
                            {t('Done')}
                        </button>
                    </div>
                </div>

                {!draft ? (
                    <p class="empty">{t('Loading settings…')}</p>
                ) : (
                    <div class="sheet-body">
                        <LanguagePicker />
                        <Group title={t('Getting in')} note={t('How your phone reaches this laptop.')}>
                            <Num
                                label={t('Port')}
                                hint={t('restart required')}
                                value={draft.port}
                                min={0}
                                max={65535}
                                onChange={(port) => patch({ port })}
                            />
                        </Group>

                        <Group
                            title={t('Keeping others out')}
                            note={t('Limits on pairing attempts and sessions.')}
                        >
                            <Num
                                label={t('Phones at once')}
                                value={draft.max_sessions}
                                min={1}
                                max={10}
                                onChange={(max_sessions) => patch({ max_sessions })}
                            />
                            <Num
                                label={t('Wrong keys before lockout')}
                                value={draft.max_auth_attempts}
                                min={1}
                                max={20}
                                onChange={(max_auth_attempts) => patch({ max_auth_attempts })}
                            />
                            <Num
                                label={t('Lockout length')}
                                unit={t('seconds')}
                                value={draft.lockout_seconds}
                                min={10}
                                max={600}
                                onChange={(lockout_seconds) => patch({ lockout_seconds })}
                            />
                            <Toggle
                                label={t('Require a PIN to disarm')}
                                value={draft.pin_protection.enabled}
                                onChange={(enabled) =>
                                    patch({ pin_protection: { ...draft.pin_protection, enabled } })
                                }
                            />
                            {draft.pin_protection.enabled && (
                                <Text
                                    label={t('New PIN')}
                                    password
                                    placeholder={
                                        draft.pin_protection.has_pin
                                            ? t('Leave blank to keep the current PIN')
                                            : t('Choose a PIN')
                                    }
                                    value={pin}
                                    onChange={setPin}
                                />
                            )}
                        </Group>

                        <Group title={t('Watching')} note={t('How closely it watches, and when it starts.')}>
                            <Num
                                label={t('Status update every')}
                                unit={t('seconds')}
                                value={draft.heartbeat_seconds}
                                min={5}
                                max={60}
                                onChange={(heartbeat_seconds) => patch({ heartbeat_seconds })}
                            />
                            <Num
                                label={t('Wait after your phone drops')}
                                unit={t('seconds')}
                                value={draft.disconnect_grace_seconds}
                                min={5}
                                max={120}
                                onChange={(disconnect_grace_seconds) => patch({ disconnect_grace_seconds })}
                            />
                            <Num
                                label={t('Input sensitivity')}
                                unit={t('seconds')}
                                value={draft.input_threshold}
                                min={1}
                                max={10}
                                onChange={(input_threshold) => patch({ input_threshold })}
                            />
                            <Toggle
                                label={t('Arm itself when the screen locks')}
                                value={draft.auto_arm_on_lock}
                                onChange={(auto_arm_on_lock) => patch({ auto_arm_on_lock })}
                            />
                            <Toggle
                                label={t('Build the alarm up gradually')}
                                hint={t('Notify your phone first, then half volume, then full.')}
                                value={draft.alarm.escalation_enabled}
                                onChange={(escalation_enabled) => patch({ alarm: { escalation_enabled } })}
                            />
                        </Group>

                        <Group
                            title={t('Where it is')}
                            note={t(
                                'Off by default. With it off, nothing is scanned and no lookup leaves the laptop. Restart required.',
                            )}
                        >
                            <Toggle
                                label={t('Report its position while armed')}
                                value={draft.location.enabled}
                                onChange={(enabled) => patch({ location: { ...draft.location, enabled } })}
                            />
                            <Toggle
                                label={t("Use your phone's position when arming")}
                                hint={t('The most precise source, and it stays on your network.')}
                                value={draft.location.phone_anchor}
                                onChange={(phone_anchor) =>
                                    patch({ location: { ...draft.location, phone_anchor } })
                                }
                            />
                            <Toggle
                                label={t('Look up the public IP')}
                                hint={t('City level, roughly 25 km.')}
                                value={draft.location.ip_fallback}
                                onChange={(ip_fallback) =>
                                    patch({ location: { ...draft.location, ip_fallback } })
                                }
                            />
                            <Toggle
                                label={t('Use Wi-Fi positioning')}
                                hint={t('Around 30 m. Needs an API key. Not available on macOS.')}
                                value={draft.location.wifi_enabled}
                                onChange={(wifi_enabled) =>
                                    patch({ location: { ...draft.location, wifi_enabled } })
                                }
                            />
                            <Text
                                label={t('Geolocation API key')}
                                password
                                placeholder={
                                    draft.location.has_geolocate_key
                                        ? t('A key is set — leave blank to keep it')
                                        : t('No key set')
                                }
                                value={geoKey}
                                onChange={setGeoKey}
                            />
                            <Num
                                label={t('Update position every')}
                                unit={t('seconds')}
                                value={draft.location.poll_seconds}
                                min={15}
                                max={3600}
                                onChange={(poll_seconds) =>
                                    patch({ location: { ...draft.location, poll_seconds } })
                                }
                            />
                        </Group>

                        <Group
                            title={t('Updates')}
                            note={t(
                                'LeaveSafe asks GitHub whether a newer release exists. It never downloads or replaces anything.',
                            )}
                        >
                            <Toggle
                                label={t('Check for updates')}
                                value={draft.update_check}
                                onChange={(update_check) => patch({ update_check })}
                            />
                            {draft.update_check && (
                                <Select
                                    label={t('Channel')}
                                    hint={t('Beta means prereleases as well, not instead.')}
                                    value={draft.update_channel || 'stable'}
                                    options={[
                                        ['stable', t('Stable releases only')],
                                        ['beta', t('Include betas')],
                                    ]}
                                    onChange={(update_channel) => patch({ update_channel })}
                                />
                            )}
                            <UpdateStatus />
                        </Group>

                        <Group
                            title={t('When your phone is asleep')}
                            note={t(
                                'LeaveSafe talks straight to your laptop, with no cloud in between. That is also the limit: when your phone locks its screen or you switch apps, the operating system freezes this page and closes the connection, so an alert may not reach you until you open it again.',
                            )}
                        >
                            <p class="group-note">
                                {t(
                                    'The laptop does not depend on your phone. It keeps watching every sensor you enabled and sounds its own alarm whether or not a phone is connected — and losing the connection is not itself treated as an intrusion, so a phone in your pocket will not set it off.',
                                )}
                            </p>
                        </Group>

                        <div class="sheet-actions">
                            <button type="button" class="alarm-primary" onClick={save}>
                                <Icon name="check" />
                                {saved ? t('Saved') : t('Save settings')}
                            </button>
                            <button
                                type="button"
                                class="sheet-reset"
                                onClick={() => {
                                    clearSession();
                                    setToken(null);
                                    closeTransport();
                                    settingsOpen.value = false;
                                    screen.value = 'pair';
                                }}
                            >
                                {/* A chain with its middle gone: this breaks
                                    the pairing rather than undoing a setting. */}
                                <Icon name="unlink" />
                                {t('Forget this phone')}
                            </button>
                            <button type="button" class="sheet-reset" onClick={reset}>
                                <Icon name="undo" />
                                {t('Reset everything')}
                            </button>
                        </div>
                    </div>
                )}
            </div>
        </Scrim>
    );
}

/**
 * What the last check found, and what to run about it.
 *
 * The command comes from the laptop, which is the only side that knows whether
 * this copy arrived through Homebrew, Scoop, winget or a download. When it did
 * not recognise the installation there is no command to give, and the releases
 * page is offered instead of a guess.
 */
/**
 * The one place a server-supplied string reaches an href.
 *
 * The laptop already refuses to pass on a release URL that is not on
 * github.com, so this is the second lock on the same door — but it is the only
 * unchecked URL sink in the app.
 *
 * It holds the same line the laptop does rather than a weaker one. Refusing
 * only `javascript:` left every https host in the world allowed, so a link the
 * user taps from inside the app could lead anywhere — and the value arrives
 * over the socket, which is the one place this app takes strings from something
 * other than itself. Matching the server's rule exactly means the check is
 * still worth something if the server's ever slips.
 */
function safeHttpUrl(raw: string): string {
    const fallback = 'https://github.com/atakankizilyuce/LeaveSafe/releases/latest';
    try {
        const parsed = new URL(raw, window.location.origin);
        if (parsed.protocol !== 'https:' || parsed.hostname !== 'github.com') return fallback;
        return parsed.href;
    } catch {
        return fallback;
    }
}

function UpdateStatus() {
    const found = updateAvailable.value;
    const [copied, setCopied] = useState(false);

    if (!found) {
        return (
            <p class="group-note">
                {t('Running {version}. Nothing newer has been found.', {
                    version: serverVersion.value ?? t('an unknown version'),
                })}
            </p>
        );
    }

    const copy = () => {
        if (!found.command) return;
        // Clipboard access needs a secure context, which the plain-HTTP local
        // path is not. The command stays selectable either way, so a failure
        // costs nothing worth reporting.
        navigator.clipboard
            ?.writeText(found.command)
            .then(() => {
                setCopied(true);
                window.setTimeout(() => setCopied(false), 1500);
            })
            .catch(() => showToast(t('Could not copy — select the command instead')));
    };

    return (
        <>
            <Field label={t('Running')}>
                <span class="field-readout figure">{found.running}</span>
            </Field>
            <Field label={t('Available')}>
                <span class="field-readout figure">{found.latest}</span>
            </Field>
            {found.command ? (
                <div class="setting setting-stack">
                    <code class="update-command">{found.command}</code>
                    <button type="button" class="chip" onClick={copy}>
                        {copied ? t('Copied') : t('Copy')}
                    </button>
                </div>
            ) : (
                <p class="group-note">
                    <a href={safeHttpUrl(found.url)} target="_blank" rel="noreferrer noopener">
                        {t('Open the releases page')}
                    </a>{' '}
                    {t('Download it yourself. Nothing is installed for you.')}
                </p>
            )}
        </>
    );
}

function Group({
    title,
    note,
    children,
}: {
    title: string;
    note: string;
    children: preact.ComponentChildren;
}) {
    return (
        <section class="group">
            <h3 class="group-title readout">{title}</h3>
            <p class="group-note">{note}</p>
            {children}
        </section>
    );
}

/**
 * One row of the settings sheet.
 *
 * Rows holding a real form control associate with it explicitly through
 * for/id, so tapping the label text focuses the input. A toggle is a button
 * carrying its own aria-label — a <label> around a button labels nothing — so
 * those rows are a plain div instead.
 */
function Field({
    label,
    hint,
    htmlFor,
    children,
}: {
    label: string;
    hint?: string;
    htmlFor?: string;
    children: preact.ComponentChildren;
}) {
    const text = (
        <span class="setting-text">
            <span class="setting-label">{label}</span>
            {hint && <span class="setting-hint">{hint}</span>}
        </span>
    );

    if (!htmlFor) {
        return (
            <div class="setting">
                {text}
                {children}
            </div>
        );
    }

    return (
        <label class="setting" for={htmlFor}>
            {text}
            {children}
        </label>
    );
}

function Num({
    label,
    hint,
    unit,
    value,
    min,
    max,
    onChange,
}: {
    label: string;
    hint?: string;
    unit?: string;
    value: number;
    min: number;
    max: number;
    onChange(value: number): void;
}) {
    const id = useId();
    return (
        <Field label={label} hint={hint} htmlFor={id}>
            <span class="setting-num">
                <input
                    id={id}
                    class="field field-num figure"
                    type="number"
                    min={min}
                    max={max}
                    value={value}
                    onInput={(e) => onChange(Number((e.target as HTMLInputElement).value) || 0)}
                />
                {unit && <span class="readout">{unit}</span>}
            </span>
        </Field>
    );
}

function Text({
    label,
    hint,
    value,
    placeholder,
    password,
    onChange,
}: {
    label: string;
    hint?: string;
    value: string;
    placeholder?: string;
    password?: boolean;
    onChange(value: string): void;
}) {
    const id = useId();
    return (
        <Field label={label} hint={hint} htmlFor={id}>
            <input
                id={id}
                class="field field-text"
                type={password ? 'password' : 'text'}
                autoComplete="off"
                placeholder={placeholder}
                value={value}
                onInput={(e) => onChange((e.target as HTMLInputElement).value)}
            />
        </Field>
    );
}

function Select({
    label,
    hint,
    value,
    options,
    onChange,
}: {
    label: string;
    hint?: string;
    value: string;
    options: Array<[string, string]>;
    onChange(value: string): void;
}) {
    const id = useId();
    return (
        <Field label={label} hint={hint} htmlFor={id}>
            <select
                id={id}
                class="field field-select"
                value={value}
                onChange={(e) => onChange((e.target as HTMLSelectElement).value)}
            >
                {options.map(([v, text]) => (
                    <option key={v} value={v}>
                        {text}
                    </option>
                ))}
            </select>
        </Field>
    );
}

function Toggle({
    label,
    hint,
    value,
    onChange,
}: {
    label: string;
    hint?: string;
    value: boolean;
    onChange(value: boolean): void;
}) {
    return (
        <Field label={label} hint={hint}>
            <button
                type="button"
                class="switch"
                role="switch"
                aria-checked={value}
                aria-label={label}
                onClick={() => onChange(!value)}
            >
                <span class="switch-knob" />
            </button>
        </Field>
    );
}
