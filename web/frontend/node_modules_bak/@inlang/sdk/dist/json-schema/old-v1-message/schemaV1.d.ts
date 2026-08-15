import { type Static } from "@sinclair/typebox";
/**
 * A (text) element that is translatable and rendered to the UI.
 */
export type TextV1 = Static<typeof TextV1>;
export declare const TextV1: import("@sinclair/typebox").TObject<{
    type: import("@sinclair/typebox").TLiteral<"Text">;
    value: import("@sinclair/typebox").TString;
}>;
export type VariableReferenceV1 = Static<typeof VariableReferenceV1>;
export declare const VariableReferenceV1: import("@sinclair/typebox").TObject<{
    type: import("@sinclair/typebox").TLiteral<"VariableReference">;
    name: import("@sinclair/typebox").TString;
}>;
/**
 * An expression is a reference to a variable or a function.
 *
 * Think of expressions as elements that are rendered to a
 * text value during runtime.
 */
export type ExpressionV1 = Static<typeof ExpressionV1>;
export declare const ExpressionV1: import("@sinclair/typebox").TObject<{
    type: import("@sinclair/typebox").TLiteral<"VariableReference">;
    name: import("@sinclair/typebox").TString;
}>;
/**
 * A pattern is a sequence of elements that comprise
 * a message that is rendered to the UI.
 */
export type PatternV1 = Static<typeof PatternV1>;
export declare const PatternV1: import("@sinclair/typebox").TArray<import("@sinclair/typebox").TUnion<[import("@sinclair/typebox").TObject<{
    type: import("@sinclair/typebox").TLiteral<"Text">;
    value: import("@sinclair/typebox").TString;
}>, import("@sinclair/typebox").TObject<{
    type: import("@sinclair/typebox").TLiteral<"VariableReference">;
    name: import("@sinclair/typebox").TString;
}>]>>;
/**
 * A variant contains a pattern that is rendered to the UI.
 */
export type VariantV1 = Static<typeof VariantV1>;
export declare const VariantV1: import("@sinclair/typebox").TObject<{
    languageTag: import("@sinclair/typebox").TString;
    /**
     * The number of keys in each variant match MUST equal the number of expressions in the selectors.
     *
     * Inspired by: https://github.com/unicode-org/message-format-wg/blob/main/spec/formatting.md#pattern-selection
     */
    match: import("@sinclair/typebox").TArray<import("@sinclair/typebox").TString>;
    pattern: import("@sinclair/typebox").TArray<import("@sinclair/typebox").TUnion<[import("@sinclair/typebox").TObject<{
        type: import("@sinclair/typebox").TLiteral<"Text">;
        value: import("@sinclair/typebox").TString;
    }>, import("@sinclair/typebox").TObject<{
        type: import("@sinclair/typebox").TLiteral<"VariableReference">;
        name: import("@sinclair/typebox").TString;
    }>]>>;
}>;
export type MessageV1 = Static<typeof MessageV1>;
export declare const MessageV1: import("@sinclair/typebox").TObject<{
    id: import("@sinclair/typebox").TString;
    alias: import("@sinclair/typebox").TRecord<import("@sinclair/typebox").TString, import("@sinclair/typebox").TString>;
    /**
     * The order in which the selectors are placed determines the precedence of patterns.
     */
    selectors: import("@sinclair/typebox").TArray<import("@sinclair/typebox").TObject<{
        type: import("@sinclair/typebox").TLiteral<"VariableReference">;
        name: import("@sinclair/typebox").TString;
    }>>;
    variants: import("@sinclair/typebox").TArray<import("@sinclair/typebox").TObject<{
        languageTag: import("@sinclair/typebox").TString;
        /**
         * The number of keys in each variant match MUST equal the number of expressions in the selectors.
         *
         * Inspired by: https://github.com/unicode-org/message-format-wg/blob/main/spec/formatting.md#pattern-selection
         */
        match: import("@sinclair/typebox").TArray<import("@sinclair/typebox").TString>;
        pattern: import("@sinclair/typebox").TArray<import("@sinclair/typebox").TUnion<[import("@sinclair/typebox").TObject<{
            type: import("@sinclair/typebox").TLiteral<"Text">;
            value: import("@sinclair/typebox").TString;
        }>, import("@sinclair/typebox").TObject<{
            type: import("@sinclair/typebox").TLiteral<"VariableReference">;
            name: import("@sinclair/typebox").TString;
        }>]>>;
    }>>;
}>;
//# sourceMappingURL=schemaV1.d.ts.map