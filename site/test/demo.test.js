/**
 * @vitest-environment jsdom
 *
 * The landing page's demonstration.
 *
 * Nothing on it is connected to anything, which does not make it untestable —
 * it makes it the only part of the page that can be wrong. Two of these tests
 * exist because it was: the disarm that never landed when frames were throttled,
 * and the release that re-armed the panel a moment after disarming it.
 *
 * The markup comes out of index.html rather than a fixture written here. A test
 * against a hand-copied panel would keep passing after someone renamed an id,
 * which is the one mistake it is in a position to catch.
 */

import { readFileSync } from 'node:fs';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { initDemo, preferredOs } from '../demo.js';

const here = dirname(fileURLToPath(import.meta.url));
const page = readFileSync(join(here, '..', 'index.html'), 'utf8');
const body = page.slice(page.indexOf('<body>') + '<body>'.length, page.indexOf('</body>'));

const $ = (id) => document.getElementById(id);
const node = (sensor) => document.querySelector(`.node[data-sensor="${sensor}"]`);
const mini = (sensor) => document.querySelector(`.core-icons .ico[data-sensor="${sensor}"]`);
const state = () => document.documentElement.dataset.state ?? 'standby';

/** A pointer press on an element, the way a browser sequences one. */
function press(el) {
    el.dispatchEvent(new window.Event('pointerdown', { bubbles: true, cancelable: true }));
}
function release(el, kind = 'pointerup') {
    el.dispatchEvent(new window.Event(kind, { bubbles: true }));
    el.dispatchEvent(new window.Event('click', { bubbles: true }));
}

beforeEach(() => {
    document.body.innerHTML = body;
    document.documentElement.removeAttribute('data-state');
    document.documentElement.removeAttribute('data-count');
    vi.useFakeTimers();
});

afterEach(() => {
    vi.useRealTimers();
    vi.restoreAllMocks();
});

describe('the panel at rest', () => {
    it('starts in standby, counting every sensor and offering none to fire', () => {
        initDemo();

        expect(state()).toBe('standby');
        expect($('p-state').textContent).toBe('STANDBY');
        expect($('p-sub').textContent).toBe('6 sensors ready');
        expect($('core-word').textContent).toBe('NOT WATCHING');
        expect($('p-arm-label').textContent).toBe('Arm');
        expect($('btn-trigger').disabled).toBe(true);
        expect(document.querySelector('.core-eye use').getAttribute('href')).toBe('#i-eye-off');
    });
});

describe('arming', () => {
    it('counts three seconds down and lands armed', () => {
        initDemo();
        $('p-arm').click();

        expect(state()).toBe('arming');
        expect($('p-arm-label').textContent).toBe('Cancel');
        expect($('core-note').textContent).toBe('TAP TO CANCEL');

        vi.advanceTimersByTime(0);
        expect(document.documentElement.dataset.count).toBe('3');
        expect($('p-state').textContent).toBe('ARMING 3');
        expect($('p-sub').textContent).toBe('6 sensors · 3s');

        vi.advanceTimersByTime(1000);
        expect(document.documentElement.dataset.count).toBe('2');

        vi.advanceTimersByTime(1000);
        expect(document.documentElement.dataset.count).toBe('1');

        vi.advanceTimersByTime(1000);
        expect(state()).toBe('armed');
        expect(document.documentElement.dataset.count).toBeUndefined();
        expect($('p-state').textContent).toBe('ARMED');
        expect($('p-sub').textContent).toBe('Watching 6 sensors · just now');
        expect($('p-arm-label').textContent).toBe('Hold to disarm');
        expect($('btn-trigger').disabled).toBe(false);
        expect(document.querySelector('.core-eye use').getAttribute('href')).toBe('#i-eye');
    });

    // The countdown exists so the tap can be taken back. A cancel that did not
    // stop the pending timers would arm the panel anyway, three seconds later.
    it('can be taken back, and then stays taken back', () => {
        initDemo();
        $('p-arm').click();
        vi.advanceTimersByTime(1200);

        $('p-arm').click();
        expect(state()).toBe('standby');

        vi.advanceTimersByTime(5000);
        expect(state()).toBe('standby');
    });

    it('spends the invitation on the first tap', () => {
        initDemo();
        const phone = document.querySelector('.phone');
        expect(phone.dataset.untouched).toBe('');

        $('p-arm').click();
        expect(phone.dataset.untouched).toBeUndefined();
    });
});

