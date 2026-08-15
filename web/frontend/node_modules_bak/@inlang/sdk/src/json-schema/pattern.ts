import { Type, type Static } from "@sinclair/typebox";

export type VariableReference = Static<typeof VariableReference>;
export const VariableReference = Type.Object({
	type: Type.Literal("variable-reference"),
	name: Type.String(),
});

export type Literal = Static<typeof Literal>;
export const Literal = Type.Object({
	type: Type.Literal("literal"),
	value: Type.String(),
});

export type Option = Static<typeof Option>;
export const Option = Type.Object({
	name: Type.String(),
	value: Type.Union([Literal, VariableReference]),
});

export type Attribute = Static<typeof Attribute>;
export const Attribute = Type.Object({
	name: Type.String(),
	value: Type.Union([Literal, Type.Literal(true)]),
});

export type FunctionReference = Static<typeof FunctionReference>;
export const FunctionReference = Type.Object({
	type: Type.Literal("function-reference"),
	name: Type.String(),
	options: Type.Array(Option),
});

export type Expression = Static<typeof Expression>;
export const Expression = Type.Object({
	type: Type.Literal("expression"),
	arg: Type.Union([VariableReference, Literal]),
	annotation: Type.Optional(FunctionReference),
});

export type Text = Static<typeof Text>;
export const Text = Type.Object({
	type: Type.Literal("text"),
	value: Type.String(),
});

export type MarkupStart = Static<typeof MarkupStart>;
export const MarkupStart = Type.Object({
	type: Type.Literal("markup-start"),
	name: Type.String(),
	options: Type.Optional(Type.Array(Option)),
	attributes: Type.Optional(Type.Array(Attribute)),
});

export type MarkupEnd = Static<typeof MarkupEnd>;
export const MarkupEnd = Type.Object({
	type: Type.Literal("markup-end"),
	name: Type.String(),
	options: Type.Optional(Type.Array(Option)),
	attributes: Type.Optional(Type.Array(Attribute)),
});

export type MarkupStandalone = Static<typeof MarkupStandalone>;
export const MarkupStandalone = Type.Object({
	type: Type.Literal("markup-standalone"),
	name: Type.String(),
	options: Type.Optional(Type.Array(Option)),
	attributes: Type.Optional(Type.Array(Attribute)),
});

export type LocalVariable = Static<typeof LocalVariable>;
export const LocalVariable = Type.Object({
	type: Type.Literal("local-variable"),
	name: Type.String(),
	value: Expression,
});

export type InputVariable = Static<typeof InputVariable>;
export const InputVariable = Type.Object({
	type: Type.Literal("input-variable"),
	name: Type.String(),
	annotation: Type.Optional(FunctionReference),
});

export type Declaration = Static<typeof Declaration>;
export const Declaration = Type.Union([InputVariable, LocalVariable]);

export type Pattern = Static<typeof Pattern>;
export const Pattern = Type.Array(
	Type.Union([Text, Expression, MarkupStart, MarkupEnd, MarkupStandalone])
);
