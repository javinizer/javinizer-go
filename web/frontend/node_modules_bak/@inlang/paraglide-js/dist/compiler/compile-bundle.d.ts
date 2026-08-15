import type { Bundle, BundleNested, Message, ProjectSettings } from "@inlang/sdk";
import type { Compiled } from "./types.js";
import { type InputMatchTypes } from "./jsdoc-types.js";
export type CompiledBundleWithMessages = {
    /** The compilation result for the bundle index */
    bundle: Compiled<Bundle>;
    /** The compilation results for the languages */
    messages: {
        [locale: string]: Compiled<Message>;
    };
    /** Match literal types inferred from bundle variants */
    matchTypes: InputMatchTypes;
    /** Shared typedef name for bundle input types used across emitted JSDoc */
    inputTypeAliasName?: string;
};
/**
 * Compiles all the messages in the bundle and returns an index-function + each compiled message
 */
export declare const compileBundle: (args: {
    bundle: BundleNested;
    fallbackMap: Record<string, string | undefined>;
    messageReferenceExpression: (locale: string, bundleId: string) => string;
    settings?: ProjectSettings;
    experimentalMiddlewareLocaleSplitting?: boolean;
}) => CompiledBundleWithMessages;
export declare function toBundleInputTypeAliasName(safeBundleId: string): string;
//# sourceMappingURL=compile-bundle.d.ts.map