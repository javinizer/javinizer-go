import type { Logger } from "../../services/logger/index.js";
import type { CliStep } from "../utils.js";
import type { InlangProject } from "@inlang/sdk";
export declare const maybeAddSherlock: CliStep<{
    fs: typeof import("node:fs/promises");
    logger: Logger;
    project: InlangProject;
}, unknown>;
//# sourceMappingURL=maybe-add-sherlock.d.ts.map