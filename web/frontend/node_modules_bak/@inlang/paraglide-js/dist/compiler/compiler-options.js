export const defaultCompilerOptions = {
    outputStructure: "message-modules",
    emitGitIgnore: true,
    includeEslintDisableComment: true,
    emitPrettierIgnore: true,
    emitReadme: true,
    emitTsDeclarations: false,
    cleanOutdir: true,
    disableAsyncLocalStorage: false,
    experimentalMiddlewareLocaleSplitting: false,
    localStorageKey: "PARAGLIDE_LOCALE",
    isServer: "typeof window === 'undefined'",
    strategy: ["cookie", "globalVariable", "baseLocale"],
    cookieName: "PARAGLIDE_LOCALE",
    cookieMaxAge: 60 * 60 * 24 * 400,
    cookieDomain: "",
};
//# sourceMappingURL=compiler-options.js.map