// @vitest-environment jsdom

import { h, render } from 'preact';
import { act } from 'preact/test-utils';
import { afterEach, expect, it } from 'vitest';
import { Log } from '../src/components/Log';
import { log, logFilter } from '../src/lib/store';

const host = document.createElement('div');
document.body.appendChild(host);
afterEach(() => {
    act(() => render(null, host));
    log.value = [];
});

it('redacts legacy stored system details while retaining real alarm descriptions', () => {
    logFilter.value = 'all';
    log.value = [
        { id: 1, sensor: 'system', level: 'warning', message: 'SQLSTATE secret-key /private/config', at: 1 },
        { id: 2, sensor: 'power', level: 'critical', message: 'Charger disconnected', at: 2 },
    ];
    act(() => render(h(Log, {}), host));
    expect(host.textContent).toContain('A setting needs attention. Check the settings on the device.');
    expect(host.textContent).not.toContain('SQLSTATE');
    expect(host.textContent).not.toContain('secret-key');
    expect(host.textContent).not.toContain('/private/config');
    expect(host.textContent).toContain('Charger disconnected');
});
