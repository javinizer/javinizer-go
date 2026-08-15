import { ValueErrorIterator } from '../errors/errors';
import * as Types from '../typebox';
export type CheckFunction = (value: unknown) => boolean;
export declare class TypeCheck<T extends Types.TSchema> {
    private readonly schema;
    private readonly references;
    private readonly checkFunc;
    private readonly code;
    private readonly hasTransform;
    constructor(schema: T, references: Types.TSchema[], checkFunc: CheckFunction, code: string);
    /** Returns the generated assertion code used to validate this type. */
    Code(): string;
    /** Returns an iterator for each error in this value. */
    Errors(value: unknown): ValueErrorIterator;
    /** Returns true if the value matches the compiled type. */
    Check(value: unknown): value is Types.Static<T>;
    /** Decodes a value or throws if error */
    Decode(value: unknown): Types.StaticDecode<T>;
    /** Encodes a value or throws if error */
    Encode(value: unknown): Types.StaticEncode<T>;
}
export declare class TypeCompilerUnknownTypeError extends Types.TypeBoxError {
    readonly schema: Types.TSchema;
    constructor(schema: Types.TSchema);
}
export declare class TypeCompilerTypeGuardError extends Types.TypeBoxError {
    readonly schema: Types.TSchema;
    constructor(schema: Types.TSchema);
}
export declare namespace Policy {
    function IsExactOptionalProperty(value: string, key: string, expression: string): string;
    function IsObjectLike(value: string): string;
    function IsRecordLike(value: string): string;
    function IsNumberLike(value: string): string;
    function IsVoidLike(value: string): string;
}
export type TypeCompilerLanguageOption = 'typescript' | 'javascript';
export interface TypeCompilerCodegenOptions {
    language?: TypeCompilerLanguageOption;
}
/** Compiles Types for Runtime Type Checking */
export declare namespace TypeCompiler {
    /** Generates the code used to assert this type and returns it as a string */
    function Code<T extends Types.TSchema>(schema: T, references: Types.TSchema[], options?: TypeCompilerCodegenOptions): string;
    /** Generates the code used to assert this type and returns it as a string */
    function Code<T extends Types.TSchema>(schema: T, options?: TypeCompilerCodegenOptions): string;
    /** Compiles a TypeBox type for optimal runtime type checking. Types must be valid TypeBox types of TSchema */
    function Compile<T extends Types.TSchema>(schema: T, references?: Types.TSchema[]): TypeCheck<T>;
}
