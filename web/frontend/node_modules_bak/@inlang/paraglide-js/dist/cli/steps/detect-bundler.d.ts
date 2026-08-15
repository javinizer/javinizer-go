import type { CliStep } from "../utils.js";
export declare const detectBundler: CliStep<{
    fs: typeof import("node:fs/promises");
}, {
    bundler?: undefined;
    configPath?: undefined;
} | {
    bundler: "vite";
    configPath: string;
}>;
//# sourceMappingURL=detect-bundler.d.ts.map