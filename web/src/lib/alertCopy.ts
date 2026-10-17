import { t } from './i18n';

const exact = new Set([
    'Charger disconnected!',
    'Charger reconnected',
    'Lid closed!',
    'Lid opened',
    'Screen turned off!',
    'Screen turned on',
    'USB device configuration changed!',
    'Sustained mouse or keyboard activity detected!',
    'Test alert triggered',
    'Test alert from console',
]);
const variable = [
    ['USB removed: ', 'USB removed: {device}'],
    ['USB connected: ', 'USB connected: {device}'],
    ['USB device removed: ', 'USB device removed: {device}'],
    ['USB device connected: ', 'USB device connected: {device}'],
    [
        'IP address changed (possible network switch). IPs: ',
        'IP address changed (possible network switch). IPs: {device}',
    ],
] as const;
/** Translate known protocol messages without altering opaque device names or diagnostic text. */
export function alertCopy(message: string): string {
    if (exact.has(message)) return t(message);
    for (const [prefix, key] of variable) {
        if (message.startsWith(prefix)) return t(key, { device: message.slice(prefix.length) });
    }
    return message;
}
