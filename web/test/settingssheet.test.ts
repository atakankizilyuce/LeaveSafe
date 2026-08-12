// @vitest-environment jsdom

import { h, render } from 'preact';
import { act } from 'preact/test-utils';
import { afterEach, beforeEach, expect, it, vi } from 'vitest';
import { SettingsSheet } from '../src/components/SettingsSheet';
import type { AppConfig } from '../src/lib/protocol';
import { config, send, settingsOpen, showToast, updateAvailable } from '../src/lib/store';

// The largest component in the phone interface, and the one holding the two
// decisions that are not about presentation at all: which link the update notice
// is allowed to point at, and when turning PIN protection off is allowed to
// happen without the current PIN.
//
// The link matters because its address arrives over the socket. That is the one
// place this app takes a string from something other than itself, and a tap on
// it opens whatever it says.

vi.mock('../src/lib/store', async (importOriginal) => {
    const actual = await importOriginal<typeof import('../src/lib/store')>();
    return { ...actual, send: vi.fn(), showToast: vi.fn() };
});

const sent = vi.mocked(send);
const toasted = vi.mocked(showToast);

/** A complete config, so the sheet renders its fields rather than its spinner. */
function aConfig(overrides: Partial<AppConfig> = {}): AppConfig {
    return {
        port: 9443,
        max_sessions: 3,
        max_auth_attempts: 5,
        lockout_seconds: 60,
        heartbeat_seconds: 10,
        disconnect_grace_seconds: 30,
        auto_arm_on_lock: false,
        input_threshold: 3,
        connection_mode: 'local',
        update_check: true,
        remote_access: false,
        remote_port: 9444,
        alarm: { escalation_enabled: false },
        pin_protection: { enabled: false },
        location: {
            enabled: false,
            poll_seconds: 60,
            phone_anchor: false,
            ip_fallback: false,
            wifi_enabled: false,
            has_geolocate_key: false,
        },
        ...overrides,
    };
}

let host: HTMLDivElement;

/** Opens the sheet against the given saved config and renders it. */
function open(cfg: AppConfig = aConfig()) {
    config.value = cfg;
    settingsOpen.value = true;
    act(() => {
        render(h(SettingsSheet, {}), host);
    });
}

/** Clicks the button whose label reads exactly this. */
function click(label: string) {
    const button = [...host.querySelectorAll('button')].find((b) => b.textContent === label);
    if (!button) throw new Error(`no button labelled ${label}`);
    act(() => {
        button.click();
    });
}

/** The message the sheet last asked the laptop to act on. */
function lastSent() {
    return sent.mock.calls.at(-1)?.[0];
}

beforeEach(() => {
    sent.mockClear();
    toasted.mockClear();
    config.value = null;
    updateAvailable.value = null;
    settingsOpen.value = false;
    host = document.createElement('div');
    document.body.appendChild(host);
});

afterEach(() => {
    render(null, host);
    host.remove();
    settingsOpen.value = false;
    config.value = null;
    updateAvailable.value = null;
    vi.unstubAllGlobals();
});

it('is not on the screen until it is opened', () => {
    act(() => {
        render(h(SettingsSheet, {}), host);
    });

    expect(host.querySelector('.sheet')).toBeNull();
});

it('asks the laptop for the current settings when it opens', () => {
    open();

    expect(sent).toHaveBeenCalledWith({ type: 'get_config' });
});

// ── The update link ──────────────────────────────────────────────────────────
//
// Every case below is the same tap by the user. What differs is what the laptop
// said the address was.

/** Renders the sheet with an update whose link is `url`, and reads the href. */
function updateLinkFor(url: string): string | undefined {
    updateAvailable.value = { running: '1.0.0', latest: '1.1.0', url, channel: 'stable' };
    open();
    return host.querySelector('a')?.getAttribute('href') ?? undefined;
}

const FALLBACK = 'https://github.com/atakankizilyuce/LeaveSafe/releases/latest';

