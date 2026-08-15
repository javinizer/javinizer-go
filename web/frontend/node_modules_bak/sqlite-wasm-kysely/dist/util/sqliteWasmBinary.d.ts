/**
 * Resolving the sqlite wasm dynamically recurringly breaks
 * in different environments (node, browser, vitest).
 *
 * To ease development, the wasm binary is bundled (for now).
 *
 * You can bundle the wasm binary by:
 *
 * 1. Convert the wasm binary with https://base64.guru/converter/encode/file to base64
 * 2. Copy the base64 string to the `sqliteWasmBinary.ts` variable
 *
 * The binary is located in `node_modules/@sqlite.org/sqlite-wasm/sqlite-wasm/jswasm/sqlite3.wasm`
 */
export declare const wasmBinary: ArrayBufferLike;
