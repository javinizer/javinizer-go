/**
 * Turns an unsafe module id like `helloWorld🍌` into a safe one like `helloworld__`.
 *
 * Mainly exists to support https://github.com/opral/inlang-paraglide-js/issues/285
 */
export declare function toSafeModuleId(id: string): string;
export declare function isSafeModuleId(id: string): boolean;
//# sourceMappingURL=safe-module-id.d.ts.map