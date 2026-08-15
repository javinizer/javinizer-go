/**
 * Change introduced in v2.
 *
 * The sourceLanguageTag and languageTags are now deprecated
 * in favor of baseLocale and locales.
 */
export function withLanguageTagToLocaleMigration(settings) {
    if (settings.sourceLanguageTag === undefined) {
        settings.sourceLanguageTag = settings.baseLocale;
    }
    if (settings.languageTags === undefined) {
        settings.languageTags = settings.locales;
    }
    if (settings.baseLocale === undefined) {
        settings.baseLocale = settings.sourceLanguageTag;
    }
    if (settings.locales === undefined) {
        settings.locales = settings.languageTags;
    }
    if (settings.sourceLanguageTag !== settings.baseLocale) {
        settings.sourceLanguageTag = settings.baseLocale;
    }
    if (settings.languageTags !== settings.locales) {
        settings.languageTags = settings.locales;
    }
    return settings;
}
//# sourceMappingURL=withLanguageTagToLocaleMigration.js.map