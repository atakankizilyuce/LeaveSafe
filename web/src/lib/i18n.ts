import { signal } from '@preact/signals';
import english from '../locales/en.json';

export type Message = string | Record<string, string>;
export type Catalog = Record<string, Message>;
const catalogs = import.meta.glob<Catalog>('../locales/*.json', { eager: true, import: 'default' });
const available = new Map(
    Object.entries(catalogs).map(([path, catalog]) => [path.split('/').pop()?.replace('.json', ''), catalog]),
);
export const languages = [
    ['en', 'English'],
    ['zh-Hans', '简体中文'],
    ['hi', 'हिन्दी'],
    ['es', 'Español'],
    ['ar', 'العربية'],
    ['fr', 'Français'],
    ['bn', 'বাংলা'],
    ['pt', 'Português'],
    ['id', 'Bahasa Indonesia'],
    ['ur', 'اردو'],
    ['ru', 'Русский'],
    ['de', 'Deutsch'],
    ['ja', '日本語'],
    ['pcm', 'Naijá'],
    ['arz', 'مصري'],
    ['mr', 'मराठी'],
    ['vi', 'Tiếng Việt'],
    ['te', 'తెలుగు'],
    ['sw', 'Kiswahili'],
    ['ha', 'Hausa'],
    ['tr', 'Türkçe'],
    ['pnb-Arab', 'پنجابی'],
    ['tl', 'Filipino'],
    ['ta', 'தமிழ்'],
    ['yue-Hant', '粵語'],
] as const;
export type Language = (typeof languages)[number][0];
const storageKey = 'leavesafe.language';
const rtl = new Set<Language>(['ar', 'arz', 'ur', 'pnb-Arab']);
/** Intl does not recognize every product locale. These aliases affect formatting only. */
const formattingAliases: Partial<Record<Language, string>> = {
    arz: 'ar-EG',
    pcm: 'en-NG',
    'pnb-Arab': 'pa-Arab-PK',
    'yue-Hant': 'zh-Hant-HK',
    tl: 'fil',
};
const pluralAliases: Partial<Record<Language, string>> = {
    arz: 'ar',
    pcm: 'am',
    'pnb-Arab': 'pa',
    'yue-Hant': 'zh',
    ha: 'az',
};

export function normalizeLanguage(value: string): Language | null {
    const tag = value.replaceAll('_', '-').toLowerCase();
    const exact = languages.find(([code]) => code.toLowerCase() === tag);
    if (exact) return exact[0];
    if (tag === 'pa-arab' || tag.startsWith('pa-arab-') || tag === 'pnb' || tag.startsWith('pnb-'))
        return 'pnb-Arab';
    if (tag === 'yue' || tag.startsWith('yue-')) return 'yue-Hant';
    // Traditional Mandarin is not silently relabeled as Cantonese.
    if (tag === 'zh' || tag.startsWith('zh-hans') || tag === 'zh-cn' || tag === 'zh-sg') return 'zh-Hans';
    if (tag === 'fil' || tag.startsWith('fil-')) return 'tl';
    return languages.find(([code]) => code === tag.split('-')[0])?.[0] ?? null;
}

export function isComplete(catalog: Catalog): boolean {
    return Object.keys(english).every((key) => {
        const value = catalog[key];
        return typeof value === 'string' ? value.trim().length > 0 : !!value?.other;
    });
}
export const supportedLanguages = languages.filter(([code]) => {
    const catalog = available.get(code);
    return catalog !== undefined && isComplete(catalog);
});
function supported(value: string): Language | null {
    const normalized = normalizeLanguage(value);
    return supportedLanguages.some(([code]) => code === normalized) ? normalized : null;
}
export function systemLanguage(preferences: readonly string[]): Language {
    for (const preference of preferences) {
        const found = supported(preference);
        if (found) return found;
    }
    return 'en';
}
function storedPreference(): string {
    try {
        return localStorage.getItem(storageKey) ?? '';
    } catch {
        return '';
    }
}
export const languagePreference = signal(storedPreference());
export const language = signal<Language>(
    supported(languagePreference.value) ??
        systemLanguage(typeof navigator === 'undefined' ? [] : navigator.languages),
);
export function intlLocale(): string {
    return formattingAliases[language.value] ?? language.value;
}
export function direction(code: Language): 'rtl' | 'ltr' {
    return rtl.has(code) ? 'rtl' : 'ltr';
}
function reflectLanguage() {
    if (typeof document === 'undefined') return;
    document.documentElement.lang = language.value;
    document.documentElement.dir = direction(language.value);
    document
        .querySelector('meta[name="description"]')
        ?.setAttribute('content', t("Leave it. We'll watch it."));
    document
        .querySelector('link[rel="manifest"]')
        ?.setAttribute('href', `/manifests/${language.value}.webmanifest`);
}
export function setLanguage(preference: string) {
    const chosen = supported(preference);
    languagePreference.value = chosen ?? '';
    language.value = chosen ?? systemLanguage(typeof navigator === 'undefined' ? [] : navigator.languages);
    try {
        if (chosen) localStorage.setItem(storageKey, chosen);
        else localStorage.removeItem(storageKey);
    } catch {
        /* Private browsing can refuse storage; the current page still changes. */
    }
    reflectLanguage();
    syncWorkerLanguage();
}
reflectLanguage();
if (typeof window !== 'undefined')
    window.addEventListener('languagechange', () => {
        if (!languagePreference.value) setLanguage('');
    });

export function number(value: number, options: Intl.NumberFormatOptions = {}): string {
    return new Intl.NumberFormat(intlLocale(), options).format(value);
}
/** Full messages, including plural forms, keep variables in the translator's chosen order. */
export function t(key: string, values: Record<string, string | number> = {}): string {
    const catalog = available.get(language.value);
    const message = catalog?.[key];
    // A missing key is a programming/catalog error, never a silently English translation.
    if (message === undefined) throw new Error(`Missing localization key: ${key}`);
    let pattern: string;
    if (typeof message === 'string') pattern = message;
    else {
        const count = Number(values.count);
        const rule = new Intl.PluralRules(pluralAliases[language.value] ?? intlLocale()).select(count);
        pattern = message[`=${count}`] ?? message[rule] ?? message.other ?? '';
    }
    return pattern.replace(/\{([a-zA-Z][a-zA-Z0-9]*)\}/g, (_, name: string) => {
        const value = values[name];
        if (value === undefined) throw new Error(`Missing localization variable: ${name}`);
        return typeof value === 'number' ? number(value) : value;
    });
}

/** The worker persists only public translated copy, never pairing/account credentials. */
export function syncWorkerLanguage() {
    if (typeof navigator === 'undefined' || !navigator.serviceWorker?.controller) return;
    navigator.serviceWorker.controller.postMessage({
        type: 'locale',
        language: language.value,
        title: t('LeaveSafe alert'),
        fallback: t('Something touched your laptop.'),
        messages: available.get(language.value),
    });
}
if (typeof navigator !== 'undefined' && navigator.serviceWorker) {
    navigator.serviceWorker.addEventListener('controllerchange', syncWorkerLanguage);
}
