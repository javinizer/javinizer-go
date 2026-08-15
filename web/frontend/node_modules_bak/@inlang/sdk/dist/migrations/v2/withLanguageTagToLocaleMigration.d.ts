import type { ProjectSettings } from "../../json-schema/settings.js";
/**
 * Change introduced in v2.
 *
 * The sourceLanguageTag and languageTags are now deprecated
 * in favor of baseLocale and locales.
 */
export declare function withLanguageTagToLocaleMigration(settings: ProjectSettings): ProjectSettings;
//# sourceMappingURL=withLanguageTagToLocaleMigration.d.ts.map