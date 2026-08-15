import type { Account } from "@inlang/sdk/lix";
export declare function getLocalAccount(args: {
    fs: typeof import("node:fs");
}): Account | undefined;
export declare function saveLocalAccount(args: {
    fs: typeof import("node:fs");
    account: Account;
}): void;
/**
 * Returns the path to the local account file.
 *
 * Based on the env-paths package https://github.com/sindresorhus/env-paths/blob/main/index.js
 */
export declare function getAccountFilePath(): string;
//# sourceMappingURL=index.d.ts.map