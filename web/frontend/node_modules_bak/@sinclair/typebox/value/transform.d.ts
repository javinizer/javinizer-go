import { ValueError } from '../errors/errors';
import * as Types from '../typebox';
export declare class TransformDecodeCheckError extends Types.TypeBoxError {
    readonly schema: Types.TSchema;
    readonly value: unknown;
    readonly error: ValueError;
    constructor(schema: Types.TSchema, value: unknown, error: ValueError);
}
export declare class TransformEncodeCheckError extends Types.TypeBoxError {
    readonly schema: Types.TSchema;
    readonly value: unknown;
    readonly error: ValueError;
    constructor(schema: Types.TSchema, value: unknown, error: ValueError);
}
export declare class TransformDecodeError extends Types.TypeBoxError {
    readonly schema: Types.TSchema;
    readonly value: unknown;
    constructor(schema: Types.TSchema, value: unknown, error: any);
}
export declare class TransformEncodeError extends Types.TypeBoxError {
    readonly schema: Types.TSchema;
    readonly value: unknown;
    constructor(schema: Types.TSchema, value: unknown, error: any);
}
/** Recursively checks a schema for transform codecs */
export declare namespace HasTransform {
    /** Returns true if this schema contains a transform codec */
    function Has(schema: Types.TSchema, references: Types.TSchema[]): boolean;
}
/** Decodes a value using transform decoders if available. Does not ensure correct results. */
export declare namespace DecodeTransform {
    function Decode(schema: Types.TSchema, references: Types.TSchema[], value: unknown): unknown;
}
/** Encodes a value using transform encoders if available. Does not ensure correct results. */
export declare namespace EncodeTransform {
    function Encode(schema: Types.TSchema, references: Types.TSchema[], value: unknown): unknown;
}
