import { type Static } from "@sinclair/typebox";
export type VariableReference = Static<typeof VariableReference>;
export declare const VariableReference: import("@sinclair/typebox").TObject<{
    type: import("@sinclair/typebox").TLiteral<"variable-reference">;
    name: import("@sinclair/typebox").TString;
}>;
export type Literal = Static<typeof Literal>;
export declare const Literal: import("@sinclair/typebox").TObject<{
    type: import("@sinclair/typebox").TLiteral<"literal">;
    value: import("@sinclair/typebox").TString;
}>;
export type Option = Static<typeof Option>;
export declare const Option: import("@sinclair/typebox").TObject<{
    name: import("@sinclair/typebox").TString;
    value: import("@sinclair/typebox").TUnion<[import("@sinclair/typebox").TObject<{
        type: import("@sinclair/typebox").TLiteral<"literal">;
        value: import("@sinclair/typebox").TString;
    }>, import("@sinclair/typebox").TObject<{
        type: import("@sinclair/typebox").TLiteral<"variable-reference">;
        name: import("@sinclair/typebox").TString;
    }>]>;
}>;
export type Attribute = Static<typeof Attribute>;
export declare const Attribute: import("@sinclair/typebox").TObject<{
    name: import("@sinclair/typebox").TString;
    value: import("@sinclair/typebox").TUnion<[import("@sinclair/typebox").TObject<{
        type: import("@sinclair/typebox").TLiteral<"literal">;
        value: import("@sinclair/typebox").TString;
    }>, import("@sinclair/typebox").TLiteral<true>]>;
}>;
export type FunctionReference = Static<typeof FunctionReference>;
export declare const FunctionReference: import("@sinclair/typebox").TObject<{
    type: import("@sinclair/typebox").TLiteral<"function-reference">;
    name: import("@sinclair/typebox").TString;
    options: import("@sinclair/typebox").TArray<import("@sinclair/typebox").TObject<{
        name: import("@sinclair/typebox").TString;
        value: import("@sinclair/typebox").TUnion<[import("@sinclair/typebox").TObject<{
            type: import("@sinclair/typebox").TLiteral<"literal">;
            value: import("@sinclair/typebox").TString;
        }>, import("@sinclair/typebox").TObject<{
            type: import("@sinclair/typebox").TLiteral<"variable-reference">;
            name: import("@sinclair/typebox").TString;
        }>]>;
    }>>;
}>;
export type Expression = Static<typeof Expression>;
export declare const Expression: import("@sinclair/typebox").TObject<{
    type: import("@sinclair/typebox").TLiteral<"expression">;
    arg: import("@sinclair/typebox").TUnion<[import("@sinclair/typebox").TObject<{
        type: import("@sinclair/typebox").TLiteral<"variable-reference">;
        name: import("@sinclair/typebox").TString;
    }>, import("@sinclair/typebox").TObject<{
        type: import("@sinclair/typebox").TLiteral<"literal">;
        value: import("@sinclair/typebox").TString;
    }>]>;
    annotation: import("@sinclair/typebox").TOptional<import("@sinclair/typebox").TObject<{
        type: import("@sinclair/typebox").TLiteral<"function-reference">;
        name: import("@sinclair/typebox").TString;
        options: import("@sinclair/typebox").TArray<import("@sinclair/typebox").TObject<{
            name: import("@sinclair/typebox").TString;
            value: import("@sinclair/typebox").TUnion<[import("@sinclair/typebox").TObject<{
                type: import("@sinclair/typebox").TLiteral<"literal">;
                value: import("@sinclair/typebox").TString;
            }>, import("@sinclair/typebox").TObject<{
                type: import("@sinclair/typebox").TLiteral<"variable-reference">;
                name: import("@sinclair/typebox").TString;
            }>]>;
        }>>;
    }>>;
}>;
export type Text = Static<typeof Text>;
export declare const Text: import("@sinclair/typebox").TObject<{
    type: import("@sinclair/typebox").TLiteral<"text">;
    value: import("@sinclair/typebox").TString;
}>;
export type MarkupStart = Static<typeof MarkupStart>;
export declare const MarkupStart: import("@sinclair/typebox").TObject<{
    type: import("@sinclair/typebox").TLiteral<"markup-start">;
    name: import("@sinclair/typebox").TString;
    options: import("@sinclair/typebox").TOptional<import("@sinclair/typebox").TArray<import("@sinclair/typebox").TObject<{
        name: import("@sinclair/typebox").TString;
        value: import("@sinclair/typebox").TUnion<[import("@sinclair/typebox").TObject<{
            type: import("@sinclair/typebox").TLiteral<"literal">;
            value: import("@sinclair/typebox").TString;
        }>, import("@sinclair/typebox").TObject<{
            type: import("@sinclair/typebox").TLiteral<"variable-reference">;
            name: import("@sinclair/typebox").TString;
        }>]>;
    }>>>;
    attributes: import("@sinclair/typebox").TOptional<import("@sinclair/typebox").TArray<import("@sinclair/typebox").TObject<{
        name: import("@sinclair/typebox").TString;
        value: import("@sinclair/typebox").TUnion<[import("@sinclair/typebox").TObject<{
            type: import("@sinclair/typebox").TLiteral<"literal">;
            value: import("@sinclair/typebox").TString;
        }>, import("@sinclair/typebox").TLiteral<true>]>;
    }>>>;
}>;
export type MarkupEnd = Static<typeof MarkupEnd>;
export declare const MarkupEnd: import("@sinclair/typebox").TObject<{
    type: import("@sinclair/typebox").TLiteral<"markup-end">;
    name: import("@sinclair/typebox").TString;
    options: import("@sinclair/typebox").TOptional<import("@sinclair/typebox").TArray<import("@sinclair/typebox").TObject<{
        name: import("@sinclair/typebox").TString;
        value: import("@sinclair/typebox").TUnion<[import("@sinclair/typebox").TObject<{
            type: import("@sinclair/typebox").TLiteral<"literal">;
            value: import("@sinclair/typebox").TString;
        }>, import("@sinclair/typebox").TObject<{
            type: import("@sinclair/typebox").TLiteral<"variable-reference">;
            name: import("@sinclair/typebox").TString;
        }>]>;
    }>>>;
    attributes: import("@sinclair/typebox").TOptional<import("@sinclair/typebox").TArray<import("@sinclair/typebox").TObject<{
        name: import("@sinclair/typebox").TString;
        value: import("@sinclair/typebox").TUnion<[import("@sinclair/typebox").TObject<{
            type: import("@sinclair/typebox").TLiteral<"literal">;
            value: import("@sinclair/typebox").TString;
        }>, import("@sinclair/typebox").TLiteral<true>]>;
    }>>>;
}>;
export type MarkupStandalone = Static<typeof MarkupStandalone>;
export declare const MarkupStandalone: import("@sinclair/typebox").TObject<{
    type: import("@sinclair/typebox").TLiteral<"markup-standalone">;
    name: import("@sinclair/typebox").TString;
    options: import("@sinclair/typebox").TOptional<import("@sinclair/typebox").TArray<import("@sinclair/typebox").TObject<{
        name: import("@sinclair/typebox").TString;
        value: import("@sinclair/typebox").TUnion<[import("@sinclair/typebox").TObject<{
            type: import("@sinclair/typebox").TLiteral<"literal">;
            value: import("@sinclair/typebox").TString;
        }>, import("@sinclair/typebox").TObject<{
            type: import("@sinclair/typebox").TLiteral<"variable-reference">;
            name: import("@sinclair/typebox").TString;
        }>]>;
    }>>>;
    attributes: import("@sinclair/typebox").TOptional<import("@sinclair/typebox").TArray<import("@sinclair/typebox").TObject<{
        name: import("@sinclair/typebox").TString;
        value: import("@sinclair/typebox").TUnion<[import("@sinclair/typebox").TObject<{
            type: import("@sinclair/typebox").TLiteral<"literal">;
            value: import("@sinclair/typebox").TString;
        }>, import("@sinclair/typebox").TLiteral<true>]>;
    }>>>;
}>;
export type LocalVariable = Static<typeof LocalVariable>;
export declare const LocalVariable: import("@sinclair/typebox").TObject<{
    type: import("@sinclair/typebox").TLiteral<"local-variable">;
    name: import("@sinclair/typebox").TString;
    value: import("@sinclair/typebox").TObject<{
        type: import("@sinclair/typebox").TLiteral<"expression">;
        arg: import("@sinclair/typebox").TUnion<[import("@sinclair/typebox").TObject<{
            type: import("@sinclair/typebox").TLiteral<"variable-reference">;
            name: import("@sinclair/typebox").TString;
        }>, import("@sinclair/typebox").TObject<{
            type: import("@sinclair/typebox").TLiteral<"literal">;
            value: import("@sinclair/typebox").TString;
        }>]>;
        annotation: import("@sinclair/typebox").TOptional<import("@sinclair/typebox").TObject<{
            type: import("@sinclair/typebox").TLiteral<"function-reference">;
            name: import("@sinclair/typebox").TString;
            options: import("@sinclair/typebox").TArray<import("@sinclair/typebox").TObject<{
                name: import("@sinclair/typebox").TString;
                value: import("@sinclair/typebox").TUnion<[import("@sinclair/typebox").TObject<{
                    type: import("@sinclair/typebox").TLiteral<"literal">;
                    value: import("@sinclair/typebox").TString;
                }>, import("@sinclair/typebox").TObject<{
                    type: import("@sinclair/typebox").TLiteral<"variable-reference">;
                    name: import("@sinclair/typebox").TString;
                }>]>;
            }>>;
        }>>;
    }>;
}>;
export type InputVariable = Static<typeof InputVariable>;
export declare const InputVariable: import("@sinclair/typebox").TObject<{
    type: import("@sinclair/typebox").TLiteral<"input-variable">;
    name: import("@sinclair/typebox").TString;
    annotation: import("@sinclair/typebox").TOptional<import("@sinclair/typebox").TObject<{
        type: import("@sinclair/typebox").TLiteral<"function-reference">;
        name: import("@sinclair/typebox").TString;
        options: import("@sinclair/typebox").TArray<import("@sinclair/typebox").TObject<{
            name: import("@sinclair/typebox").TString;
            value: import("@sinclair/typebox").TUnion<[import("@sinclair/typebox").TObject<{
                type: import("@sinclair/typebox").TLiteral<"literal">;
                value: import("@sinclair/typebox").TString;
            }>, import("@sinclair/typebox").TObject<{
                type: import("@sinclair/typebox").TLiteral<"variable-reference">;
                name: import("@sinclair/typebox").TString;
            }>]>;
        }>>;
    }>>;
}>;
export type Declaration = Static<typeof Declaration>;
export declare const Declaration: import("@sinclair/typebox").TUnion<[import("@sinclair/typebox").TObject<{
    type: import("@sinclair/typebox").TLiteral<"input-variable">;
    name: import("@sinclair/typebox").TString;
    annotation: import("@sinclair/typebox").TOptional<import("@sinclair/typebox").TObject<{
        type: import("@sinclair/typebox").TLiteral<"function-reference">;
        name: import("@sinclair/typebox").TString;
        options: import("@sinclair/typebox").TArray<import("@sinclair/typebox").TObject<{
            name: import("@sinclair/typebox").TString;
            value: import("@sinclair/typebox").TUnion<[import("@sinclair/typebox").TObject<{
                type: import("@sinclair/typebox").TLiteral<"literal">;
                value: import("@sinclair/typebox").TString;
            }>, import("@sinclair/typebox").TObject<{
                type: import("@sinclair/typebox").TLiteral<"variable-reference">;
                name: import("@sinclair/typebox").TString;
            }>]>;
        }>>;
    }>>;
}>, import("@sinclair/typebox").TObject<{
    type: import("@sinclair/typebox").TLiteral<"local-variable">;
    name: import("@sinclair/typebox").TString;
    value: import("@sinclair/typebox").TObject<{
        type: import("@sinclair/typebox").TLiteral<"expression">;
        arg: import("@sinclair/typebox").TUnion<[import("@sinclair/typebox").TObject<{
            type: import("@sinclair/typebox").TLiteral<"variable-reference">;
            name: import("@sinclair/typebox").TString;
        }>, import("@sinclair/typebox").TObject<{
            type: import("@sinclair/typebox").TLiteral<"literal">;
            value: import("@sinclair/typebox").TString;
        }>]>;
        annotation: import("@sinclair/typebox").TOptional<import("@sinclair/typebox").TObject<{
            type: import("@sinclair/typebox").TLiteral<"function-reference">;
            name: import("@sinclair/typebox").TString;
            options: import("@sinclair/typebox").TArray<import("@sinclair/typebox").TObject<{
                name: import("@sinclair/typebox").TString;
                value: import("@sinclair/typebox").TUnion<[import("@sinclair/typebox").TObject<{
                    type: import("@sinclair/typebox").TLiteral<"literal">;
                    value: import("@sinclair/typebox").TString;
                }>, import("@sinclair/typebox").TObject<{
                    type: import("@sinclair/typebox").TLiteral<"variable-reference">;
                    name: import("@sinclair/typebox").TString;
                }>]>;
            }>>;
        }>>;
    }>;
}>]>;
export type Pattern = Static<typeof Pattern>;
export declare const Pattern: import("@sinclair/typebox").TArray<import("@sinclair/typebox").TUnion<[import("@sinclair/typebox").TObject<{
    type: import("@sinclair/typebox").TLiteral<"text">;
    value: import("@sinclair/typebox").TString;
}>, import("@sinclair/typebox").TObject<{
    type: import("@sinclair/typebox").TLiteral<"expression">;
    arg: import("@sinclair/typebox").TUnion<[import("@sinclair/typebox").TObject<{
        type: import("@sinclair/typebox").TLiteral<"variable-reference">;
        name: import("@sinclair/typebox").TString;
    }>, import("@sinclair/typebox").TObject<{
        type: import("@sinclair/typebox").TLiteral<"literal">;
        value: import("@sinclair/typebox").TString;
    }>]>;
    annotation: import("@sinclair/typebox").TOptional<import("@sinclair/typebox").TObject<{
        type: import("@sinclair/typebox").TLiteral<"function-reference">;
        name: import("@sinclair/typebox").TString;
        options: import("@sinclair/typebox").TArray<import("@sinclair/typebox").TObject<{
            name: import("@sinclair/typebox").TString;
            value: import("@sinclair/typebox").TUnion<[import("@sinclair/typebox").TObject<{
                type: import("@sinclair/typebox").TLiteral<"literal">;
                value: import("@sinclair/typebox").TString;
            }>, import("@sinclair/typebox").TObject<{
                type: import("@sinclair/typebox").TLiteral<"variable-reference">;
                name: import("@sinclair/typebox").TString;
            }>]>;
        }>>;
    }>>;
}>, import("@sinclair/typebox").TObject<{
    type: import("@sinclair/typebox").TLiteral<"markup-start">;
    name: import("@sinclair/typebox").TString;
    options: import("@sinclair/typebox").TOptional<import("@sinclair/typebox").TArray<import("@sinclair/typebox").TObject<{
        name: import("@sinclair/typebox").TString;
        value: import("@sinclair/typebox").TUnion<[import("@sinclair/typebox").TObject<{
            type: import("@sinclair/typebox").TLiteral<"literal">;
            value: import("@sinclair/typebox").TString;
        }>, import("@sinclair/typebox").TObject<{
            type: import("@sinclair/typebox").TLiteral<"variable-reference">;
            name: import("@sinclair/typebox").TString;
        }>]>;
    }>>>;
    attributes: import("@sinclair/typebox").TOptional<import("@sinclair/typebox").TArray<import("@sinclair/typebox").TObject<{
        name: import("@sinclair/typebox").TString;
        value: import("@sinclair/typebox").TUnion<[import("@sinclair/typebox").TObject<{
            type: import("@sinclair/typebox").TLiteral<"literal">;
            value: import("@sinclair/typebox").TString;
        }>, import("@sinclair/typebox").TLiteral<true>]>;
    }>>>;
}>, import("@sinclair/typebox").TObject<{
    type: import("@sinclair/typebox").TLiteral<"markup-end">;
    name: import("@sinclair/typebox").TString;
    options: import("@sinclair/typebox").TOptional<import("@sinclair/typebox").TArray<import("@sinclair/typebox").TObject<{
        name: import("@sinclair/typebox").TString;
        value: import("@sinclair/typebox").TUnion<[import("@sinclair/typebox").TObject<{
            type: import("@sinclair/typebox").TLiteral<"literal">;
            value: import("@sinclair/typebox").TString;
        }>, import("@sinclair/typebox").TObject<{
            type: import("@sinclair/typebox").TLiteral<"variable-reference">;
            name: import("@sinclair/typebox").TString;
        }>]>;
    }>>>;
    attributes: import("@sinclair/typebox").TOptional<import("@sinclair/typebox").TArray<import("@sinclair/typebox").TObject<{
        name: import("@sinclair/typebox").TString;
        value: import("@sinclair/typebox").TUnion<[import("@sinclair/typebox").TObject<{
            type: import("@sinclair/typebox").TLiteral<"literal">;
            value: import("@sinclair/typebox").TString;
        }>, import("@sinclair/typebox").TLiteral<true>]>;
    }>>>;
}>, import("@sinclair/typebox").TObject<{
    type: import("@sinclair/typebox").TLiteral<"markup-standalone">;
    name: import("@sinclair/typebox").TString;
    options: import("@sinclair/typebox").TOptional<import("@sinclair/typebox").TArray<import("@sinclair/typebox").TObject<{
        name: import("@sinclair/typebox").TString;
        value: import("@sinclair/typebox").TUnion<[import("@sinclair/typebox").TObject<{
            type: import("@sinclair/typebox").TLiteral<"literal">;
            value: import("@sinclair/typebox").TString;
        }>, import("@sinclair/typebox").TObject<{
            type: import("@sinclair/typebox").TLiteral<"variable-reference">;
            name: import("@sinclair/typebox").TString;
        }>]>;
    }>>>;
    attributes: import("@sinclair/typebox").TOptional<import("@sinclair/typebox").TArray<import("@sinclair/typebox").TObject<{
        name: import("@sinclair/typebox").TString;
        value: import("@sinclair/typebox").TUnion<[import("@sinclair/typebox").TObject<{
            type: import("@sinclair/typebox").TLiteral<"literal">;
            value: import("@sinclair/typebox").TString;
        }>, import("@sinclair/typebox").TLiteral<true>]>;
    }>>>;
}>]>>;
//# sourceMappingURL=pattern.d.ts.map