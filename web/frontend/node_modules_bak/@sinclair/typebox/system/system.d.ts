import { ValueErrorType } from '../errors/errors';
import * as Types from '../typebox';
export declare class TypeSystemDuplicateTypeKind extends Types.TypeBoxError {
    constructor(kind: string);
}
export declare class TypeSystemDuplicateFormat extends Types.TypeBoxError {
    constructor(kind: string);
}
/** Creates user defined types and formats and provides overrides for value checking behaviours */
export declare namespace TypeSystem {
    /** Creates a new type */
    function Type<Type, Options = Record<PropertyKey, unknown>>(kind: string, check: (options: Options, value: unknown) => boolean): (options?: Partial<Options>) => Types.TUnsafe<Type>;
    /** Creates a new string format */
    function Format<F extends string>(format: F, check: (value: string) => boolean): F;
}
/** Manages error message providers */
export declare namespace TypeSystemErrorFunction {
    /** Resets the error message function to en-us */
    function Reset(): void;
    /** Sets the error message function used to generate error messages */
    function Set(callback: ErrorFunction): void;
    /** Gets the error message function */
    function Get(): ErrorFunction;
}
/** Shared assertion routines used by the value and errors modules */
export declare namespace TypeSystemPolicy {
    /** Sets whether TypeBox should assert optional properties using the TypeScript `exactOptionalPropertyTypes` assertion policy. The default is `false` */
    let ExactOptionalPropertyTypes: boolean;
    /** Sets whether arrays should be treated as a kind of objects. The default is `false` */
    let AllowArrayObject: boolean;
    /** Sets whether `NaN` or `Infinity` should be treated as valid numeric values. The default is `false` */
    let AllowNaN: boolean;
    /** Sets whether `null` should validate for void types. The default is `false` */
    let AllowNullVoid: boolean;
    /** Asserts this value using the ExactOptionalPropertyTypes policy */
    function IsExactOptionalProperty(value: Record<keyof any, unknown>, key: string): boolean;
    /** Asserts this value using the AllowArrayObjects policy */
    function IsObjectLike(value: unknown): value is Record<keyof any, unknown>;
    /** Asserts this value as a record using the AllowArrayObjects policy */
    function IsRecordLike(value: unknown): value is Record<keyof any, unknown>;
    /** Asserts this value using the AllowNaN policy */
    function IsNumberLike(value: unknown): value is number;
    /** Asserts this value using the AllowVoidNull policy */
    function IsVoidLike(value: unknown): value is void;
}
export type ErrorFunction = (schema: Types.TSchema, type: ValueErrorType) => string;
/** Creates an error message using en-US as the default locale */
export declare function DefaultErrorFunction(schema: Types.TSchema, errorType: ValueErrorType): string;
