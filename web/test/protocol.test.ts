import { expect, it } from 'vitest';
import { SYSTEM_NOTICE } from '../src/lib/protocol';

// Every sensor the laptop registers. Kept here rather than imported because the
// point of the assertion below is that the two lists must never meet, and a
// shared constant would make that true by construction instead of checking it.
const LAPTOP_SENSORS = ['power', 'lid', 'usb', 'screen', 'network', 'input'];

// The laptop reports things about itself on the same channel it reports
// intrusions on. SYSTEM_NOTICE is the sensor name that marks the difference, so
// it has to be a name no real sensor could ever have: were a sensor called
// "system" added, its alerts would be shown as notices and the phone would stay
// quiet through a real one.
it('reserves a sensor name that no real sensor uses', () => {
    expect(LAPTOP_SENSORS).not.toContain(SYSTEM_NOTICE);
});

it('is the name the laptop sends for its own notices', () => {
    expect(SYSTEM_NOTICE).toBe('system');
});