it('offers a github release link as given', () => {
    expect(updateLinkFor('https://github.com/atakankizilyuce/LeaveSafe/releases/tag/v1.1.0')).toBe(
        'https://github.com/atakankizilyuce/LeaveSafe/releases/tag/v1.1.0',
    );
});

it('refuses a javascript: url', () => {
    expect(updateLinkFor('javascript:alert(document.cookie)')).toBe(FALLBACK);
});

// https only. A plain-http github link is still github, and still readable by
// anyone on the path between the phone and it.
it('refuses a link that is not https', () => {
    expect(updateLinkFor('http://github.com/atakankizilyuce/LeaveSafe/releases')).toBe(FALLBACK);
});

// The host check is the point. Allowing any https address would mean a laptop
// that has been talked into sending a different one can send the user anywhere.
it('refuses an https link to somewhere that is not github', () => {
    expect(updateLinkFor('https://github.com.example.invalid/releases')).toBe(FALLBACK);
});

it('refuses a data: url', () => {
    expect(updateLinkFor('data:text/html,<script>alert(1)</script>')).toBe(FALLBACK);
});

it('refuses something that is not a url at all', () => {
    expect(updateLinkFor('not a url')).toBe(FALLBACK);
});

// A relative path resolves against the app's own origin, which is not github —
// so it takes the fallback rather than pointing back into the phone interface.
it('refuses a relative path', () => {
    expect(updateLinkFor('/releases/latest')).toBe(FALLBACK);
});

it('opens the release link in a tab that cannot reach back', () => {
    updateLinkFor('https://github.com/atakankizilyuce/LeaveSafe/releases/latest');

    const link = host.querySelector('a');
    expect(link?.getAttribute('target')).toBe('_blank');
    expect(link?.getAttribute('rel')).toContain('noopener');
    expect(link?.getAttribute('rel')).toContain('noreferrer');
});

// ── Whether remote access is actually reachable ──────────────────────────────
//
// A user who turns this on and gets nothing has to be able to tell a router that
// refused the port mapping from an internet provider that cannot offer one at
// all. The two need entirely different responses — one is a page in the router's
// admin, the other is nothing the user can do — so saying the wrong one sends
// someone to spend an evening in a settings page that was never the problem.

/** Opens the sheet with remote access on and the laptop reporting `state`. */
function remoteState(state: AppConfig['remote_state']) {
    open(aConfig({ remote_access: true, remote_state: state }));
}

it('says it is still checking before the answer is in', () => {
    remoteState({ enabled: true, probing: true });

    expect(host.textContent).toContain('Checking whether it can be reached');
});

it('names carrier-grade NAT as something nothing on the laptop can fix', () => {
    remoteState({ enabled: true, upnp: 'cgnat' });

    expect(host.textContent).toContain('carrier-grade NAT');
    expect(host.textContent).toContain('The local network still works normally.');
    expect(host.textContent).not.toContain("router's admin page");
});

it('sends the user to the router when the router is what refused', () => {
    remoteState({ enabled: true, upnp: 'failed', manual_port: 9444 });

    expect(host.textContent).toContain("router's admin page");
    expect(host.textContent).toContain('9444');
    expect(host.textContent).not.toContain('carrier-grade NAT');
});

// "Reachable at" is a claim. With the mapping refused the address is a
// destination the user has to make work rather than one that does, and labelling
// it the same way was the app asserting the opposite of what it had found out.
it('does not call an address reachable when nothing will carry a connection to it', () => {
    remoteState({ enabled: true, upnp: 'failed', manual_port: 9444, public_url: 'https://1.2.3.4:9444' });

    expect(host.textContent).toContain('Will be reachable at');
    expect(host.textContent).not.toContain('Reachable at');
});

it('calls it reachable once something has been seen to reach it', () => {
    remoteState({ enabled: true, upnp: 'ok', reach: 'verified', public_url: 'https://1.2.3.4:9444' });

    expect(host.textContent).toContain('Reachable at');
    expect(host.textContent).toContain('https://1.2.3.4:9444');
});

