import * as Types from '../typebox';
export declare class ValueCheckUnknownTypeError extends Types.TypeBoxError {
    readonly schema: Types.TSchema;
    constructor(schema: Types.TSchema);
}
/** Returns true if the value matches the given type. */
export declare function Check<T extends Types.TSchema>(schema: T, references: Types.TSchema[], value: unknown): value is Types.Static<T>;
/** Returns true if the value matches the given type. */
export declare function Check<T extends Types.TSchema>(schema: T, value: unknown): value is Types.Static<T>;
