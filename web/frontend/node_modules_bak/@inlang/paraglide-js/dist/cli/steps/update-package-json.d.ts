import type { Logger } from "../../services/logger/index.js";
import type { CliStep } from "../utils.js";
export declare function updatePackageJson(opt: {
    dependencies?: (deps: Record<string, string>) => Promise<Record<string, string>>;
    devDependencies?: (devDeps: Record<string, string>) => Promise<Record<string, string>>;
    scripts?: (scripts: Record<string, string>) => Promise<Record<string, string>>;
}): CliStep<{
    packageJsonPath: string;
    fs: typeof import("node:fs/promises");
    logger: Logger;
}, unknown>;
//# sourceMappingURL=update-package-json.d.ts.map