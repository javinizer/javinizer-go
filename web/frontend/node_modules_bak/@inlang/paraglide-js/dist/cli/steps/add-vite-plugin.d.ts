import type { Logger } from "../../services/logger/index.js";
import type { CliStep } from "../utils.js";
export declare const addVitePlugin: CliStep<{
    fs: typeof import("node:fs/promises");
    projectPath: string;
    outdir: string;
    logger?: Logger;
    configPath: string;
}, unknown>;
//# sourceMappingURL=add-vite-plugin.d.ts.map