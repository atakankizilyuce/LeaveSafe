// LeaveSafe Web Client
(function() {
    'use strict';

    var ws = null;
    var token = null;
    var armed = false;
    var connectedAt = null;
    var uptimeInterval = null;
    var reconnectTimeout = null;
    var alarmOscillator = null;
    var alarmCtx = null;
    var sensors = {};
    var lastPongTime = null;
    var disconnectShown = false;
    var serverVersion = null;
    var alarmTriggerSensor = null;
    var bleDevice = null;
    var bleRxChar = null;
    var bleTxChar = null;

    var authScreen = document.getElementById('auth-screen');
    var dashScreen = document.getElementById('dashboard-screen');
    var keyInput = document.getElementById('pairing-key');
    var authError = document.getElementById('auth-error');
    var connectBtn = document.getElementById('connect-btn');
    var statusBadge = document.getElementById('status-badge');
    var connStatus = document.getElementById('conn-status');
    var connDot = document.getElementById('conn-dot');
    var connUptime = document.getElementById('conn-uptime');
    var connLastCheck = document.getElementById('conn-last-check');
    var sensorList = document.getElementById('sensor-list');
    var alertFeed = document.getElementById('alert-feed');
    var armBtn = document.getElementById('arm-btn');
    var alertOverlay = document.getElementById('alert-overlay');
    var alertOverlayText = document.getElementById('alert-overlay-text');
    var disconnectOverlay = document.getElementById('disconnect-overlay');

    keyInput.addEventListener('input', function(e) {
        var v = e.target.value.replace(/[^0-9]/g, '');
        if (v.length > 16) v = v.substring(0, 16);
        var formatted = '';
        for (var i = 0; i < v.length; i++) {
            if (i > 0 && i % 4 === 0) formatted += '-';
            formatted += v[i];
        }
        e.target.value = formatted;
    });

    keyInput.addEventListener('keydown', function(e) {
        if (e.key === 'Enter') authenticate();
    });

    var params = new URLSearchParams(window.location.search);
    var urlKey = params.get('key');
    if (urlKey) {
        keyInput.value = formatKey(urlKey);
        setTimeout(authenticate, 500);
    }

    function authenticate() {
        var key = keyInput.value.replace(/-/g, '');
        if (key.length !== 16) {
            showAuthError('Please enter a 16-digit key');
            return;
        }

        connectBtn.disabled = true;
        connectBtn.textContent = 'Connecting...';
        connectBtn.classList.add('connecting');
        authError.classList.add('hidden');

        var checkUrl = location.protocol + '//' + location.host + '/';
        var controller = typeof AbortController !== 'undefined' ? new AbortController() : null;
        var fetchOpts = controller ? { signal: controller.signal } : {};
        var fetchTimeout = controller ? setTimeout(function() { controller.abort(); }, 5000) : null;

        fetch(checkUrl, fetchOpts).then(function() {
            if (fetchTimeout) clearTimeout(fetchTimeout);
            connectWebSocket(key);
        }).catch(function() {
            if (fetchTimeout) clearTimeout(fetchTimeout);
            connectWebSocket(key);
        });
    }

    function connectWebSocket(key) {
        var proto = location.protocol === 'https:' ? 'wss:' : 'ws:';
        var wsUrl = proto + '//' + location.host + '/ws';

        try {
            ws = new WebSocket(wsUrl);
        } catch(e) {
            showAuthError('Connection failed. Check that your device is on the same network.');
            resetConnectBtn();
            return;
        }

        var connTimeout = setTimeout(function() {
            if (ws && ws.readyState !== WebSocket.OPEN) {
                ws.close();
                showAuthError('Connection timed out. Make sure you are on the same Wi-Fi network.');
                resetConnectBtn();
            }
        }, 8000);

        ws.onopen = function() {
            clearTimeout(connTimeout);
            ws.send(JSON.stringify({ type: 'auth', key: key }));
        };

        ws.onmessage = function(e) {
            var msg;
            try { msg = JSON.parse(e.data); } catch(err) { return; }
            handleMessage(msg);
        };

        ws.onclose = function() {
            clearTimeout(connTimeout);
            if (token) {
                setConnectionState('disconnected');
                showDisconnectWarning();
                scheduleReconnect(key);
            } else {
                resetConnectBtn();
            }
        };

        ws.onerror = function() {
            clearTimeout(connTimeout);
            if (!token) {
                showAuthError('Connection error. Make sure you are on the same Wi-Fi network.');
                resetConnectBtn();
            }
        };
    }

    function resetConnectBtn() {
        connectBtn.disabled = false;
        connectBtn.textContent = 'Connect';
        connectBtn.classList.remove('connecting');
    }

    function scheduleReconnect(key) {
        if (reconnectTimeout) clearTimeout(reconnectTimeout);
        reconnectTimeout = setTimeout(function() {
            keyInput.value = formatKey(key);
            authenticate();
        }, 3000);
    }

    function handleMessage(msg) {
        switch (msg.type) {
            case 'auth_ok':
                token = msg.token;
                serverVersion = msg.version || null;
                sensors = {};
                if (msg.sensors) {
                    msg.sensors.forEach(function(s) {
                        sensors[s.name] = s;
                    });
                }
                resetConnectBtn();
                showDashboard();
                break;

            case 'auth_fail':
                resetConnectBtn();
                showAuthError(msg.reason + (msg.remaining_attempts ? ' (' + msg.remaining_attempts + ' left)' : ''));
                break;

            case 'status':
                if (msg.armed !== undefined && msg.armed !== null) {
                    armed = msg.armed;
                    updateArmState();
                }
                if (msg.sensor_states) {
                    for (var name in msg.sensor_states) {
                        if (sensors[name]) {
                            sensors[name].enabled = msg.sensor_states[name].enabled;
                            sensors[name].status = msg.sensor_states[name].status;
                        }
                    }
                    renderSensors();
                }
                setConnectionState('connected');
                break;

            case 'alert':
                if (msg.alert) {
                    alarmTriggerSensor = msg.alert.sensor || null;
                    addAlert(msg.alert, msg.ts);
                    triggerAlarm(msg.alert.message);
                }
                break;

            case 'alarm_active':
                if (msg.alert && msg.alert.sensor) alarmTriggerSensor = msg.alert.sensor;
                triggerAlarm(msg.alert ? msg.alert.message : 'Security alarm triggered!');
                break;

            case 'config_data':
                if (msg.config) populateSettings(msg.config);
                break;

            case 'pin_required':
                showPinDialog();
                break;

            case 'pong':
                lastPongTime = Date.now();
                setConnectionState('connected');
                updateLastCheck();
                break;
        }
    }

    function setConnectionState(state) {
        if (state === 'connected') {
            connStatus.textContent = 'Connected';
            connStatus.className = 'status-ok';
            connDot.className = 'conn-dot';
            if (disconnectShown) dismissDisconnect();
        } else if (state === 'disconnected') {
            connStatus.textContent = 'Disconnected';
            connStatus.className = 'status-alert';
            connDot.className = 'conn-dot disconnected';
        } else if (state === 'checking') {
            connStatus.textContent = 'Checking...';
            connStatus.className = 'status-checking';
            connDot.className = 'conn-dot checking';
        }
    }

    function showDisconnectWarning() {
        disconnectShown = true;
        disconnectOverlay.classList.remove('hidden');
        if (navigator.vibrate) {
            navigator.vibrate([300, 100, 300, 100, 300]);
        }
        if ('Notification' in window && Notification.permission === 'granted') {
            new Notification('LeaveSafe', { body: 'Connection lost!', tag: 'leavesafe-disconnect' });
        }
    }

    function dismissDisconnect() {
        disconnectShown = false;
        disconnectOverlay.classList.add('hidden');
    }

    function refreshConnection() {
        setConnectionState('checking');
        if (ws && ws.readyState === WebSocket.OPEN) {
            sendMsg({ type: 'ping' });
            var checkBefore = lastPongTime;
            setTimeout(function() {
                if (lastPongTime === checkBefore) {
                    setConnectionState('disconnected');
                    showDisconnectWarning();
                } else {
                    updateLastCheck();
                }
            }, 3000);
        } else {
            setTimeout(function() {
                setConnectionState('disconnected');
                showDisconnectWarning();
            }, 500);
        }
    }

    function sendTestAlert() {
        sendMsg({ type: 'test_alert' });
    }

    function updateLastCheck() {
        if (connLastCheck) connLastCheck.textContent = 'just now';
    }

    function showDashboard() {
        authScreen.classList.add('hidden');
        dashScreen.classList.remove('hidden');
        if (serverVersion) {
            var footer = document.querySelector('.app-footer span');
            if (footer) footer.textContent = 'LeaveSafe ' + serverVersion;
        }
        connectedAt = Date.now();
        lastPongTime = Date.now();
        setConnectionState('connected');
        renderSensors();
        loadAlertHistory();
        startUptime();
        updateLastCheck();
        if (window.history.replaceState) {
            window.history.replaceState({}, document.title, '/');
        }
        if ('Notification' in window && Notification.permission === 'default') {
            Notification.requestPermission();
        }
        if ('serviceWorker' in navigator) {
            navigator.serviceWorker.register('/sw.js').catch(function() {});
        }
    }

    function showAuthError(msg) {
        authError.textContent = msg;
        authError.classList.remove('hidden');
    }

    function renderSensors() {
        sensorList.innerHTML = '';
        for (var name in sensors) {
            var s = sensors[name];
            var div = document.createElement('div');
            div.className = 'sensor-item' + (s.available === false ? ' sensor-unavailable' : '');

            var dotClass = 'sensor-dot ';
            if (s.available === false) dotClass += 'unavailable';
            else if (s.status === 'alert') dotClass += 'alert';
            else dotClass += 'ok';

            var desc = sensorDescriptions[name] || '';
            div.innerHTML =
                '<div class="sensor-header" data-sensor="' + escapeHtml(name) + '">' +
                    '<div class="sensor-info">' +
                        '<span class="' + dotClass + '"></span>' +
                        '<div>' +
                            '<div class="sensor-name">' + escapeHtml(s.display_name || s.name) + '</div>' +
                            '<div class="sensor-status">' + (s.status || (s.available === false ? 'Unavailable' : 'OK')) + '</div>' +
                        '</div>' +
                    '</div>' +
                    '<div class="sensor-actions">' +
                        '<button class="btn-trigger-sensor" data-sensor="' + escapeHtml(name) + '" ' +
                            (s.available === false ? 'disabled' : '') + '>Test</button>' +
                        '<label class="toggle">' +
                            '<input type="checkbox" ' + (s.enabled ? 'checked' : '') + ' ' + (s.available === false ? 'disabled' : '') + ' data-sensor="' + escapeHtml(name) + '">' +
                            '<span class="slider"></span>' +
                        '</label>' +
                    '</div>' +
                '</div>' +
                (desc ? '<div class="sensor-detail hidden" id="detail-' + escapeHtml(name) + '">' +
                    '<p class="sensor-desc">' + escapeHtml(desc) + '</p>' +
                '</div>' : '');
            sensorList.appendChild(div);
        }
        sensorList.querySelectorAll('.sensor-header').forEach(function(header) {
            header.addEventListener('click', function(e) {
                if (e.target.closest('.sensor-actions')) return;
                var detail = document.getElementById('detail-' + this.dataset.sensor);
                if (detail) detail.classList.toggle('hidden');
            });
        });
        sensorList.querySelectorAll('input[type=checkbox]').forEach(function(cb) {
            cb.addEventListener('change', function() {
                var cfg = {};
                cfg[this.dataset.sensor] = this.checked;
                sendMsg({ type: 'configure', sensors: cfg });
            });
        });
        sensorList.querySelectorAll('.btn-trigger-sensor').forEach(function(btn) {
            btn.addEventListener('click', function() {
                sendMsg({ type: 'trigger_sensor', sensor: this.dataset.sensor });
            });
        });
    }

    var sensorDescriptions = {
        'power': 'Monitors AC power/charger connection. Alerts when charger is disconnected or reconnected.',
        'lid': 'Monitors laptop lid state. Alerts when the lid is opened or closed.',
        'usb': 'Monitors USB port activity. Alerts when a USB device is plugged in or removed.',
        'screen': 'Monitors screen/display state. Alerts when screen is turned on or off.',
        'network': 'Monitors network interfaces. Alerts when IP address changes (opt-in).',
        'input': 'Monitors mouse and keyboard activity. Alerts when input is detected while armed (opt-in).'
    };

    var ALERT_STORAGE_KEY = 'leavesafe_alerts';
    var MAX_STORED_ALERTS = 200;

    function loadAlertHistory() {
        try {
            var data = localStorage.getItem(ALERT_STORAGE_KEY);
            if (!data) return;
            var alerts = JSON.parse(data);
            if (!Array.isArray(alerts)) return;
            var placeholder = alertFeed.querySelector('.muted');
            if (placeholder && alerts.length > 0) placeholder.remove();
            for (var i = alerts.length - 1; i >= 0; i--) {
                var a = alerts[i];
                renderAlertItem(a.message, a.level, a.time);
            }
        } catch(e) {}
    }

    function saveAlertToHistory(alert, timeStr) {
        try {
            var data = localStorage.getItem(ALERT_STORAGE_KEY);
            var alerts = data ? JSON.parse(data) : [];
            if (!Array.isArray(alerts)) alerts = [];
            alerts.unshift({ message: alert.message, level: alert.level, sensor: alert.sensor, time: timeStr });
            if (alerts.length > MAX_STORED_ALERTS) alerts = alerts.slice(0, MAX_STORED_ALERTS);
            localStorage.setItem(ALERT_STORAGE_KEY, JSON.stringify(alerts));
        } catch(e) {}
    }

    function clearAlertHistory() {
        try { localStorage.removeItem(ALERT_STORAGE_KEY); } catch(e) {}
        alertFeed.innerHTML = '<p class="muted">No alerts yet</p>';
    }

    function renderAlertItem(message, level, timeStr) {
        var div = document.createElement('div');
        div.className = 'alert-item';
        div.innerHTML =
            '<div class="alert-msg">' +
                '<span class="alert-dot ' + escapeHtml(level) + '"></span>' +
                '<span class="alert-' + escapeHtml(level) + '">' + escapeHtml(message) + '</span>' +
            '</div>' +
            '<span class="alert-time">' + escapeHtml(timeStr) + '</span>';
        alertFeed.appendChild(div);
    }

    function addAlert(alert, ts) {
        var placeholder = alertFeed.querySelector('.muted');
        if (placeholder) placeholder.remove();

        var timeStr = ts ? new Date(ts * 1000).toLocaleTimeString() : new Date().toLocaleTimeString();
        var div = document.createElement('div');
        div.className = 'alert-item';
        div.innerHTML =
            '<div class="alert-msg">' +
                '<span class="alert-dot ' + escapeHtml(alert.level) + '"></span>' +
                '<span class="alert-' + escapeHtml(alert.level) + '">' + escapeHtml(alert.message) + '</span>' +
            '</div>' +
            '<span class="alert-time">' + escapeHtml(timeStr) + '</span>';
        alertFeed.insertBefore(div, alertFeed.firstChild);

        while (alertFeed.children.length > 50) {
            alertFeed.removeChild(alertFeed.lastChild);
        }

        saveAlertToHistory(alert, timeStr);
    }

    var armCountdownInterval = null;
    var disarmPressTimer = null;
    var DISARM_HOLD_MS = 1000;
    var ARM_COUNTDOWN_SECS = 3;

    function toggleArm() {
        if (!armed) {
            startArmCountdown();
        }
    }

    function startArmCountdown() {
        if (armCountdownInterval) return;
        var remaining = ARM_COUNTDOWN_SECS;
        armBtn.textContent = 'ARMING... ' + remaining;
        armBtn.disabled = true;
        armCountdownInterval = setInterval(function() {
            remaining--;
            if (remaining <= 0) {
                clearInterval(armCountdownInterval);
                armCountdownInterval = null;
                armBtn.disabled = false;
                sendMsg({ type: 'arm' });
            } else {
                armBtn.textContent = 'ARMING... ' + remaining;
            }
        }, 1000);
    }

    function cancelArmCountdown() {
        if (armCountdownInterval) {
            clearInterval(armCountdownInterval);
            armCountdownInterval = null;
            armBtn.disabled = false;
            updateArmState();
        }
    }

    function startDisarmHold() {
        if (!armed) return;
        disarmPressTimer = setTimeout(function() {
            disarmPressTimer = null;
            sendMsg({ type: 'disarm' });
        }, DISARM_HOLD_MS);
        armBtn.textContent = 'HOLD...';
    }

    function showPinDialog() {
        var overlay = document.getElementById('pin-overlay');
        var input = document.getElementById('pin-input');
        if (overlay && input) {
            input.value = '';
            overlay.classList.remove('hidden');
            input.focus();
        }
    }

    function submitPin() {
        var input = document.getElementById('pin-input');
        var pinError = document.getElementById('pin-error');
        if (!input) return;
        var pin = input.value.trim();
        if (!pin) {
            if (pinError) { pinError.textContent = 'Please enter PIN'; pinError.classList.remove('hidden'); }
            return;
        }
        sendMsg({ type: 'disarm_with_pin', pin: pin });
        document.getElementById('pin-overlay').classList.add('hidden');
        if (pinError) pinError.classList.add('hidden');
    }

    function cancelPin() {
        var overlay = document.getElementById('pin-overlay');
        if (overlay) overlay.classList.add('hidden');
    }

    function cancelDisarmHold() {
        if (disarmPressTimer) {
            clearTimeout(disarmPressTimer);
            disarmPressTimer = null;
            updateArmState();
        }
    }

    function updateArmState() {
        if (armed) {
            statusBadge.textContent = 'ARMED';
            statusBadge.className = 'badge armed';
            armBtn.textContent = 'DISARM';
            armBtn.classList.add('armed');
        } else {
            statusBadge.textContent = 'DISARMED';
            statusBadge.className = 'badge disarmed';
            armBtn.textContent = 'ARM';
            armBtn.classList.remove('armed');
        }
    }

    var vibrateInterval = null;
    var titleFlashInterval = null;
    var titleFlashTimeout = null;

    function triggerAlarm(message) {
        dismissAlarmIntervals();

        alertOverlayText.textContent = message;
        alertOverlay.classList.remove('hidden');

        var flash = true;
        var originalTitle = document.title;
        titleFlashInterval = setInterval(function() {
            document.title = flash ? 'ALERT! ' + message : originalTitle;
            flash = !flash;
        }, 500);
        titleFlashTimeout = setTimeout(function() {
            clearInterval(titleFlashInterval);
            titleFlashInterval = null;
            titleFlashTimeout = null;
            document.title = originalTitle;
        }, 30000);

        startAlarmSound();

        if (navigator.vibrate) {
            navigator.vibrate([500, 200, 500, 200, 500]);
            vibrateInterval = setInterval(function() {
                if (navigator.vibrate) navigator.vibrate([500, 200, 500, 200, 500]);
            }, 2000);
        }

        if (navigator.serviceWorker && navigator.serviceWorker.controller) {
            navigator.serviceWorker.controller.postMessage({
                type: 'alarm',
                message: message
            });
        } else if ('Notification' in window && Notification.permission === 'granted') {
            new Notification('LeaveSafe ALERT', {
                body: message,
                tag: 'leavesafe-alert',
                requireInteraction: true,
                renotify: true
            });
        } else if ('Notification' in window && Notification.permission !== 'denied') {
            Notification.requestPermission();
        }
    }

    function dismissAlarmIntervals() {
        if (vibrateInterval) {
            clearInterval(vibrateInterval);
            vibrateInterval = null;
        }
        if (titleFlashInterval) {
            clearInterval(titleFlashInterval);
            titleFlashInterval = null;
        }
        if (titleFlashTimeout) {
            clearTimeout(titleFlashTimeout);
            titleFlashTimeout = null;
        }
        document.title = 'LeaveSafe';
        if (navigator.vibrate) navigator.vibrate(0);
    }

    function dismissAlert() {
        alertOverlay.classList.add('hidden');
        stopAlarmSound();
        dismissAlarmIntervals();
        sendMsg({ type: 'dismiss_alarm' });
        alarmTriggerSensor = null;
    }

    function dismissAlertPause() {
        alertOverlay.classList.add('hidden');
        stopAlarmSound();
        dismissAlarmIntervals();
        sendMsg({ type: 'dismiss_alarm_pause', sensor: alarmTriggerSensor, duration: 5 });
        alarmTriggerSensor = null;
    }

    function dismissAlertDisable() {
        alertOverlay.classList.add('hidden');
        stopAlarmSound();
        dismissAlarmIntervals();
        sendMsg({ type: 'dismiss_alarm_disable', sensor: alarmTriggerSensor });
        alarmTriggerSensor = null;
    }

    var alarmHarmonicOsc = null;
    var modulateInterval = null;

    function startAlarmSound() {
        try {
            if (alarmCtx) stopAlarmSound();
            alarmCtx = new (window.AudioContext || window.webkitAudioContext)();

            alarmOscillator = alarmCtx.createOscillator();
            var mainGain = alarmCtx.createGain();
            alarmOscillator.type = 'square';
            alarmOscillator.frequency.value = 880;
            mainGain.gain.value = 1.0;
            alarmOscillator.connect(mainGain);
            mainGain.connect(alarmCtx.destination);

            alarmHarmonicOsc = alarmCtx.createOscillator();
            var harmGain = alarmCtx.createGain();
            alarmHarmonicOsc.type = 'square';
            alarmHarmonicOsc.frequency.value = 1760;
            harmGain.gain.value = 0.5;
            alarmHarmonicOsc.connect(harmGain);
            harmGain.connect(alarmCtx.destination);

            alarmOscillator.start();
            alarmHarmonicOsc.start();

            var high = true;
            modulateInterval = setInterval(function() {
                if (!alarmOscillator) { clearInterval(modulateInterval); modulateInterval = null; return; }
                alarmOscillator.frequency.value = high ? 880 : 660;
                alarmHarmonicOsc.frequency.value = high ? 1760 : 1320;
                high = !high;
            }, 400);
        } catch(e) {}
    }

    function stopAlarmSound() {
        if (modulateInterval) {
            clearInterval(modulateInterval);
            modulateInterval = null;
        }
        if (alarmOscillator) {
            try { alarmOscillator.stop(); } catch(e) {}
            alarmOscillator = null;
        }
        if (alarmHarmonicOsc) {
            try { alarmHarmonicOsc.stop(); } catch(e) {}
            alarmHarmonicOsc = null;
        }
        if (alarmCtx) {
            try { alarmCtx.close(); } catch(e) {}
            alarmCtx = null;
        }
    }

    function startUptime() {
        if (uptimeInterval) clearInterval(uptimeInterval);
        uptimeInterval = setInterval(function() {
            if (!connectedAt) return;
            var secs = Math.floor((Date.now() - connectedAt) / 1000);
            var mins = Math.floor(secs / 60);
            var hrs = Math.floor(mins / 60);
            if (hrs > 0) {
                connUptime.textContent = hrs + 'h ' + (mins % 60) + 'm';
            } else if (mins > 0) {
                connUptime.textContent = mins + 'm ' + (secs % 60) + 's';
            } else {
                connUptime.textContent = secs + 's';
            }
            if (lastPongTime && connLastCheck) {
                var ago = Math.floor((Date.now() - lastPongTime) / 1000);
                if (ago < 5) {
                    connLastCheck.textContent = 'just now';
                } else if (ago < 60) {
                    connLastCheck.textContent = ago + 's ago';
                } else {
                    connLastCheck.textContent = Math.floor(ago / 60) + 'm ago';
                }
            }
        }, 1000);
    }

    function requestConfig() {
        sendMsg({ type: 'get_config' });
    }

    function populateSettings(cfg) {
        document.getElementById('cfg-port').value = cfg.port || 0;
        document.getElementById('cfg-max-sessions').value = cfg.max_sessions || 3;
        document.getElementById('cfg-max-auth-attempts').value = cfg.max_auth_attempts || 5;
        document.getElementById('cfg-lockout-seconds').value = cfg.lockout_seconds || 60;
        document.getElementById('cfg-heartbeat-seconds').value = cfg.heartbeat_seconds || 15;
        document.getElementById('cfg-disconnect-grace').value = cfg.disconnect_grace_seconds || 30;
        document.getElementById('cfg-input-threshold').value = cfg.input_threshold || 3;
        document.getElementById('cfg-auto-arm').checked = !!cfg.auto_arm_on_lock;
        document.getElementById('cfg-pin-enabled').checked = cfg.pin_protection && cfg.pin_protection.enabled;
        document.getElementById('cfg-escalation').checked = cfg.alarm && cfg.alarm.escalation_enabled;
        document.getElementById('cfg-connection-mode').value = cfg.connection_mode || 'wifi';
        var pinGroup = document.getElementById('pin-config-group');
        if (cfg.pin_protection && cfg.pin_protection.enabled) {
            pinGroup.classList.remove('hidden');
        } else {
            pinGroup.classList.add('hidden');
        }
    }

    function saveSettings() {
        var cfg = {
            port: parseInt(document.getElementById('cfg-port').value) || 0,
            max_sessions: parseInt(document.getElementById('cfg-max-sessions').value) || 3,
            max_auth_attempts: parseInt(document.getElementById('cfg-max-auth-attempts').value) || 5,
            lockout_seconds: parseInt(document.getElementById('cfg-lockout-seconds').value) || 60,
            heartbeat_seconds: parseInt(document.getElementById('cfg-heartbeat-seconds').value) || 15,
            disconnect_grace_seconds: parseInt(document.getElementById('cfg-disconnect-grace').value) || 30,
            input_threshold: parseInt(document.getElementById('cfg-input-threshold').value) || 3,
            auto_arm_on_lock: document.getElementById('cfg-auto-arm').checked,
            connection_mode: document.getElementById('cfg-connection-mode').value,
            alarm: {
                escalation_enabled: document.getElementById('cfg-escalation').checked
            },
            pin_protection: {
                enabled: document.getElementById('cfg-pin-enabled').checked,
                pin: document.getElementById('cfg-pin').value || ''
            }
        };
        sendMsg({ type: 'update_config', config: cfg });
    }

    var BLE_SERVICE_UUID = '4c454156-4553-4146-452d-424c45000001';
    var BLE_TX_UUID = '4c454156-4553-4146-452d-424c45000002';
    var BLE_RX_UUID = '4c454156-4553-4146-452d-424c45000003';

    function connectBluetooth() {
        if (!navigator.bluetooth) {
            showAuthError('Bluetooth not supported in this browser. Use Chrome or Edge.');
            return;
        }
        var btBtn = document.getElementById('bluetooth-btn');
        btBtn.disabled = true;
        btBtn.textContent = 'Connecting...';

        navigator.bluetooth.requestDevice({
            filters: [{ services: [BLE_SERVICE_UUID] }]
        })
        .then(function(device) {
            bleDevice = device;
            device.addEventListener('gattserverdisconnected', function() {
                bleRxChar = null;
                bleTxChar = null;
                if (token) {
                    setConnectionState('disconnected');
                    showDisconnectWarning();
                }
            });
            return device.gatt.connect();
        })
        .then(function(server) {
            return server.getPrimaryService(BLE_SERVICE_UUID);
        })
        .then(function(service) {
            return Promise.all([
                service.getCharacteristic(BLE_TX_UUID),
                service.getCharacteristic(BLE_RX_UUID)
            ]);
        })
        .then(function(chars) {
            bleTxChar = chars[0];
            bleRxChar = chars[1];
            return bleTxChar.startNotifications();
        })
        .then(function() {
            bleTxChar.addEventListener('characteristicvaluechanged', function(event) {
                var decoder = new TextDecoder();
                var text = decoder.decode(event.target.value);
                try {
                    var msg = JSON.parse(text);
                    handleMessage(msg);
                } catch(e) {}
            });
            var key = keyInput.value.replace(/-/g, '');
            sendBLE({ type: 'auth', key: key });
            btBtn.disabled = false;
            btBtn.textContent = 'Connect via Bluetooth';
        })
        .catch(function(err) {
            showAuthError('Bluetooth: ' + err.message);
            btBtn.disabled = false;
            btBtn.textContent = 'Connect via Bluetooth';
        });
    }

    function sendBLE(msg) {
        if (!bleRxChar) return;
        if (token) msg.token = token;
        var encoder = new TextEncoder();
        var data = encoder.encode(JSON.stringify(msg));
        bleRxChar.writeValue(data).catch(function(err) {
            console.warn('BLE write error:', err);
        });
    }

    function sendMsg(msg) {
        if (bleRxChar) {
            sendBLE(msg);
        } else if (ws && ws.readyState === WebSocket.OPEN) {
            if (token) msg.token = token;
            ws.send(JSON.stringify(msg));
        }
    }

    function formatKey(key) {
        key = key.replace(/[^0-9]/g, '');
        var f = '';
        for (var i = 0; i < key.length; i++) {
            if (i > 0 && i % 4 === 0) f += '-';
            f += key[i];
        }
        return f;
    }

    function escapeHtml(text) {
        var div = document.createElement('div');
        div.textContent = text;
        return div.innerHTML;
    }

    setInterval(function() {
        sendMsg({ type: 'ping' });
    }, 15000);

    connectBtn.addEventListener('click', authenticate);
    document.getElementById('bluetooth-btn').addEventListener('click', connectBluetooth);
    document.getElementById('refresh-btn').addEventListener('click', refreshConnection);
    document.getElementById('test-alert-btn').addEventListener('click', sendTestAlert);
    document.getElementById('clear-alerts-btn').addEventListener('click', clearAlertHistory);
    armBtn.addEventListener('click', function() {
        if (!armed) toggleArm();
    });
    armBtn.addEventListener('mousedown', function(e) {
        if (armed) startDisarmHold();
    });
    armBtn.addEventListener('mouseup', cancelDisarmHold);
    armBtn.addEventListener('mouseleave', cancelDisarmHold);
    armBtn.addEventListener('touchstart', function(e) {
        if (armed) { e.preventDefault(); startDisarmHold(); }
    }, { passive: false });
    armBtn.addEventListener('touchend', function(e) {
        if (armed) { e.preventDefault(); cancelDisarmHold(); }
    });
    armBtn.addEventListener('touchcancel', cancelDisarmHold);
    document.getElementById('dismiss-disconnect-btn').addEventListener('click', dismissDisconnect);
    document.getElementById('pin-submit-btn').addEventListener('click', submitPin);
    document.getElementById('pin-cancel-btn').addEventListener('click', cancelPin);
    document.getElementById('pin-input').addEventListener('keydown', function(e) {
        if (e.key === 'Enter') submitPin();
    });
    document.getElementById('toggle-settings-btn').addEventListener('click', function() {
        var panel = document.getElementById('settings-panel');
        panel.classList.toggle('hidden');
        if (!panel.classList.contains('hidden')) requestConfig();
    });
    document.getElementById('save-settings-btn').addEventListener('click', saveSettings);
    document.getElementById('cfg-pin-enabled').addEventListener('change', function() {
        document.getElementById('pin-config-group').classList.toggle('hidden', !this.checked);
    });
    document.getElementById('dismiss-alert-btn').addEventListener('click', function(e) {
        e.stopPropagation();
        dismissAlert();
    });
    document.getElementById('dismiss-pause-btn').addEventListener('click', function(e) {
        e.stopPropagation();
        dismissAlertPause();
    });
    document.getElementById('dismiss-disable-btn').addEventListener('click', function(e) {
        e.stopPropagation();
        dismissAlertDisable();
    });
})();
