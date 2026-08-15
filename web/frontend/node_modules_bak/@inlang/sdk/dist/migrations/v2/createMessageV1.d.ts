import type { PatternV1 } from "../../json-schema/old-v1-message/schemaV1.js";
/**
 * @deprecated don't use the messageV1 type anymore.
 *
 * This function exists to ease migrations of v1 plugins to v2 plugins.
 * The tests can call `createMessageV1` to create a message object that satisfies the v1 schema.
 *
 * @example
 *   - createMessage("id", { "en": "Hello" })
 *   + createMessageV1("id", { "en": "Hello" })
 */
export declare const createMessageV1: (id: string, patterns: Record<string, PatternV1 | string>) => {
    id: string;
    alias: {};
    selectors: never[];
    variants: {
        languageTag: string;
        match: never[];
        pattern: ({
            type: "Text";
            value: string;
        } | {
            type: "VariableReference";
            name: string;
        })[];
    }[];
};
//# sourceMappingURL=createMessageV1.d.ts.map