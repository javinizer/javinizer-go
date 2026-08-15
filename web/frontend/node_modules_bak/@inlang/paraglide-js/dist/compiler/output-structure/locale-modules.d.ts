import type { ProjectSettings } from "@inlang/sdk";
import type { CompiledBundleWithMessages } from "../compile-bundle.js";
export declare function messageReferenceExpression(locale: string, bundleId: string): string;
export declare function generateOutput(compiledBundles: CompiledBundleWithMessages[], settings: Pick<ProjectSettings, "locales" | "baseLocale">, fallbackMap: Record<string, string | undefined>, experimentalMiddlewareLocaleSplitting?: boolean): Record<string, string>;
//# sourceMappingURL=locale-modules.d.ts.map