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

    // DOM elements
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

    // Auto-format pairing key input
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

    // Check URL params for key (from QR code)
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

        // Pre-check server reachability before attempting WebSocket
        var checkUrl = location.protocol + '//' + location.host + '/';
        var controller = typeof AbortController !== 'undefined' ? new AbortController() : null;
        var fetchOpts = controller ? { signal: controller.signal } : {};
        var fetchTimeout = controller ? setTimeout(function() { controller.abort(); }, 5000) : null;

        fetch(checkUrl, fetchOpts).then(function() {
            if (fetchTimeout) clearTimeout(fetchTimeout);
            connectWebSocket(key);
        }).catch(function() {
            if (fetchTimeout) clearTimeout(fetchTimeout);
            // If fetch fails but we might still succeed with WS (some browsers), try anyway
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
                    addAlert(msg.alert, msg.ts);
                    triggerAlarm(msg.alert.message);
                }
                break;

            case 'alarm_active':
                triggerAlarm(msg.alert ? msg.alert.message : 'Security alarm triggered!');
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
        connectedAt = Date.now();
        lastPongTime = Date.now();
        setConnectionState('connected');
        renderSensors();
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

            div.innerHTML =
                '<div class="sensor-info">' +
                    '<span class="' + dotClass + '"></span>' +
                    '<div>' +
                        '<div class="sensor-name">' + escapeHtml(s.display_name || s.name) + '</div>' +
                        '<div class="sensor-status">' + (s.status || (s.available === false ? 'Unavailable' : 'OK')) + '</div>' +
                    '</div>' +
                '</div>' +
                '<label class="toggle">' +
                    '<input type="checkbox" ' + (s.enabled ? 'checked' : '') + ' ' + (s.available === false ? 'disabled' : '') + ' data-sensor="' + escapeHtml(name) + '">' +
                    '<span class="slider"></span>' +
                '</label>';
            sensorList.appendChild(div);
        }
        sensorList.querySelectorAll('input[type=checkbox]').forEach(function(cb) {
            cb.addEventListener('change', function() {
                var cfg = {};
                cfg[this.dataset.sensor] = this.checked;
                sendMsg({ type: 'configure', sensors: cfg });
            });
        });
    }

    function addAlert(alert, ts) {
        var placeholder = alertFeed.querySelector('.muted');
        if (placeholder) placeholder.remove();

        var div = document.createElement('div');
        div.className = 'alert-item';
        var time = ts ? new Date(ts * 1000).toLocaleTimeString() : new Date().toLocaleTimeString();
        div.innerHTML =
            '<div class="alert-msg">' +
                '<span class="alert-dot ' + alert.level + '"></span>' +
                '<span class="alert-' + alert.level + '">' + escapeHtml(alert.message) + '</span>' +
            '</div>' +
            '<span class="alert-time">' + time + '</span>';
        alertFeed.insertBefore(div, alertFeed.firstChild);

        while (alertFeed.children.length > 50) {
            alertFeed.removeChild(alertFeed.lastChild);
        }
    }

    function toggleArm() {
        sendMsg({ type: armed ? 'disarm' : 'arm' });
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

    function triggerAlarm(message) {
        alertOverlayText.textContent = message;
        alertOverlay.classList.remove('hidden');

        var flash = true;
        var originalTitle = document.title;
        var titleFlash = setInterval(function() {
            document.title = flash ? 'ALERT! ' + message : originalTitle;
            flash = !flash;
        }, 500);
        setTimeout(function() { clearInterval(titleFlash); document.title = originalTitle; }, 30000);

        startAlarmSound();

        // Continuous vibration until dismissed
        if (navigator.vibrate) {
            navigator.vibrate([500, 200, 500, 200, 500]);
            vibrateInterval = setInterval(function() {
                if (navigator.vibrate) navigator.vibrate([500, 200, 500, 200, 500]);
            }, 2000);
        }

        // Send notification via Service Worker for persistent background alerts
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

    function dismissAlert() {
        alertOverlay.classList.add('hidden');
        stopAlarmSound();
        if (vibrateInterval) {
            clearInterval(vibrateInterval);
            vibrateInterval = null;
        }
        if (navigator.vibrate) navigator.vibrate(0);
        // Tell the server to stop the laptop alarm too
        sendMsg({ type: 'dismiss_alarm' });
    }

    var alarmHarmonicOsc = null;

    function startAlarmSound() {
        try {
            if (alarmCtx) stopAlarmSound();
            alarmCtx = new (window.AudioContext || window.webkitAudioContext)();

            // Main oscillator (fundamental)
            alarmOscillator = alarmCtx.createOscillator();
            var mainGain = alarmCtx.createGain();
            alarmOscillator.type = 'square';
            alarmOscillator.frequency.value = 880;
            mainGain.gain.value = 1.0;
            alarmOscillator.connect(mainGain);
            mainGain.connect(alarmCtx.destination);

            // Harmonic oscillator (octave up for piercing sound)
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
            var modulate = setInterval(function() {
                if (!alarmOscillator) { clearInterval(modulate); return; }
                alarmOscillator.frequency.value = high ? 880 : 660;
                alarmHarmonicOsc.frequency.value = high ? 1760 : 1320;
                high = !high;
            }, 400);
        } catch(e) {}
    }

    function stopAlarmSound() {
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

    function sendMsg(msg) {
        if (ws && ws.readyState === WebSocket.OPEN) {
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

    // Ping keepalive
    setInterval(function() {
        sendMsg({ type: 'ping' });
    }, 15000);

    // Event listeners (instead of inline onclick for mobile compatibility)
    connectBtn.addEventListener('click', authenticate);
    document.getElementById('refresh-btn').addEventListener('click', refreshConnection);
    document.getElementById('test-alert-btn').addEventListener('click', sendTestAlert);
    armBtn.addEventListener('click', toggleArm);
    document.getElementById('dismiss-disconnect-btn').addEventListener('click', dismissDisconnect);
    alertOverlay.addEventListener('click', dismissAlert);
    document.getElementById('dismiss-alert-btn').addEventListener('click', function(e) {
        e.stopPropagation();
        dismissAlert();
    });
})();