// A mapping the router accepted is an agreement, not a delivered packet: the
// laptop's own firewall can still drop the connection. This used to read
// "Reachable at" on the strength of the agreement alone.
it('will not call it reachable on the strength of a port mapping alone', () => {
    remoteState({ enabled: true, upnp: 'ok', reach: 'unproven', public_url: 'https://1.2.3.4:9444' });

    expect(host.textContent).toContain('Possibly reachable at');
    expect(host.textContent).toContain('nothing could be confirmed to reach the laptop');
    // Still offered, because it may well work — the check is the thing that
    // failed, not necessarily the address.
    expect(host.textContent).toContain('https://1.2.3.4:9444');
});

// A second router in front of the one holding the mapping. Unlike carrier-grade
// NAT this is the user's to fix, so it says which box to go and fix it on.
it('names the outer router when the mapping stops one hop short', () => {
    remoteState({
        enabled: true,
        upnp: 'ok',
        reach: 'blocked',
        manual_port: 9444,
        public_url: 'https://1.2.3.4:9444',
    });

    expect(host.textContent).toContain('itself behind another router');
    expect(host.textContent).toContain('9444');
    expect(host.textContent).not.toContain('Reachable at');
});

it('says nothing about a public address when there is none', () => {
    remoteState({ enabled: true, upnp: 'ok' });

    expect(host.textContent).toContain('No public address found.');
});

it('says nothing at all about reachability while remote access is off', () => {
    open(aConfig({ remote_access: false }));

    expect(host.textContent).not.toContain('No public address found.');
    expect(host.textContent).not.toContain('Checking whether it can be reached');
});

// ── The upgrade command ──────────────────────────────────────────────────────

/** Opens the sheet with an update the laptop knows the upgrade command for. */
function updateWithCommand(command: string) {
    updateAvailable.value = {
        running: '1.0.0',
        latest: '1.1.0',
        url: 'https://github.com/atakankizilyuce/LeaveSafe/releases/latest',
        channel: 'stable',
        command,
    };
    open();
}

it('offers the upgrade command rather than a link when it knows one', () => {
    updateWithCommand('winget upgrade LeaveSafe');

    expect(host.querySelector('.update-command')?.textContent).toBe('winget upgrade LeaveSafe');
    expect(host.querySelector('a')).toBeNull();
});

it('copies the upgrade command and says it did', async () => {
    const writeText = vi.fn().mockResolvedValue(undefined);
    vi.stubGlobal('navigator', { clipboard: { writeText } });
    updateWithCommand('brew upgrade leavesafe');

    click('Copy');
    await act(async () => {});

    expect(writeText).toHaveBeenCalledWith('brew upgrade leavesafe');
    expect([...host.querySelectorAll('button')].some((b) => b.textContent === 'Copied')).toBe(true);
});

// The clipboard needs a secure context, which the plain-HTTP local path is not.
// The command stays selectable either way, so the failure is worth a sentence
// rather than an error.
it('says what to do instead when the clipboard refuses', async () => {
    vi.stubGlobal('navigator', { clipboard: { writeText: vi.fn().mockRejectedValue(new Error('no')) } });
    updateWithCommand('apt upgrade leavesafe');

    click('Copy');
    await act(async () => {});

    expect(toasted).toHaveBeenCalledWith('Could not copy — select the command instead');
});

it('says which version is running when there is nothing newer', () => {
    open();

    expect(host.textContent).toContain('Nothing newer has been found.');
});

// ── Turning PIN protection off ───────────────────────────────────────────────

/** Flips the PIN protection toggle in the rendered sheet. */
function togglePinProtection() {
    const toggle = [...host.querySelectorAll('button')].find(
        (b) => b.getAttribute('aria-label') === 'Require a PIN to disarm',
    );
    if (!toggle) throw new Error('the PIN protection toggle is not on screen');
    act(() => {
        toggle.click();
    });
}

