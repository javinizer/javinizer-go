import fs from "node:fs";
import { toSafeModuleId } from "../safe-module-id.js";
/**
 * Returns the code for the `runtime.js` module
 */
export function createServerFile(args) {
    let code = `
import * as runtime from "./runtime.js";

${injectCode("./middleware.js")}
`;
    const asyncLocalStorageMarker = "// %async-local-storage";
    const markerIndex = code.indexOf(asyncLocalStorageMarker);
    if (markerIndex === -1) {
        throw new Error("Expected a single %async-local-storage marker in server middleware.");
    }
    const lineStart = code.lastIndexOf("\n", markerIndex) + 1;
    const indent = code.slice(lineStart, markerIndex).replace(/\t/g, "  ");
    const innerIndent = `${indent}  `;
    const asyncLocalStorageBlock = args.compilerOptions.disableAsyncLocalStorage
        ? [
            `${indent}if (!runtime.serverAsyncLocalStorage) {`,
            `${innerIndent}runtime.overwriteServerAsyncLocalStorage(createMockAsyncLocalStorage());`,
            `${indent}}`,
        ]
        : [
            `${indent}if (!runtime.disableAsyncLocalStorage && !runtime.serverAsyncLocalStorage) {`,
            `${innerIndent}const { AsyncLocalStorage } = await import("async_hooks");`,
            `${innerIndent}runtime.overwriteServerAsyncLocalStorage(new AsyncLocalStorage());`,
            `${indent}} else if (!runtime.serverAsyncLocalStorage) {`,
            `${innerIndent}runtime.overwriteServerAsyncLocalStorage(createMockAsyncLocalStorage());`,
            `${indent}}`,
        ];
    const lineEnd = code.indexOf("\n", markerIndex);
    const endOfMarkerLine = lineEnd === -1 ? code.length : lineEnd + 1;
    code =
        code.slice(0, lineStart) +
            `${asyncLocalStorageBlock.join("\n")}\n` +
            code.slice(endOfMarkerLine);
    if (args.compilerOptions.experimentalMiddlewareLocaleSplitting) {
        code = code.replace("const compiledBundles = {};", `const compiledBundles = ${JSON.stringify(createCompiledMessagesObject(args.compiledBundles))};`);
    }
    return code.replace(/\t/g, "  ");
}
function createCompiledMessagesObject(compiledBundles) {
    const result = {};
    for (const compiledBundle of compiledBundles) {
        const bundleId = compiledBundle.bundle.node.id;
        const safeModuleId = toSafeModuleId(bundleId);
        if (result[bundleId] === undefined) {
            result[bundleId] = {};
        }
        for (const [locale, compiledMessage] of Object.entries(compiledBundle.messages)) {
            result[bundleId][locale] = compiledMessage.code
                .replace(`export const ${safeModuleId} = `, "")
                .replace(/;$/, "");
        }
    }
    return result;
}
/**
 * Load a file from the current directory.
 *
 * Prunes the imports on top of the file as the runtime is
 * self-contained.
 *
 * @param {string} path
 * @returns {string}
 */
function injectCode(path) {
    const code = fs.readFileSync(new URL(path, import.meta.url), "utf-8");
    // Regex to match single-line and multi-line imports
    const importRegex = /import\s+[\s\S]*?from\s+['"][^'"]+['"]\s*;?/g;
    const sourceMapRegex = /\/\/# sourceMappingURL=.*$/gm;
    const blockSourceMapRegex = /\/\*# sourceMappingURL=.*?\*\//g;
    return code
        .replace(importRegex, "")
        .replace(sourceMapRegex, "")
        .replace(blockSourceMapRegex, "")
        .trim();
}
//# sourceMappingURL=create-server-file.js.map