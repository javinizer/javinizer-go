import { type InlangProject } from "@inlang/sdk";
import type { Logger } from "../../services/logger/index.js";
import type { CliStep } from "../utils.js";
import fs from "node:fs";
export declare const initializeInlangProject: CliStep<{
    fs: typeof fs.promises;
    syncFs: typeof fs;
    logger: Logger;
    root: string;
}, {
    project: InlangProject;
    /** Relative path to the project */
    projectPath: string;
}>;
export declare const createNewProjectFlow: (ctx: {
    fs: typeof fs.promises;
    syncFs: typeof fs;
    logger: Logger;
}) => Promise<{
    project: InlangProject;
    /** An absolute path to the created project */
    projectPath: string;
}>;
/**
 * Get's the common prefix of a set of strings.
 * If only one string is passed the prefix will be the empty string
 *
 * @example
 * ```ts
 * getCommonPrefix(["foo", "foobar"]) // "foo"
 * getCommonPrefix(["foobar"]) // ""
 * ```
 */
export declare function getCommonPrefix(strings: string[]): string;
/**
 * Follows the IETF BCP 47 locale schema with modifications.
 */
export declare const pattern = "^((?<grandfathered>(en-GB-oed|i-ami|i-bnn|i-default|i-enochian|i-hak|i-klingon|i-lux|i-mingo|i-navajo|i-pwn|i-tao|i-tay|i-tsu|sgn-BE-FR|sgn-BE-NL|sgn-CH-DE)|(art-lojban|cel-gaulish|no-bok|no-nyn|zh-guoyu|zh-hakka|zh-min|zh-min-nan|zh-xiang))|((?<language>([A-Za-z]{2,3}(-(?<extlang>[A-Za-z]{3}(-[A-Za-z]{3}){0,2}))?))(-(?<script>[A-Za-z]{4}))?(-(?<region>[A-Za-z]{2}|[0-9]{3}))?(-(?<variant>[A-Za-z0-9]{5,8}|[0-9][A-Za-z0-9]{3}))*))$";
//# sourceMappingURL=initialize-inlang-project.d.ts.map