it('does not ask for a PIN when there was none to begin with', () => {
    open(aConfig({ pin_protection: { enabled: false } }));
    const prompt = vi.fn();
    vi.stubGlobal('prompt', prompt);

    click('Save settings');

    expect(prompt).not.toHaveBeenCalled();
    expect(lastSent()).toMatchObject({ type: 'update_config' });
});

// Turning the protection off is the change that matters: without this gate,
// whoever is holding the phone could remove the PIN without knowing it.
it('asks for the current PIN before turning PIN protection off', () => {
    open(aConfig({ pin_protection: { enabled: true, has_pin: true } }));
    const prompt = vi.fn().mockReturnValue('1234');
    vi.stubGlobal('prompt', prompt);

    togglePinProtection();
    click('Save settings');

    expect(prompt).toHaveBeenCalledTimes(1);
    expect(lastSent()).toMatchObject({ type: 'update_config', pin: '1234' });
});

// Dismissing that prompt has to abandon the whole save. Sending the rest of the
// settings anyway would turn a cancelled change into a partial one.
it('sends nothing when the PIN confirmation is dismissed', () => {
    open(aConfig({ pin_protection: { enabled: true, has_pin: true } }));
    vi.stubGlobal('prompt', vi.fn().mockReturnValue(null));
    sent.mockClear();

    togglePinProtection();
    click('Save settings');

    expect(sent).not.toHaveBeenCalled();
});

it('treats an empty answer to the PIN prompt as a cancellation', () => {
    open(aConfig({ pin_protection: { enabled: true, has_pin: true } }));
    vi.stubGlobal('prompt', vi.fn().mockReturnValue(''));
    sent.mockClear();

    togglePinProtection();
    click('Save settings');

    expect(sent).not.toHaveBeenCalled();
});

// ── Putting everything back ──────────────────────────────────────────────────

it('does nothing when the reset is not confirmed', () => {
    open();
    vi.stubGlobal('confirm', vi.fn().mockReturnValue(false));
    sent.mockClear();

    click('Reset everything');

    expect(sent).not.toHaveBeenCalled();
});

it('resets when it is confirmed', () => {
    open();
    vi.stubGlobal('confirm', vi.fn().mockReturnValue(true));
    sent.mockClear();

    click('Reset everything');

    expect(lastSent()).toEqual({ type: 'reset_config' });
});

it('asks for the PIN before resetting a protected laptop', () => {
    open(aConfig({ pin_protection: { enabled: true, has_pin: true } }));
    vi.stubGlobal('confirm', vi.fn().mockReturnValue(true));
    vi.stubGlobal('prompt', vi.fn().mockReturnValue('4321'));
    sent.mockClear();

    click('Reset everything');

    expect(lastSent()).toEqual({ type: 'reset_config', pin: '4321' });
});

it('sends no reset when that PIN prompt is dismissed', () => {
    open(aConfig({ pin_protection: { enabled: true, has_pin: true } }));
    vi.stubGlobal('confirm', vi.fn().mockReturnValue(true));
    vi.stubGlobal('prompt', vi.fn().mockReturnValue(null));
    sent.mockClear();

    click('Reset everything');

    expect(sent).not.toHaveBeenCalled();
});

// ── Closing on unsaved edits ─────────────────────────────────────────────────

// The sheet can be flicked shut with a thumb, which is a far easier thing to do
// by accident than pressing Done — so it says what it just threw away rather
// than letting the settings quietly snap back.
it('says so when it is closed with edits that were never saved', () => {
    open();
    togglePinProtection();

    click('Done');

    expect(toasted).toHaveBeenCalledWith('Closed without saving');
    expect(settingsOpen.value).toBe(false);
});

it('closes quietly when nothing was changed', () => {
    open();

    click('Done');

    expect(toasted).not.toHaveBeenCalledWith('Closed without saving');
    expect(settingsOpen.value).toBe(false);
});
