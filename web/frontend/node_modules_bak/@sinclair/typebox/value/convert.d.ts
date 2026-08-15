import * as Types from '../typebox';
export declare class ValueConvertUnknownTypeError extends Types.TypeBoxError {
    readonly schema: Types.TSchema;
    constructor(schema: Types.TSchema);
}
export declare function Default(value: any): any;
/** Converts any type mismatched values to their target type if a reasonable conversion is possible. */
export declare function Convert<T extends Types.TSchema>(schema: T, references: Types.TSchema[], value: unknown): unknown;
/** Converts any type mismatched values to their target type if a reasonable conversion is possible. */
export declare function Convert<T extends Types.TSchema>(schema: T, value: unknown): unknown;
