import { useState } from 'preact/hooks';
import { ago, distance } from '../lib/format';
import { captureAnchor } from '../lib/geo';
import { armed, location, send, showToast } from '../lib/store';

const SOURCE: Record<string, string> = {
    phone: 'Your phone, when you armed it',
    wifi: 'Wi-Fi positioning',
    ip: 'IP address lookup',
};

/**
 * Where the machine is, stated no more confidently than it is known.
 *
 * The accuracy radius is given as a distance in plain words rather than as a
 * number nobody reads, because a 25 km circle and a 30 m circle are different
 * kinds of answer and the difference matters more than the coordinates do.
 */
export function Position() {
    const [mapOpen, setMapOpen] = useState(false);
    const loc = location.value;

    if (!loc?.enabled) return null;

    const fix = loc.fix;
    const lat = Number(fix?.lat);
    const lon = Number(fix?.lon);
    const usable = fix !== undefined && Number.isFinite(lat) && Number.isFinite(lon);

    return (
        <section class="card">
            <div class="card-head">
                <h2 class="readout">Position</h2>
                <button
                    type="button"
                    class="chip"
                    onClick={() => {
                        send({ type: 'get_location' });
                        if (armed.value) captureAnchor();
                    }}
                >
                    Refresh
                </button>
            </div>

            {!usable ? (
                <p class="empty">
                    {loc.available
                        ? 'Looking for a position…'
                        : 'No position source works on this machine. The README lists what each platform supports.'}
                </p>
            ) : (
                <>
                    {loc.moved && (
                        <div class="moved">
                            Moved {distance(Number(loc.moved_m) || 0)} from where you armed it
                        </div>
                    )}

                    <div class="coords figure">
                        {lat.toFixed(5)}, {lon.toFixed(5)}
                    </div>
                    <div class="coords-note">Accurate to about {distance(Number(fix.accuracy_m) || 0)}</div>

                    <div class="row">
                        <span>Source</span>
                        <span>{SOURCE[fix.source ?? ''] ?? fix.source ?? 'unknown'}</span>
                    </div>
                    <div class="row">
                        <span>Updated</span>
                        <span class="figure">{ago(fix.ts)}</span>
                    </div>
                    {fix.label && (
                        <div class="row">
                            <span>Place</span>
                            <span>{fix.label}</span>
                        </div>
                    )}

                    <div class="pos-actions">
                        <button
                            type="button"
                            class="chip"
                            onClick={() => {
                                const text = `${lat.toFixed(5)}, ${lon.toFixed(5)}`;
                                navigator.clipboard
                                    ?.writeText(text)
                                    .then(() => showToast('Coordinates copied'))
                                    .catch(() => showToast('Could not copy'));
                            }}
                        >
                            Copy
                        </button>
                        <a
                            class="chip"
                            target="_blank"
                            rel="noopener noreferrer"
                            href={`https://www.openstreetmap.org/?mlat=${lat}&mlon=${lon}#map=17/${lat}/${lon}`}
                        >
                            Open in maps
                        </a>
                        <button
                            type="button"
                            class={mapOpen ? 'chip on' : 'chip'}
                            aria-pressed={mapOpen}
                            onClick={() => setMapOpen(!mapOpen)}
                        >
                            {mapOpen ? 'Hide map' : 'Show map'}
                        </button>
                    </div>

                    {mapOpen && <TileMap lat={lat} lon={lon} accuracy={Number(fix.accuracy_m) || 0} />}
                </>
            )}
        </section>
    );
}

/**
 * Opt-in, every time. Everything above this point is drawn from data the laptop
 * already sent; opening the map is the only thing on this screen that reaches
 * out to a third party, so it stays a decision rather than a default.
 */
function TileMap({ lat, lon, accuracy }: { lat: number; lon: number; accuracy: number }) {
    const span = Math.max(0.002, Math.min(0.5, accuracy / 60000));
    const bbox = [lon - span, lat - span, lon + span, lat + span].join(',');

    return (
        <>
            <iframe
                class="map rise"
                title="Map of the reported position"
                loading="lazy"
                src={`https://www.openstreetmap.org/export/embed.html?bbox=${bbox}&layer=mapnik&marker=${lat},${lon}`}
            />
            <p class="map-note readout">Tiles loaded from openstreetmap.org</p>
        </>
    );
}
