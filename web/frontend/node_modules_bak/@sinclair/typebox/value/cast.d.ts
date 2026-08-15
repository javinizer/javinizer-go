import * as Types from '../typebox';
export declare class ValueCastArrayUniqueItemsTypeError extends Types.TypeBoxError {
    readonly schema: Types.TSchema;
    readonly value: unknown;
    constructor(schema: Types.TSchema, value: unknown);
}
export declare class ValueCastNeverTypeError extends Types.TypeBoxError {
    readonly schema: Types.TSchema;
    constructor(schema: Types.TSchema);
}
export declare class ValueCastRecursiveTypeError extends Types.TypeBoxError {
    readonly schema: Types.TSchema;
    constructor(schema: Types.TSchema);
}
export declare class ValueCastUnknownTypeError extends Types.TypeBoxError {
    readonly schema: Types.TSchema;
    constructor(schema: Types.TSchema);
}
export declare function DefaultClone(schema: Types.TSchema, references: Types.TSchema[], value: any): any;
export declare function Default(schema: Types.TSchema, references: Types.TSchema[], value: any): any;
/** Casts a value into a given type and references. The return value will retain as much information of the original value as possible. */
export declare function Cast<T extends Types.TSchema>(schema: T, references: Types.TSchema[], value: unknown): Types.Static<T>;
/** Casts a value into a given type. The return value will retain as much information of the original value as possible. */
export declare function Cast<T extends Types.TSchema>(schema: T, value: unknown): Types.Static<T>;
