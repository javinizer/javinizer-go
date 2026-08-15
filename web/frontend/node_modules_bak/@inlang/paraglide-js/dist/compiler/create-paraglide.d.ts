import type { Runtime } from "./runtime/type.js";
import type { ServerRuntime } from "./server/type.js";
import { type CompilerOptions } from "./compiler-options.js";
/**
 * Creates an in-memory Paraglide module for use in tests and non-bundled environments.
 *
 * @example
 *	 const blob = await fs.readFile("./project.inlang");
 *   const paraglide = await createParaglide({
 *     blob,
 *     // other options
 *   })
 *
 *   // Access functions
 *   paraglide.localizeUrl("https://example.com", { locale: "de" })
 *   app.use(paraglide.paraglideMiddleware())
 *
 * You can load a project from a directory as well.
 *
 * @example
 *   import { loadProjectFromDirectory } from "@inlang/sdk";
 *
 * 	 const project = await loadProjectFromDirectory("./project");
 *
 *   const paraglide = await createParaglide({
 *     blob: await project.toBlob(),
 *     // other options
 *   })
 *
 *   // Access functions
 *   paraglide.localizeUrl("https://example.com", { locale: "de" })
 *   app.use(paraglide.paraglideMiddleware())
 *
 */
export declare function createParaglide(args: {
    /**
     * The inlang file.
     */
    blob: Blob;
} & Omit<CompilerOptions, "outdir" | "project" | "fs">): Promise<Runtime & ServerRuntime & {
    m: Record<string, unknown>;
}>;
//# sourceMappingURL=create-paraglide.d.ts.map