import { expect, it, vi } from 'vitest';
import { evaluateEvidence, readEvidence } from '../../tools/quality/sonar-evidence.mjs';

const gate = { projectStatus: { status: 'OK' } };
const clear = { paging: { total: 0 } };
const measured = (overrides = {}) => ({
    component: {
        measures: Object.entries({
            vulnerabilities: 0,
            new_vulnerabilities: 0,
            coverage: 95,
            new_coverage: 96,
            new_lines_to_cover: 20,
            security_hotspots: 0,
            new_security_hotspots: 0,
            security_rating: 1,
            new_security_rating: 1,
            ...overrides,
        }).map(([metric, value]) =>
            metric.startsWith('new_')
                ? { metric, period: { value: String(value) } }
                : { metric, value: String(value) },
        ),
    },
});
it('accepts only green quality, zero new issues/security concerns and >=80% coverage', () => {
    expect(evaluateEvidence(gate, clear, measured(), clear).coverage).toBe(95);
    expect(() => evaluateEvidence({ projectStatus: { status: 'ERROR' } }, clear, measured(), clear)).toThrow(
        'Quality Gate',
    );
    expect(() => evaluateEvidence(gate, { paging: { total: 1 } }, measured(), clear)).toThrow('New issues');
    expect(() => evaluateEvidence(gate, clear, measured({ security_hotspots: 1 }), clear)).toThrow(
        'hotspots',
    );
    expect(() => evaluateEvidence(gate, clear, measured(), { paging: { total: 1 } })).toThrow('hotspots');
    expect(() => evaluateEvidence(gate, clear, measured({ security_rating: 2 }), clear)).toThrow('rating');
    expect(() => evaluateEvidence(gate, clear, measured({ coverage: 79.9 }), clear)).toThrow(
        'Overall coverage',
    );
    expect(() => evaluateEvidence(gate, clear, measured({ new_coverage: 79.9 }), clear)).toThrow(
        'New-code coverage',
    );
    expect(() =>
        evaluateEvidence(gate, clear, measured({ new_lines_to_cover: 0, new_coverage: NaN }), clear),
    ).not.toThrow();
});
it('fails explicitly when access is absent or a remote request fails', async () => {
    await expect(readEvidence({ token: '' })).rejects.toThrow('not configured');
    await expect(
        readEvidence({
            token: 'test-token',
            project: 'test',
            branch: 'main',
            fetcher: async () => ({ ok: false, status: 401 }),
        }),
    ).rejects.toThrow('401');
});
it.each([{ branch: 'main' }, { pullRequest: '123' }])(
    'uses the requested branch/PR scope without exposing its credential',
    async (scope) => {
        const fetcher = vi.fn(async (url) => ({
            ok: true,
            json: async () =>
                url.pathname.includes('qualitygates')
                    ? gate
                    : url.pathname.includes('measures')
                      ? measured()
                      : clear,
        }));
        const result = await readEvidence({ token: 'test-token', project: 'test', ...scope, fetcher });
        expect(fetcher).toHaveBeenCalledTimes(4);
        expect(JSON.stringify(result)).not.toContain('test-token');
        for (const [url, options] of fetcher.mock.calls) {
            expect(url.origin).toBe('https://sonarcloud.io');
            expect(url.searchParams.get(scope.pullRequest ? 'pullRequest' : 'branch')).toBe(
                scope.pullRequest ?? scope.branch,
            );
            expect(options.headers.Authorization).toBe('Bearer test-token');
        }
    },
);

it('reads period arrays used by SonarCloud and refuses unavailable security measures', () => {
    const response = measured();
    for (const item of response.component.measures) {
        if (item.period) {
            item.periods = [{ index: 1, ...item.period }];
            delete item.period;
        }
    }
    expect(evaluateEvidence(gate, clear, response, clear).new_coverage).toBe(96);
    expect(() => evaluateEvidence(gate, clear, measured({ vulnerabilities: 1 }), clear)).toThrow(
        'vulnerabilities',
    );
    expect(() => evaluateEvidence(gate, clear, measured({ new_lines_to_cover: NaN }), clear)).toThrow(
        'scope',
    );
    expect(() => evaluateEvidence(gate, clear, measured({ vulnerabilities: NaN }), clear)).toThrow(
        'vulnerabilities',
    );
});
