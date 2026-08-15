import * as Types from '../typebox';
export declare class ValueCreateUnknownTypeError extends Types.TypeBoxError {
    readonly schema: Types.TSchema;
    constructor(schema: Types.TSchema);
}
export declare class ValueCreateNeverTypeError extends Types.TypeBoxError {
    readonly schema: Types.TSchema;
    constructor(schema: Types.TSchema);
}
export declare class ValueCreateNotTypeError extends Types.TypeBoxError {
    readonly schema: Types.TSchema;
    constructor(schema: Types.TSchema);
}
export declare class ValueCreateIntersectTypeError extends Types.TypeBoxError {
    readonly schema: Types.TSchema;
    constructor(schema: Types.TSchema);
}
export declare class ValueCreateTempateLiteralTypeError extends Types.TypeBoxError {
    readonly schema: Types.TSchema;
    constructor(schema: Types.TSchema);
}
export declare class ValueCreateRecursiveInstantiationError extends Types.TypeBoxError {
    readonly schema: Types.TSchema;
    readonly recursiveMaxDepth: number;
    constructor(schema: Types.TSchema, recursiveMaxDepth: number);
}
/** Creates a value from the given schema and references */
export declare function Create<T extends Types.TSchema>(schema: T, references: Types.TSchema[]): Types.Static<T>;
/** Creates a value from the given schema */
export declare function Create<T extends Types.TSchema>(schema: T): Types.Static<T>;