describe('the stations', () => {
    it('switches a sensor off, drops it from the count and from the shield', () => {
        initDemo();

        node('lid').click();

        expect(node('lid').dataset.off).toBe('');
        expect(mini('lid').dataset.off).toBe('');
        expect($('p-sub').textContent).toBe('5 sensors ready');
    });

    it('switches it back on again', () => {
        initDemo();

        node('lid').click();
        node('lid').click();

        expect(node('lid').dataset.off).toBeUndefined();
        expect(mini('lid').dataset.off).toBeUndefined();
        expect($('p-sub').textContent).toBe('6 sensors ready');
    });

    // Changing what is watched while it is being watched is the confusion the
    // product avoids, so the demonstration avoids it too.
    it('refuses to change while armed', () => {
        initDemo();
        $('p-arm').click();
        vi.advanceTimersByTime(3000);

        node('lid').click();

        expect(node('lid').dataset.off).toBeUndefined();
    });

    // A station whose icon is missing from the shield must not take the sync
    // down with it — the panel is still telling the truth about the rest.
    it('survives a station with no icon in the shield', () => {
        mini('usb').remove();
        initDemo();

        expect(() => node('lid').click()).not.toThrow();
        expect(mini('lid').dataset.off).toBe('');
    });
});

describe('the alarm', () => {
    const arm = () => {
        $('p-arm').click();
        vi.advanceTimersByTime(3000);
    };

    it('fires a sensor, lights it in the shield and names it in the alert', () => {
        initDemo();
        arm();

        $('btn-trigger').click();

        expect($('p-alert').hidden).toBe(false);
        expect($('p-alert-sensor').textContent).toBe('POWER');
        expect($('p-alert-msg').textContent).toBe('Charger unplugged');
        expect(node('power').dataset.tripped).toBe('');
        expect(mini('power').dataset.tripped).toBe('');
    });

    // Pressing it three times should show three different sensors: the point of
    // the button is that any of them can wake you.
    it('goes round the ring rather than repeating itself', () => {
        initDemo();
        arm();

        const fired = [];
        for (let i = 0; i < 3; i++) {
            $('btn-trigger').click();
            fired.push($('p-alert-sensor').textContent);
            $('p-dismiss').click();
        }

        expect(new Set(fired).size).toBe(3);
        expect(fired).toEqual(['POWER', 'LID', 'USB']);
    });

    it('lights only the sensor that fired', () => {
        initDemo();
        arm();

        $('btn-trigger').click();
        $('p-dismiss').click();
        $('btn-trigger').click();

        expect(document.querySelectorAll('.node[data-tripped]')).toHaveLength(1);
        expect(node('lid').dataset.tripped).toBe('');
    });

    it('has nothing to fire when every sensor is off', () => {
        initDemo();
        for (const s of ['power', 'lid', 'usb', 'screen', 'network', 'input']) node(s).click();
        arm();

        $('btn-trigger').click();

        expect($('p-alert').hidden).toBe(true);
    });

    it('is dismissed without disarming', () => {
        initDemo();
        arm();
        $('btn-trigger').click();

        $('p-dismiss').click();

        expect($('p-alert').hidden).toBe(true);
        expect(state()).toBe('armed');
    });
});

