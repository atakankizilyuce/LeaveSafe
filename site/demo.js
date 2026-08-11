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

/*
 * Which install tab to open on.
 *
 * Three tabs of which two are wrong for whoever is reading is a small puzzle
 * with the answer already in the request. Linux and macOS share a tab because
 * they share the commands. Anything else — a phone, a console, something new —
 * gets the direct download, which works everywhere and needs no package
 * manager to exist.
 *
 * Exported because it is the only part of the panel that is a decision rather
 * than a wire, and the only part worth a test of its own.
 */
export function preferredOs(ua) {
    const s = String(ua || '').toLowerCase();
    if (s.includes('windows') || s.includes('win32') || s.includes('win64')) return 'windows';
    if (s.includes('mac') || s.includes('linux') || s.includes('x11')) return 'mac';
    return 'other';
}

export function initDemo() {
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

    /*
     * The button teaches the order rather than a paragraph beside it doing so.
     * There is nothing to set off until the panel is armed, and while there is
     * not, the button says what to do instead of being a dead grey word.
     */
    const TRIGGER_LOCKED = 'Arm it first';
    const TRIGGER_READY = 'Touch the laptop';
    const alertBox = $('p-alert');
    const alertSensor = $('p-alert-sensor');
    const alertMsg = $('p-alert-msg');
    const nodes = Array.from(document.querySelectorAll('.node'));
    const miniIcons = Array.from(document.querySelectorAll('.core-icons .ico'));
    const phone = document.querySelector('.phone');

    /* The laptop. Half the product, and until now not on the page at all. */
    const lap = $('lap');
    const lapState = $('lap-state');
    const lapSensors = $('lap-sensors');
    const lapLog = $('lap-log');
    const lapSiren = $('lap-siren');

    const mini = (name) => miniIcons.find((i) => i.dataset.sensor === name);

    /*
     * Every `data-` flag on this page is read and written through these two.
     * The DOM offers `hasAttribute`/`setAttribute` for the same job, but a
     * `data-` attribute has `dataset` as its own front door and mixing the two
     * is how one of them ends up spelled `data-tripped` in one place and
     * `tripped` in another.
     */
    const flag = (el, name) => el.dataset[name] !== undefined;
    const setFlag = (el, name, on) => {
        if (on) el.dataset[name] = '';
        else delete el.dataset[name];
    };

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

    const enabled = () => nodes.filter((n) => !flag(n, 'off'));

    /*
     * The shield's row of icons is not decoration — it is the answer to "which
     * sensors are covering me". So it is kept in step with the ring: a station
     * switched off has no icon in there, and the one that fired lights red.
     */
    const syncShield = () => {
        nodes.forEach((node) => {
            const icon = mini(node.dataset.sensor);
            if (!icon) return;
            setFlag(icon, 'off', flag(node, 'off'));
            setFlag(icon, 'tripped', flag(node, 'tripped'));
        });
    };

    // The invitation is spent the moment it is taken.
    const touched = () => setFlag(phone, 'untouched', false);

    /*
     * The laptop's log.
     *
     * Five lines, oldest dropped. A log that grows moves everything under it
     * every time a line lands, and the rig walks about beneath the cursor of
     * the person who is trying to press its buttons.
     */
    const LOG_LINES = 5;

    function log(text, kind) {
        const line = document.createElement('li');
        if (kind) line.dataset.kind = kind;

        const stamp = document.createElement('time');
        stamp.textContent = new Date().toTimeString().slice(0, 8);

        const said = document.createElement('span');
        said.textContent = text;

        line.append(stamp, said);
        lapLog.append(line);
        while (lapLog.children.length > LOG_LINES) lapLog.firstElementChild.remove();
    }

    const laptopSensors = () => {
        lapSensors.textContent = `${enabled().length} ready`;
    };

    // ── the three rooms ───────────────────────────────────────────────────

    function toStandby() {
        clearTimers();
        /* Read before it is cleared: the first call is the page loading, and a
           log line saying the visitor disarmed something they never armed is a
           lie the panel tells itself. */
        const wasRunning = Boolean(root.dataset.state);
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
        triggerBtn.textContent = TRIGGER_LOCKED;
        syncShield();

        lapState.textContent = 'NOT ARMED';
        lapSiren.hidden = true;
        setFlag(lap, 'alarm', false);
        setFlag(root, 'alarm', false);
        laptopSensors();
        log(wasRunning ? 'disarmed from the phone' : 'demo build · not watching a real machine');
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
                lapState.textContent = `ARMING ${left}`;
            });
        }

        log(`arming · ${ARM_SECONDS}s to walk away`);
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
        triggerBtn.textContent = TRIGGER_READY;

        lapState.textContent = 'WATCHING';
        log(`armed · watching ${enabled().length} sensors`, 'armed');
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

    for (const evt of ['pointerup', 'pointerleave', 'pointercancel']) {
        armBtn.addEventListener(evt, () => {
            if (holdTimer) releaseHold();
        });
    }

    // ── the stations ──────────────────────────────────────────────────────

    nodes.forEach((node) => {
        node.addEventListener('click', () => {
            // Armed, the set is fixed: changing what is being watched while it
            // is being watched is exactly the confusion the product avoids.
            if (root.dataset.state === 'armed') return;

            touched();
            setFlag(node, 'off', !flag(node, 'off'));
            setFlag(node, 'tripped', false);
            subEl.textContent = `${enabled().length} sensors ready`;
            syncShield();
            laptopSensors();
        });
    });

    // ── the alarm ─────────────────────────────────────────────────────────

    /*
     * Which sensor fires goes round the ring in order rather than being picked
     * at random. Two reasons, and the second is the one that decided it:
     *
     * A visitor who presses Trigger three times should see three different
     * sensors, not the same one twice — the point of the button is that any of
     * them can wake you, and a repeat argues the opposite.
     *
     * And there is no reason for a security tool's own page to be calling a
     * random number generator at all. `Math.random()` is not fit for anything
     * that has to be unpredictable, so every scanner that reads it has to stop
     * and ask whether this is one of those places. Removing the call answers
     * the question better than a comment claiming it is harmless would.
     */
    let nextToFire = 0;

    triggerBtn.addEventListener('click', () => {
        const candidates = enabled();
        if (!candidates.length) return;

        const node = candidates[nextToFire % candidates.length];
        nextToFire++;
        const name = node.dataset.sensor;

        for (const n of nodes) setFlag(n, 'tripped', false);
        setFlag(node, 'tripped', true);
        syncShield();

        alertSensor.textContent = name.toUpperCase();
        alertMsg.textContent = MESSAGES[name];
        alertBox.hidden = false;

        /* Both ends. The phone is the convenience; the laptop is the alarm, and
           it goes off whether or not anyone is looking at a phone. */
        lapState.textContent = 'ALARM';
        lapSiren.hidden = false;
        /* Red is the alarm and nothing else on this page, so it is switched on
           here and switched off in the two places the alarm ends. */
        setFlag(lap, 'alarm', true);
        /* The alarm is not only in the panel that raised it: every colour on
           the page hangs off the root, and so does the wiring behind it. */
        setFlag(root, 'alarm', true);
        log(`${name.toUpperCase()} · ${MESSAGES[name]}`, 'trip');
        log('sounding here, and on the paired phone', 'trip');
    });

    $('p-dismiss').addEventListener('click', () => {
        alertBox.hidden = true;
        lapSiren.hidden = true;
        setFlag(lap, 'alarm', false);
        setFlag(root, 'alarm', false);
        lapState.textContent = 'WATCHING';
        log('alarm silenced · still watching', 'armed');
    });

    // ── install tabs ──────────────────────────────────────────────────────

    const tabs = Array.from(document.querySelectorAll('.tab'));

    const selectTab = (tab) => {
        tabs.forEach((t) => {
            const on = t === tab;
            t.classList.toggle('is-on', on);
            t.setAttribute('aria-selected', String(on));
            document.getElementById(t.getAttribute('aria-controls')).hidden = !on;
        });
    };

    tabs.forEach((tab) => {
        tab.addEventListener('click', () => selectTab(tab));
    });

    /* Open on the tab for the machine that asked for the page. Falls through to
       whatever the markup already had selected if the platform is not one of
       the three, so a visitor is never left with no tab open. */
    const mine = tabs.find((t) => t.dataset.os === preferredOs(navigator.userAgent));
    if (mine) selectTab(mine);

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
}
