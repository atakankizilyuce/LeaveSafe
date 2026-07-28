import { send } from './store';

/**
 * Hand the laptop this phone's own position.
 *
 * The laptop has no GPS. The phone does, and at the moment you arm the system
 * the two are in the same place — so this is the most precise statement
 * available about where the laptop is. It stops being true as soon as the
 * laptop is carried off, which is why the laptop keeps checking it against its
 * own live sources rather than trusting it forever.
 */
export function captureAnchor(): void {
    if (!navigator.geolocation) return;

    navigator.geolocation.getCurrentPosition(
        (pos) => {
            send({
                type: 'location_anchor',
                location: {
                    lat: pos.coords.latitude,
                    lon: pos.coords.longitude,
                    accuracy_m: pos.coords.accuracy || 0,
                    ts: Math.floor(pos.timestamp / 1000),
                },
            });
        },
        () => {
            // Permission refused, or no fix indoors. The laptop's own sources
            // still work, so this is not worth interrupting the user for.
        },
        { enableHighAccuracy: true, timeout: 15000, maximumAge: 60000 },
    );
}
