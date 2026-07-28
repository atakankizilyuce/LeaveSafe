import { clock } from '../lib/format';
import { clearLog, log, logFilter, logNewestFirst, send, visibleLog } from '../lib/store';

const FILTERS: Array<{ value: 'all' | 'critical' | 'warning'; label: string }> = [
    { value: 'all', label: 'All' },
    { value: 'critical', label: 'Critical' },
    { value: 'warning', label: 'Warning' },
];

/** Everything that has happened, newest first, in the panel's own voice. */
export function Log() {
    const entries = visibleLog.value;

    return (
        <section class="card">
            <div class="card-head">
                <h2 class="readout">Log</h2>
                <div class="log-tools">
                    <button type="button" class="chip" onClick={() => send({ type: 'test_alert' })}>
                        Send test
                    </button>
                    <button type="button" class="chip" onClick={clearLog} disabled={log.value.length === 0}>
                        Clear
                    </button>
                </div>
            </div>

            <div class="log-filters">
                {FILTERS.map((f) => (
                    <button
                        key={f.value}
                        type="button"
                        class="chip"
                        aria-pressed={logFilter.value === f.value}
                        onClick={() => {
                            logFilter.value = f.value;
                        }}
                    >
                        {f.label}
                    </button>
                ))}
                <button
                    type="button"
                    class="chip log-order"
                    onClick={() => {
                        logNewestFirst.value = !logNewestFirst.value;
                    }}
                >
                    {logNewestFirst.value ? 'Newest' : 'Oldest'}
                </button>
            </div>

            {entries.length === 0 ? (
                <p class="empty">Nothing has happened yet. That is the good outcome.</p>
            ) : (
                <ol class="log-list">
                    {entries.map((entry) => (
                        <li key={entry.id} class="log-entry" data-level={entry.level}>
                            <span class="log-time figure">{clock(entry.at)}</span>
                            <span class="log-msg">{entry.message}</span>
                        </li>
                    ))}
                </ol>
            )}
        </section>
    );
}
