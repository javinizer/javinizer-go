import type { Logger } from "../../services/logger/index.js";
import type { CliStep } from "../utils.js";
export declare const maybeAddMachineTranslation: CliStep<{
    fs: typeof import("node:fs/promises");
    logger: Logger;
    projectPath: string;
    root: string;
    packageJsonPath: string;
}, {
    fs: typeof import("node:fs/promises");
    logger: Logger;
    projectPath: string;
    root: string;
    packageJsonPath: string;
}>;
//# sourceMappingURL=maybe-add-machine-translation.d.ts.map