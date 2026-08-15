import type { ProjectSettings } from "../json-schema/settings.js";
/**
 * Creates a new inlang project.
 *
 * The app is responsible for saving the project "whereever"
 * e.g. the user's computer, cloud storage, or OPFS in the browser.
 */
export declare function newProject(args?: {
    settings?: ProjectSettings;
}): Promise<Blob>;
export declare const defaultProjectSettings: {
    $schema: "https://inlang.com/schema/project-settings";
    baseLocale: string;
    locales: string[];
    modules: never[];
};
//# sourceMappingURL=newProject.d.ts.map