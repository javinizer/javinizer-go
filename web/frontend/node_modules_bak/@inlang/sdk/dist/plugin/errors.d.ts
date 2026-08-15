import type { ValueError } from "@sinclair/typebox/errors";
export declare class PluginError extends Error {
    readonly plugin: string;
    constructor(message: string, options: {
        plugin: string;
        cause?: Error;
    });
}
/**
 * Error when a plugin cannot be imported.
 */
export declare class PluginImportError extends PluginError {
    constructor(options: {
        plugin: string;
        cause: Error;
    });
}
export declare class PluginSettingsAreInvalidError extends PluginError {
    constructor(options: {
        plugin: string;
        errors: ValueError[];
    });
}
/**
 * Error when a plugin does not implement a required function
 */
export declare class PluginDoesNotImplementFunctionError extends PluginError {
    constructor(options: {
        plugin: string;
        function: string;
    });
}
/**
 * Error when a plugin was expected to exist, but doesn't
 */
export declare class PluginMissingError extends PluginError {
    constructor(options: {
        plugin: string;
    });
}
//# sourceMappingURL=errors.d.ts.map