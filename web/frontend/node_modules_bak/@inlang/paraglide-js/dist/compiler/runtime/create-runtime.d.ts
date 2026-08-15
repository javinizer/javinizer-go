import type { CompilerOptions } from "../compiler-options.js";
/**
 * Returns the code for the `runtime.js` module
 */
export declare function createRuntimeFile(args: {
    baseLocale: string;
    locales: string[];
    compilerOptions: {
        strategy: NonNullable<CompilerOptions["strategy"]>;
        routeStrategies?: CompilerOptions["routeStrategies"];
        cookieName: NonNullable<CompilerOptions["cookieName"]>;
        cookieMaxAge: NonNullable<CompilerOptions["cookieMaxAge"]>;
        cookieDomain: CompilerOptions["cookieDomain"];
        urlPatterns?: CompilerOptions["urlPatterns"];
        experimentalMiddlewareLocaleSplitting: CompilerOptions["experimentalMiddlewareLocaleSplitting"];
        isServer: CompilerOptions["isServer"];
        experimentalStaticLocale?: CompilerOptions["experimentalStaticLocale"];
        localStorageKey: CompilerOptions["localStorageKey"];
        disableAsyncLocalStorage: NonNullable<CompilerOptions["disableAsyncLocalStorage"]>;
    };
}): string;
//# sourceMappingURL=create-runtime.d.ts.map