/*
 * The demonstration in the hero.
 *
 * It is a demonstration and it says so on the page. Nothing here is connected
 * to anything: the countdown is three timeouts, the sensors are six buttons,
 * and the alarm is a div. A visitor deciding whether to download cannot tell
 * the difference between this and the product, and the product is one click
 * away for anyone who wants to.
 *
 * The one thing copied exactly is the state machine's shape — standby, arming
 * with a count, armed — written to `documentElement.dataset` the way the panel
 * itself does it, because every colour on this page hangs off that attribute.
 */

(() => {
    'use strict';

    const root = document.documentElement;
    const $ = (id) => document.getElementById(id);

    const stateEl = $('p-state');
    const subEl = $('p-sub');
    const hintEl = $('p-hint');
    const coreWord = $('core-word');
    const coreNote = $('core-note');
    // The eye is the one thing in the middle that has to agree with the word
    // under it. A shut eye over "WATCHING" reads as a bug, because it is one.
    const coreEye = document.querySelector('.core-eye use');
    const armBtn = $('p-arm');
    const armLabel = $('p-arm-label');
    const armFill = $('p-arm-fill');
    const armIcon = armBtn.querySelector('use');
    const triggerBtn = $('btn-trigger');
    const alertBox = $('p-alert');
    const alertSensor = $('p-alert-sensor');
    const alertMsg = $('p-alert-msg');
    const nodes = Array.from(document.querySelectorAll('.node'));
    const miniIcons = Array.from(document.querySelectorAll('.core-icons .ico'));
    const phone = document.querySelector('.phone');

    const mini = (name) => miniIcons.find((i) => i.dataset.sensor === name);

    const MESSAGES = {
        power: 'Charger unplugged',
        lid: 'Lid closed',
        usb: 'A USB device was connected',
        screen: 'The display turned off',
        network: 'The network interface changed',
        input: 'Sustained mouse or keyboard activity detected',
    };

    const ARM_SECONDS = 3;
    const DISARM_MS = 1500;

    let timers = [];

    const clearTimers = () => {
        timers.forEach(clearTimeout);
        timers = [];
    };

    const after = (ms, fn) => timers.push(setTimeout(fn, ms));

    const enabled = () => nodes.filter((n) => !n.hasAttribute('data-off'));

    /*
     * The shield's row of icons is not decoration — it is the answer to "which
     * sensors are covering me". So it is kept in step with the ring: a station
     * switched off has no icon in there, and the one that fired lights red.
     */
    const syncShield = () => {
        nodes.forEach((node) => {
            const icon = mini(node.dataset.sensor);
            if (!icon) return;
            icon.toggleAttribute('data-off', node.hasAttribute('data-off'));
            icon.toggleAttribute('data-tripped', node.hasAttribute('data-tripped'));
        });
    };

    // The invitation is spent the moment it is taken.
    const touched = () => phone.removeAttribute('data-untouched');

    // ── the three rooms ───────────────────────────────────────────────────

    function toStandby() {
        clearTimers();
        delete root.dataset.state;
        delete root.dataset.count;

        stateEl.textContent = 'STANDBY';
        subEl.textContent = `${enabled().length} sensors ready`;
        hintEl.textContent = 'TAP TO TURN ON OR OFF';
        coreWord.textContent = 'NOT WATCHING';
        coreNote.textContent = 'TAP TO ARM';
        coreEye.setAttribute('href', '#i-eye-off');
        armLabel.textContent = 'Arm';
        armIcon.setAttribute('href', '#i-lock');
        armFill.style.transition = '';
        armFill.style.width = '0';
        triggerBtn.disabled = true;
        syncShield();
    }

    function toArming() {
        clearTimers();
        root.dataset.state = 'arming';

        hintEl.textContent = 'ARMING';
        coreWord.textContent = 'ARMING';
        coreNote.textContent = 'TAP TO CANCEL';
        armLabel.textContent = 'Cancel';

        // A third of the way to armed per second, so the colour is the
        // countdown rather than a decoration beside one.
        for (let left = ARM_SECONDS; left > 0; left--) {
            const elapsed = (ARM_SECONDS - left) * 1000;
            after(elapsed, () => {
                root.dataset.count = String(left);
                stateEl.textContent = `ARMING ${left}`;
                subEl.textContent = `${enabled().length} sensors · ${left}s`;
            });
        }

        after(ARM_SECONDS * 1000, toArmed);
    }

    function toArmed() {
        clearTimers();
        root.dataset.state = 'armed';
        delete root.dataset.count;

        stateEl.textContent = 'ARMED';
        subEl.textContent = `Watching ${enabled().length} sensors · just now`;
        hintEl.textContent = 'DISARM TO CHANGE';
        coreWord.textContent = 'WATCHING';
        coreEye.setAttribute('href', '#i-eye');
        armLabel.textContent = 'Hold to disarm';
        armIcon.setAttribute('href', '#i-unlock');
        triggerBtn.disabled = false;
    }

    // ── the arm control ───────────────────────────────────────────────────

    armBtn.addEventListener('click', () => {
        if (swallowNextClick) {
            swallowNextClick = false;
            return;
        }

        touched();
        const state = root.dataset.state;
        if (!state) toArming();
        // A countdown can be taken back. That is the whole reason it exists.
        else if (state === 'arming') toStandby();
    });

    /*
     * Disarming asks for a second and a half of deliberate intent, and shows it
     * filling. A press let go early is not a disarm — it leaves the page armed
     * and the fill returns to nothing.
     *
     * The bar is filled by a CSS transition and the disarm itself by a timer,
     * rather than both by a rAF loop stepping the width frame by frame. The
     * loop was the first version and it had a real fault: when frames are
     * throttled — a background tab, a busy compositor — the callback stops
     * arriving, so the width freezes partway and the disarm never lands however
     * long the button is held. A timer fires either way, and the browser
     * animates the bar without needing to be asked once per frame.
     */
    let holdTimer = 0;

    /*
     * A completed hold is followed by the pointerup's `click`, which lands on a
     * button that is now in standby — where a click means arm. Left alone, the
     * panel disarms and then immediately re-arms itself as the finger comes up,
     * which looks like the demonstration ignoring you.
     *
     * `preventDefault` on pointerdown does not stop that click, so the click is
     * swallowed once instead. Only a *completed* hold sets this: a hold let go
     * early leaves the page armed, and a click while armed already does
     * nothing. And the flag is always consumed by the click that follows the
     * release, so it can never sit true and eat someone's next keypress.
     */
    let swallowNextClick = false;

    function releaseHold() {
        clearTimeout(holdTimer);
        holdTimer = 0;
        armFill.style.transition = 'width 120ms cubic-bezier(0.32, 0.72, 0, 1)';
        armFill.style.width = '0';
    }

    armBtn.addEventListener('pointerdown', (e) => {
        if (root.dataset.state !== 'armed' || holdTimer) return;
        e.preventDefault();

        armFill.style.transition = `width ${DISARM_MS}ms linear`;
        armFill.style.width = '100%';

        holdTimer = setTimeout(() => {
            holdTimer = 0;
            swallowNextClick = true;
            releaseHold();
            toStandby();
        }, DISARM_MS);
    });

    ['pointerup', 'pointerleave', 'pointercancel'].forEach((evt) =>
        armBtn.addEventListener(evt, () => {
            if (holdTimer) releaseHold();
        }),
    );

    // ── the stations ──────────────────────────────────────────────────────

    nodes.forEach((node) => {
        node.addEventListener('click', () => {
            // Armed, the set is fixed: changing what is being watched while it
            // is being watched is exactly the confusion the product avoids.
            if (root.dataset.state === 'armed') return;

            touched();
            node.toggleAttribute('data-off');
            node.removeAttribute('data-tripped');
            subEl.textContent = `${enabled().length} sensors ready`;
            syncShield();
        });
    });

    // ── the alarm ─────────────────────────────────────────────────────────

    triggerBtn.addEventListener('click', () => {
        const candidates = enabled();
        if (!candidates.length) return;

        const node = candidates[Math.floor(Math.random() * candidates.length)];
        const name = node.dataset.sensor;

        nodes.forEach((n) => n.removeAttribute('data-tripped'));
        node.setAttribute('data-tripped', '');
        syncShield();

        alertSensor.textContent = name.toUpperCase();
        alertMsg.textContent = MESSAGES[name];
        alertBox.hidden = false;
    });

    $('p-dismiss').addEventListener('click', () => {
        alertBox.hidden = true;
    });

    // ── install tabs ──────────────────────────────────────────────────────

    const tabs = Array.from(document.querySelectorAll('.tab'));

    tabs.forEach((tab) => {
        tab.addEventListener('click', () => {
            tabs.forEach((t) => {
                const on = t === tab;
                t.classList.toggle('is-on', on);
                t.setAttribute('aria-selected', String(on));
                document.getElementById(t.getAttribute('aria-controls')).hidden = !on;
            });
        });
    });

    // ── copy to clipboard ─────────────────────────────────────────────────

    document.querySelectorAll('.copy').forEach((btn) => {
        btn.addEventListener('click', async () => {
            const text = btn.parentElement.querySelector('code').textContent;
            try {
                await navigator.clipboard.writeText(text);
            } catch {
                // Denied clipboard permission, or an insecure origin. Saying
                // nothing is better than an error the visitor cannot act on.
                return;
            }
            btn.classList.add('is-done');
            btn.querySelector('use').setAttribute('href', '#i-check');
            setTimeout(() => {
                btn.classList.remove('is-done');
                btn.querySelector('use').setAttribute('href', '#i-copy');
            }, 1400);
        });
    });

    toStandby();
})();
