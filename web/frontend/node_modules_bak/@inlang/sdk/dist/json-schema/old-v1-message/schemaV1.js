import { Type } from "@sinclair/typebox";
export const TextV1 = Type.Object({
    type: Type.Literal("Text"),
    value: Type.String(),
});
export const VariableReferenceV1 = Type.Object({
    type: Type.Literal("VariableReference"),
    name: Type.String(),
});
export const ExpressionV1 = Type.Union([VariableReferenceV1]);
export const PatternV1 = Type.Array(Type.Union([TextV1, ExpressionV1]));
export const VariantV1 = Type.Object({
    languageTag: Type.String(),
    /**
     * The number of keys in each variant match MUST equal the number of expressions in the selectors.
     *
     * Inspired by: https://github.com/unicode-org/message-format-wg/blob/main/spec/formatting.md#pattern-selection
     */
    // a match can always only be string-based because a string is what is rendered to the UI
    match: Type.Array(Type.String()),
    pattern: PatternV1,
});
export const MessageV1 = Type.Object({
    id: Type.String(),
    alias: Type.Record(Type.String(), Type.String()),
    /**
     * The order in which the selectors are placed determines the precedence of patterns.
     */
    selectors: Type.Array(ExpressionV1),
    variants: Type.Array(VariantV1),
});
//# sourceMappingURL=schemaV1.js.map