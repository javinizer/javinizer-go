/**
 * @returns A new copy of the default project template that is safe to mutate.
 */
export declare function getNewProjectTemplate(): {
    $schema: "https://inlang.com/schema/project-settings";
    baseLocale: string;
    locales: string[];
    modules: string[];
    "plugin.inlang.messageFormat": {
        pathPattern: string;
    };
};
export declare const DEFAULT_PROJECT_PATH = "./project.inlang";
export declare const DEFAULT_OUTDIR = "./src/paraglide";
//# sourceMappingURL=defaults.d.ts.map