import { languagePreference, setLanguage, supportedLanguages, t } from '../lib/i18n';

export function LanguagePicker() {
    return (
        <label class="language-picker">
            <span>{t('Language')}</span>
            <select
                value={languagePreference.value}
                onChange={(event) => setLanguage(event.currentTarget.value)}
            >
                <option value="">{t('Use the device language')}</option>
                {supportedLanguages.map(([code, name]) => (
                    <option key={code} value={code} lang={code}>
                        {name}
                    </option>
                ))}
            </select>
        </label>
    );
}
