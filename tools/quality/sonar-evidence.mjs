import path from 'node:path';

export function evaluateEvidence(gate, issues, measures, hotspots) {
    const values = Object.fromEntries(measures.component.measures.map((item) => [item.metric, Number(item.value ?? item.period?.value ?? item.periods?.find((period) => period.index === 1)?.value ?? item.periods?.[0]?.value)]));
    const failures = [];
    if (gate.projectStatus.status !== 'OK') failures.push('Quality Gate did not pass');
    if (issues.paging.total !== 0) failures.push('New issues are present');
    if (hotspots.paging.total !== 0 || values.security_hotspots !== 0 || values.new_security_hotspots !== 0) failures.push('Security hotspots are present');
    if (values.vulnerabilities !== 0 || values.new_vulnerabilities !== 0) failures.push('Security vulnerabilities are present or unavailable');
    if (!Number.isFinite(values.new_lines_to_cover)) failures.push('New-code coverage scope is unavailable');
    if (values.security_rating !== 1 || values.new_security_rating !== 1) failures.push('Security rating is not A');
    if (!Number.isFinite(values.coverage) || values.coverage < 80) failures.push('Overall coverage is below 80% or unavailable');
    if (values.new_lines_to_cover > 0 && (!Number.isFinite(values.new_coverage) || values.new_coverage < 80)) failures.push('New-code coverage is below 80% or unavailable');
    if (failures.length) throw new Error(failures.join('; '));
    return values;
}

export async function readEvidence({ token, project, branch, pullRequest, fetcher = fetch }) {
    if (!token) throw new Error('SONAR_TOKEN is not configured; Sonar verification did not run');
    const scope = pullRequest ? { pullRequest } : { branch };
    const get = async (endpoint, parameters) => {
        const url = new URL(`https://sonarcloud.io/api/${endpoint}`);
        for (const [key, value] of Object.entries({ ...parameters, ...scope })) url.searchParams.set(key, value);
        const response = await fetcher(url, { headers: { Authorization: `Bearer ${token}` } });
        if (!response.ok) throw new Error(`Sonar evidence request failed: ${endpoint} (${response.status})`);
        return response.json();
    };
    const [gate, issues, measures, hotspots] = await Promise.all([
        get('qualitygates/project_status', { projectKey: project }),
        get('issues/search', { componentKeys: project, inNewCodePeriod: 'true', issueStatuses: 'OPEN,CONFIRMED,ACCEPTED', ps: '1' }),
        get('measures/component', { component: project, metricKeys: 'vulnerabilities,new_vulnerabilities,coverage,new_coverage,new_lines_to_cover,security_hotspots,new_security_hotspots,security_rating,new_security_rating' }),
        get('hotspots/search', { projectKey: project, status: 'TO_REVIEW', ps: '1' }),
    ]);
    return { values: evaluateEvidence(gate, issues, measures, hotspots), gate, issueCount: issues.paging.total, hotspotCount: hotspots.paging.total };
}

if (process.argv[1] && path.resolve(process.argv[1]) === path.join(import.meta.dirname, 'sonar-evidence.mjs')) {
    const evidence = await readEvidence({ token: process.env.SONAR_TOKEN, project: 'atakankizilyuce_LeaveSafe', branch: process.env.BRANCH, pullRequest: process.env.PR_NUMBER });
    // Only numeric measurements reach the CI log; remote descriptions are not logged.
    console.log('Sonar gate OK; issues=%d; hotspots=%d; vulnerabilities=%d; new vulnerabilities=%d; coverage=%d; new coverage=%d; new lines=%d; security rating=%d; new security rating=%d',
        Number(evidence.issueCount), Number(evidence.hotspotCount),
        Number(evidence.values.vulnerabilities), Number(evidence.values.new_vulnerabilities),
        Number(evidence.values.coverage), Number(evidence.values.new_coverage),
        Number(evidence.values.new_lines_to_cover), Number(evidence.values.security_rating),
        Number(evidence.values.new_security_rating));
}
