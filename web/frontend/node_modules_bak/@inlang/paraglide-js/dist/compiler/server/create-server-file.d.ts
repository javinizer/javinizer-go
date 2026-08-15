import type { CompiledBundleWithMessages } from "../compile-bundle.js";
import type { CompilerOptions } from "../compiler-options.js";
/**
 * Returns the code for the `runtime.js` module
 */
export declare function createServerFile(args: {
    compiledBundles: CompiledBundleWithMessages[];
    compilerOptions: {
        experimentalMiddlewareLocaleSplitting: NonNullable<CompilerOptions["experimentalMiddlewareLocaleSplitting"]>;
        disableAsyncLocalStorage: NonNullable<CompilerOptions["disableAsyncLocalStorage"]>;
    };
}): string;
//# sourceMappingURL=create-server-file.d.ts.map