describe('holding to disarm', () => {
    const arm = () => {
        $('p-arm').click();
        vi.advanceTimersByTime(3000);
    };

    /*
     * The first version stepped the fill with requestAnimationFrame and disarmed
     * when it reached the end. Throttled frames — a background tab — stopped the
     * callback arriving, so the bar froze partway and the disarm never landed
     * however long the button was held. A timer is what makes this test possible
     * to write at all: there are no frames here.
     */
    it('disarms after a second and a half, with no frames produced', () => {
        initDemo();
        arm();

        press($('p-arm'));
        expect($('p-arm-fill').style.width).toBe('100%');

        vi.advanceTimersByTime(1500);
        expect(state()).toBe('standby');
        expect($('p-arm-fill').style.width).toBe('0px');
    });

    it('does not disarm when the press is let go early', () => {
        initDemo();
        arm();

        press($('p-arm'));
        vi.advanceTimersByTime(600);
        release($('p-arm'));

        vi.advanceTimersByTime(3000);
        expect(state()).toBe('armed');
        expect($('p-arm-fill').style.width).toBe('0px');
    });

    it.each(['pointerup', 'pointerleave', 'pointercancel'])('a %s ends the hold', (kind) => {
        initDemo();
        arm();

        press($('p-arm'));
        $('p-arm').dispatchEvent(new window.Event(kind, { bubbles: true }));
        vi.advanceTimersByTime(3000);

        expect(state()).toBe('armed');
    });

    // Lifting the finger after a completed hold fires a click on a button that
    // is now in standby, where a click means arm. Left alone the panel disarmed
    // and re-armed itself in the same gesture.
    it('does not re-arm when the finger comes up', () => {
        initDemo();
        arm();

        press($('p-arm'));
        vi.advanceTimersByTime(1500);
        release($('p-arm'));

        expect(state()).toBe('standby');
        vi.advanceTimersByTime(3000);
        expect(state()).toBe('standby');
    });

    // ...and the swallowed click must be spent, not held against the next one.
    it('arms normally on the tap after that', () => {
        initDemo();
        arm();

        press($('p-arm'));
        vi.advanceTimersByTime(1500);
        release($('p-arm'));

        $('p-arm').click();
        vi.advanceTimersByTime(3000);
        expect(state()).toBe('armed');
    });

    it('ignores a press while it is not armed', () => {
        initDemo();

        press($('p-arm'));
        vi.advanceTimersByTime(3000);

        expect(state()).toBe('standby');
        expect($('p-arm-fill').style.width).toBe('0px');
    });

    it('ignores a second press while one is already being held', () => {
        initDemo();
        arm();

        press($('p-arm'));
        vi.advanceTimersByTime(400);
        press($('p-arm'));

        // The second press must not restart the clock: the disarm still lands
        // 1500ms after the first.
        vi.advanceTimersByTime(1100);
        expect(state()).toBe('standby');
    });

    // The station that fired stays lit after disarming, which is how you find
    // out what happened while you were away.
    it('leaves the station that fired still lit', () => {
        initDemo();
        arm();
        $('btn-trigger').click();
        $('p-dismiss').click();

        press($('p-arm'));
        vi.advanceTimersByTime(1500);

        expect(state()).toBe('standby');
        expect(node('power').dataset.tripped).toBe('');
    });
});

describe('the install tabs', () => {
    it('shows one panel at a time and says which', () => {
        initDemo();

        $('t-win').click();

        expect($('tab-win').hidden).toBe(false);
        expect($('tab-brew').hidden).toBe(true);
        expect($('tab-bin').hidden).toBe(true);
        expect($('t-win').getAttribute('aria-selected')).toBe('true');
        expect($('t-brew').getAttribute('aria-selected')).toBe('false');
        expect($('t-win').classList.contains('is-on')).toBe(true);
    });
});

