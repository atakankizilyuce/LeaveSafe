import { t } from './i18n';

const copy: Record<string, [string, string]> = {
    power: ['POWER', 'Watches the charger. Fires when power is disconnected or reconnected.'],
    lid: ['LID', 'Watches the lid. Fires when it is opened or closed.'],
    usb: ['USB', 'Watches the USB ports. Fires when a device is plugged in or pulled out.'],
    screen: ['SCREEN', 'Watches the display. Fires when the screen turns on or off.'],
    network: ['NETWORK', 'Watches the network interfaces. Fires when the IP address changes.'],
    input: ['INPUT', 'Watches the keyboard and trackpad. Fires on any input while armed.'],
};
export function sensorCaption(name: string, fallback: string): string {
    return copy[name] ? t(copy[name][0]) : fallback;
}
export function sensorDescription(name: string): string {
    return copy[name] ? t(copy[name][1]) : '';
}
