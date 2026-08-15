import { type Static } from "@sinclair/typebox";
export type ProjectSettings = Omit<Static<typeof ProjectSettings>, "languageTags" | "sourceLanguageTag"> & {
    /** @deprecated Use `baseLocale` */
    sourceLanguageTag?: string;
    /** @deprecated Use `locales` */
    languageTags?: string[];
    /** @deprecated This will soon be replaced by `Lix Validation Rules` */
    messageLintRuleLevels?: Record<string, "error" | "warning">;
} & Record<string, any>;
export declare const ProjectSettings: import("@sinclair/typebox").TObject<{
    $schema: import("@sinclair/typebox").TOptional<import("@sinclair/typebox").TLiteral<"https://inlang.com/schema/project-settings">>;
    baseLocale: import("@sinclair/typebox").TString;
    locales: import("@sinclair/typebox").TArray<import("@sinclair/typebox").TString>;
    sourceLanguageTag: import("@sinclair/typebox").TOptional<import("@sinclair/typebox").TString>;
    languageTags: import("@sinclair/typebox").TOptional<import("@sinclair/typebox").TArray<import("@sinclair/typebox").TString>>;
    /**
     * The modules to load.
     *
     * @example
     *  modules: [
     * 	  "https://cdn.jsdelivr.net/npm/@inlang/plugin-i18next@3/dist/index.js",
     * 	  "https://cdn.jsdelivr.net/npm/@inlang/plugin-csv@1/dist/index.js",
     *  ]
     */
    modules: import("@sinclair/typebox").TOptional<import("@sinclair/typebox").TArray<import("@sinclair/typebox").TIntersect<[import("@sinclair/typebox").TString, import("@sinclair/typebox").TString]>>>;
    experimental: import("@sinclair/typebox").TOptional<import("@sinclair/typebox").TRecord<import("@sinclair/typebox").TString, import("@sinclair/typebox").TLiteral<true>>>;
}>;
//# sourceMappingURL=settings.d.ts.map