describe('copying a command', () => {
    const copyButton = () => document.querySelector('#tab-brew .copy');

    it('copies the command beside it and says so, then stops saying so', async () => {
        const writeText = vi.fn().mockResolvedValue(undefined);
        vi.stubGlobal('navigator', { clipboard: { writeText } });
        initDemo();

        copyButton().click();
        await vi.waitFor(() => expect(writeText).toHaveBeenCalled());

        expect(writeText).toHaveBeenCalledWith('brew tap atakankizilyuce/tap && brew install leavesafe');
        expect(copyButton().classList.contains('is-done')).toBe(true);
        expect(copyButton().querySelector('use').getAttribute('href')).toBe('#i-check');

        vi.advanceTimersByTime(1400);
        expect(copyButton().classList.contains('is-done')).toBe(false);
        expect(copyButton().querySelector('use').getAttribute('href')).toBe('#i-copy');
    });

    // A refused clipboard is not something the visitor can act on, so it is not
    // reported — but it must not leave the button claiming it copied either.
    it('says nothing at all when the clipboard refuses', async () => {
        const writeText = vi.fn().mockRejectedValue(new Error('denied'));
        vi.stubGlobal('navigator', { clipboard: { writeText } });
        initDemo();

        copyButton().click();
        await vi.waitFor(() => expect(writeText).toHaveBeenCalled());

        expect(copyButton().classList.contains('is-done')).toBe(false);
        expect(copyButton().querySelector('use').getAttribute('href')).toBe('#i-copy');
    });
});

/*
 * The laptop.
 *
 * The demonstration was a phone on its own for as long as it existed, which
 * taught the wrong thing — that LeaveSafe is a phone app. It is a program on
 * the laptop, and the laptop is the end that makes the noise. These tests are
 * mostly about that last sentence.
 */
const lapLines = () => Array.from($('lap-log').children).map((li) => li.textContent);

describe('the laptop', () => {
    it('starts not armed, counting the same sensors the phone counts', () => {
        initDemo();

        expect($('lap-state').textContent).toBe('NOT ARMED');
        expect($('lap-sensors').textContent).toBe('6 ready');
        expect($('lap-siren').hidden).toBe(true);
    });

    /*
     * The page used to carry a badge reading "this is live". The disclosure is
     * the window's own first line now, in the voice of the thing being
     * demonstrated, plus a build tag in its title bar that never scrolls away.
     */
    it('opens by saying what it is, rather than claiming it was just disarmed', () => {
        initDemo();

        expect(lapLines()).toHaveLength(1);
        expect(lapLines()[0]).toContain('demo build · not watching a real machine');
        expect(document.querySelector('.lap-tag').textContent).toBe('demo');
    });

    it('counts down beside the phone and lands watching', () => {
        initDemo();
        $('p-arm').click();

        vi.advanceTimersByTime(0);
        expect($('lap-state').textContent).toBe('ARMING 3');

        vi.advanceTimersByTime(3000);
        expect($('lap-state').textContent).toBe('WATCHING');
        expect(lapLines().at(-1)).toContain('armed · watching 6 sensors');
    });

    it('follows the sensors being switched off', () => {
        initDemo();
        node('usb').click();

        expect($('lap-sensors').textContent).toBe('5 ready');
    });

    it('sounds when a sensor fires, and says the phone is sounding too', () => {
        initDemo();
        $('p-arm').click();
        vi.advanceTimersByTime(3000);

        $('btn-trigger').click();

        expect($('lap-state').textContent).toBe('ALARM');
        expect($('lap-siren').hidden).toBe(false);
        expect(lapLines().at(-2)).toContain('POWER');
        expect(lapLines().at(-1)).toContain('sounding here, and on the paired phone');
        // And the other end says the same thing about this one.
        expect($('p-alert').hidden).toBe(false);
        expect(document.querySelector('.p-alert-both').textContent).toContain('laptop');
    });

    it('goes quiet when the alarm is dismissed, and keeps watching', () => {
        initDemo();
        $('p-arm').click();
        vi.advanceTimersByTime(3000);
        $('btn-trigger').click();

        $('p-dismiss').click();

        expect($('lap-siren').hidden).toBe(true);
        expect($('lap-state').textContent).toBe('WATCHING');
        expect(lapLines().at(-1)).toContain('still watching');
    });

    it('stops sounding when the phone disarms it', () => {
        initDemo();
        $('p-arm').click();
        vi.advanceTimersByTime(3000);
        $('btn-trigger').click();

        press($('p-arm'));
        vi.advanceTimersByTime(1500);

        expect($('lap-state').textContent).toBe('NOT ARMED');
        expect($('lap-siren').hidden).toBe(true);
        expect(lapLines().at(-1)).toContain('disarmed from the phone');
    });

    /*
     * A log that grows moves everything under it every time a line lands, and
     * the rig walks about beneath the cursor of whoever is pressing its
     * buttons.
     */
    it('keeps five lines and drops the oldest', () => {
        initDemo();
        $('p-arm').click();
        vi.advanceTimersByTime(3000);
        $('btn-trigger').click();
        $('p-dismiss').click();
        $('btn-trigger').click();

        expect(lapLines()).toHaveLength(5);
        expect(lapLines()[0]).not.toContain('demo build');
    });
});

