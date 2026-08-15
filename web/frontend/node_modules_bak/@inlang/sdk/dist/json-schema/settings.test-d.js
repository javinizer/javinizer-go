import { ProjectSettings } from "./settings.js";
const settings = {};
// language tag and locale should be optional
settings.languageTags = undefined;
settings.sourceLanguageTag = undefined;
// @ts-expect-error - base locale is required
settings.baseLocale = undefined;
// @ts-expect-error - locales are required
settings.locales = undefined;
// expect no error
settings["plugin.mock"] = {
    key: "value",
    nested: {
        moreNested: {},
    },
};
//# sourceMappingURL=settings.test-d.js.map