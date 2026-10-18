/** @vitest-environment jsdom */

import { h, render } from 'preact';
import { act } from 'preact/test-utils';
import { afterEach, expect, it, vi } from 'vitest';
import { LanguagePicker } from '../src/components/LanguagePicker';
import { alertCopy } from '../src/lib/alertCopy';
import { clock, distance, since } from '../src/lib/format';
import {
    direction,
    isComplete,
    language,
    normalizeLanguage,
    number,
    setLanguage,
    supportedLanguages,
    systemLanguage,
    t,
} from '../src/lib/i18n';
import { sensorCaption, sensorDescription } from '../src/lib/sensorCopy';
import english from '../src/locales/en.json';

afterEach(() => {
    setLanguage('en');
    localStorage.clear();
    vi.restoreAllMocks();
    vi.unstubAllGlobals();
});

it('resolves regional/script aliases without calling traditional Mandarin Cantonese', () => {
    for (const [input, expected] of [
        ['en-US', 'en'],
        ['tr_TR', 'tr'],
        ['zh-CN', 'zh-Hans'],
        ['zh', 'zh-Hans'],
        ['zh-SG', 'zh-Hans'],
        ['pnb', 'pnb-Arab'],
        ['pa-Arab-PK', 'pnb-Arab'],
        ['yue-HK', 'yue-Hant'],
        ['fil-PH', 'tl'],
        ['fil', 'tl'],
    ] as const)
        expect(normalizeLanguage(input)).toBe(expected);
    expect(normalizeLanguage('zh-TW')).toBeNull();
    expect(normalizeLanguage('zh-Hant')).toBeNull();
    expect(normalizeLanguage('xx')).toBeNull();
});
it('uses the first available system language and defaults safely', () => {
    expect(systemLanguage(['xx', 'tr-TR', 'en'])).toBe('tr');
    expect(systemLanguage(['xx'])).toBe('en');
    expect(systemLanguage([])).toBe('en');
});
it('never offers an incomplete catalog as translated', () => {
    expect(isComplete({})).toBe(false);
    expect(isComplete({ ...english, Settings: '' })).toBe(false);
    expect(isComplete({ ...english, Settings: { one: 'one' } })).toBe(false);
    expect(isComplete(english)).toBe(true);
    expect(supportedLanguages.some(([code]) => code === 'tr')).toBe(true);
});
it('persists selection and updates native document language/direction', () => {
    document.head.innerHTML =
        '<meta name="description" content="English"><link rel="manifest" href="/manifest.webmanifest">';
    setLanguage('tr-TR');
    expect(document.querySelector('meta[name="description"]')?.getAttribute('content')).toBe(
        t("Leave it. We'll watch it."),
    );
    expect(document.querySelector('link[rel="manifest"]')?.getAttribute('href')).toBe(
        '/manifests/tr.webmanifest',
    );
    expect(localStorage.getItem('leavesafe.language')).toBe('tr');
    expect(language.value).toBe('tr');
    expect(document.documentElement.lang).toBe('tr');
    expect(document.documentElement.dir).toBe('ltr');
    for (const locale of ['ar', 'arz', 'ur', 'pnb-Arab'] as const) expect(direction(locale)).toBe('rtl');
    expect(direction('yue-Hant')).toBe('ltr');
    setLanguage('');
    expect(localStorage.getItem('leavesafe.language')).toBeNull();
});
it('changes visible text reactively without reloading or closing the page', async () => {
    const host = document.createElement('div');
    await act(() => render(h(LanguagePicker, {}), host));
    expect(host.textContent).toContain('Language');
    await act(() => {
        setLanguage('tr');
    });
    expect(host.textContent).toContain('Dil');
    const select = host.querySelector('select');
    if (!select) throw new Error('language picker missing');
    select.value = 'en';
    await act(() => select.dispatchEvent(new Event('change', { bubbles: true })));
    expect(host.textContent).toContain('Language');
    render(null, host);
});
it('keeps selection usable when browser storage is blocked', () => {
    vi.spyOn(Storage.prototype, 'setItem').mockImplementation(() => {
        throw new Error('blocked');
    });
    expect(() => setLanguage('tr')).not.toThrow();
    expect(t('Settings')).toBe('Ayarlar');
});
it('formats whole sentences, plural categories and numeric variables', () => {
    setLanguage('en');
    expect(t('{count} sensors ready', { count: 1 })).toBe('1 sensor ready');
    expect(t('{count} sensors ready', { count: 2 })).toBe('2 sensors ready');
    setLanguage('tr');
    expect(t('{count} sensors ready', { count: 2 })).toBe('2 sensör hazır');
    expect(number(1234.5)).toBe('1.234,5');
    expect(distance(1250)).toBe('1,3 km');
    expect(since(Date.now())).toBe(t('just now'));
    expect(clock(Date.now())).toMatch(/\d/);
    expect(() => t('missing')).toThrow('Missing localization key');
    expect(() => t('{count} sensors ready')).toThrow('Missing localization variable');
});
it('translates known sensor alerts while preserving device names and unknown diagnostics', () => {
    setLanguage('tr');
    expect(alertCopy('Charger disconnected!')).toBe('Şarj bağlantısı kesildi!');
    expect(alertCopy('USB removed: My Device')).toBe('USB çıkarıldı: My Device');
    expect(alertCopy('Unknown diagnostic')).toBe('Unknown diagnostic');
    expect(sensorCaption('power', 'Power')).toBe(t('POWER'));
    expect(sensorCaption('future', 'Custom Sensor')).toBe('Custom Sensor');
    expect(sensorDescription('power')).toBe(
        t('Watches the charger. Fires when power is disconnected or reconnected.'),
    );
    expect(sensorDescription('future')).toBe('');
});

it('sends public translated copy to the active service worker', async () => {
    const postMessage = vi.fn();
    vi.stubGlobal('navigator', { languages: ['en'], serviceWorker: { controller: { postMessage } } });
    setLanguage('tr');
    expect(postMessage).toHaveBeenCalledWith(
        expect.objectContaining({ type: 'locale', language: 'tr', title: 'LeaveSafe uyarısı' }),
    );
});