describe('which install tab opens', () => {
    it('reads the platform out of the user agent', () => {
        expect(preferredOs('Mozilla/5.0 (Windows NT 10.0; Win64; x64)')).toBe('windows');
        expect(preferredOs('Mozilla/5.0 (win32) jsdom/26')).toBe('windows');
        expect(preferredOs('Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7)')).toBe('mac');
        expect(preferredOs('Mozilla/5.0 (X11; Linux x86_64)')).toBe('mac');
        expect(preferredOs('Mozilla/5.0 (PlayStation 5)')).toBe('other');
    });

    it('does not fall over when there is no user agent to read', () => {
        expect(preferredOs(undefined)).toBe('other');
        expect(preferredOs('')).toBe('other');
    });

    it('opens the tab for the machine that asked for the page', () => {
        vi.stubGlobal('navigator', { userAgent: 'Mozilla/5.0 (Windows NT 10.0; Win64; x64)' });
        initDemo();

        expect($('t-win').getAttribute('aria-selected')).toBe('true');
        expect($('tab-win').hidden).toBe(false);
        expect($('t-brew').getAttribute('aria-selected')).toBe('false');
        expect($('tab-brew').hidden).toBe(true);
    });

    it('leaves the markup to choose for a platform it does not know', () => {
        vi.stubGlobal('navigator', { userAgent: 'Mozilla/5.0 (PlayStation 5)' });
        initDemo();

        expect($('t-bin').getAttribute('aria-selected')).toBe('true');
    });
});

describe('the control under the devices', () => {
    /*
     * It sits with the laptop and the phone rather than in the column of prose
     * beside them, and its label is the instruction — there is no paragraph
     * anywhere telling anyone to arm the panel first.
     */
    it('says what to do first, and cannot be pressed until it has been done', () => {
        initDemo();

        expect($('btn-trigger').disabled).toBe(true);
        expect($('btn-trigger').textContent).toBe('Arm it first');
    });

    it('names the thing it simulates once there is something to simulate', () => {
        initDemo();
        $('p-arm').click();
        vi.advanceTimersByTime(3000);

        expect($('btn-trigger').disabled).toBe(false);
        expect($('btn-trigger').textContent).toBe('Touch the laptop');
    });

    it('goes back to the instruction when the panel is disarmed', () => {
        initDemo();
        $('p-arm').click();
        vi.advanceTimersByTime(3000);
        press($('p-arm'));
        vi.advanceTimersByTime(1500);

        expect($('btn-trigger').textContent).toBe('Arm it first');
    });

    it('lives inside the rig, beside the devices it fires', () => {
        expect(document.querySelector('.rig .demo-controls #btn-trigger')).not.toBeNull();
    });
});
