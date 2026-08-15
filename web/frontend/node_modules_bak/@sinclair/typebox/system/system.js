"use strict";
/*--------------------------------------------------------------------------

@sinclair/typebox/system

The MIT License (MIT)

Copyright (c) 2017-2023 Haydn Paterson (sinclair) <haydn.developer@gmail.com>

Permission is hereby granted, free of charge, to any person obtaining a copy
of this software and associated documentation files (the "Software"), to deal
in the Software without restriction, including without limitation the rights
to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
copies of the Software, and to permit persons to whom the Software is
furnished to do so, subject to the following conditions:

The above copyright notice and this permission notice shall be included in
all copies or substantial portions of the Software.

THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN
THE SOFTWARE.

---------------------------------------------------------------------------*/
Object.defineProperty(exports, "__esModule", { value: true });
exports.DefaultErrorFunction = exports.TypeSystemPolicy = exports.TypeSystemErrorFunction = exports.TypeSystem = exports.TypeSystemDuplicateFormat = exports.TypeSystemDuplicateTypeKind = void 0;
const guard_1 = require("../value/guard");
const errors_1 = require("../errors/errors");
const Types = require("../typebox");
// --------------------------------------------------------------------------
// Errors
// --------------------------------------------------------------------------
class TypeSystemDuplicateTypeKind extends Types.TypeBoxError {
    constructor(kind) {
        super(`Duplicate type kind '${kind}' detected`);
    }
}
exports.TypeSystemDuplicateTypeKind = TypeSystemDuplicateTypeKind;
class TypeSystemDuplicateFormat extends Types.TypeBoxError {
    constructor(kind) {
        super(`Duplicate string format '${kind}' detected`);
    }
}
exports.TypeSystemDuplicateFormat = TypeSystemDuplicateFormat;
// -------------------------------------------------------------------------------------------
// TypeSystem
// -------------------------------------------------------------------------------------------
/** Creates user defined types and formats and provides overrides for value checking behaviours */
var TypeSystem;
(function (TypeSystem) {
    /** Creates a new type */
    function Type(kind, check) {
        if (Types.TypeRegistry.Has(kind))
            throw new TypeSystemDuplicateTypeKind(kind);
        Types.TypeRegistry.Set(kind, check);
        return (options = {}) => Types.Type.Unsafe({ ...options, [Types.Kind]: kind });
    }
    TypeSystem.Type = Type;
    /** Creates a new string format */
    function Format(format, check) {
        if (Types.FormatRegistry.Has(format))
            throw new TypeSystemDuplicateFormat(format);
        Types.FormatRegistry.Set(format, check);
        return format;
    }
    TypeSystem.Format = Format;
})(TypeSystem || (exports.TypeSystem = TypeSystem = {}));
// --------------------------------------------------------------------------
// TypeSystemErrorFunction
// --------------------------------------------------------------------------
/** Manages error message providers */
var TypeSystemErrorFunction;
(function (TypeSystemErrorFunction) {
    let errorMessageFunction = DefaultErrorFunction;
    /** Resets the error message function to en-us */
    function Reset() {
        errorMessageFunction = DefaultErrorFunction;
    }
    TypeSystemErrorFunction.Reset = Reset;
    /** Sets the error message function used to generate error messages */
    function Set(callback) {
        errorMessageFunction = callback;
    }
    TypeSystemErrorFunction.Set = Set;
    /** Gets the error message function */
    function Get() {
        return errorMessageFunction;
    }
    TypeSystemErrorFunction.Get = Get;
})(TypeSystemErrorFunction || (exports.TypeSystemErrorFunction = TypeSystemErrorFunction = {}));
// --------------------------------------------------------------------------
// TypeSystemPolicy
// --------------------------------------------------------------------------
/** Shared assertion routines used by the value and errors modules */
var TypeSystemPolicy;
(function (TypeSystemPolicy) {
    /** Sets whether TypeBox should assert optional properties using the TypeScript `exactOptionalPropertyTypes` assertion policy. The default is `false` */
    TypeSystemPolicy.ExactOptionalPropertyTypes = false;
    /** Sets whether arrays should be treated as a kind of objects. The default is `false` */
    TypeSystemPolicy.AllowArrayObject = false;
    /** Sets whether `NaN` or `Infinity` should be treated as valid numeric values. The default is `false` */
    TypeSystemPolicy.AllowNaN = false;
    /** Sets whether `null` should validate for void types. The default is `false` */
    TypeSystemPolicy.AllowNullVoid = false;
    /** Asserts this value using the ExactOptionalPropertyTypes policy */
    function IsExactOptionalProperty(value, key) {
        return TypeSystemPolicy.ExactOptionalPropertyTypes ? key in value : value[key] !== undefined;
    }
    TypeSystemPolicy.IsExactOptionalProperty = IsExactOptionalProperty;
    /** Asserts this value using the AllowArrayObjects policy */
    function IsObjectLike(value) {
        const isObject = (0, guard_1.IsObject)(value);
        return TypeSystemPolicy.AllowArrayObject ? isObject : isObject && !(0, guard_1.IsArray)(value);
    }
    TypeSystemPolicy.IsObjectLike = IsObjectLike;
    /** Asserts this value as a record using the AllowArrayObjects policy */
    function IsRecordLike(value) {
        return IsObjectLike(value) && !(value instanceof Date) && !(value instanceof Uint8Array);
    }
    TypeSystemPolicy.IsRecordLike = IsRecordLike;
    /** Asserts this value using the AllowNaN policy */
    function IsNumberLike(value) {
        const isNumber = (0, guard_1.IsNumber)(value);
        return TypeSystemPolicy.AllowNaN ? isNumber : isNumber && Number.isFinite(value);
    }
    TypeSystemPolicy.IsNumberLike = IsNumberLike;
    /** Asserts this value using the AllowVoidNull policy */
    function IsVoidLike(value) {
        const isUndefined = (0, guard_1.IsUndefined)(value);
        return TypeSystemPolicy.AllowNullVoid ? isUndefined || value === null : isUndefined;
    }
    TypeSystemPolicy.IsVoidLike = IsVoidLike;
})(TypeSystemPolicy || (exports.TypeSystemPolicy = TypeSystemPolicy = {}));
// --------------------------------------------------------------------------
// DefaultErrorFunction
// --------------------------------------------------------------------------
/** Creates an error message using en-US as the default locale */
function DefaultErrorFunction(schema, errorType) {
    switch (errorType) {
        case errors_1.ValueErrorType.ArrayContains:
            return 'Expected array to contain at least one matching value';
        case errors_1.ValueErrorType.ArrayMaxContains:
            return `Expected array to contain no more than ${schema.maxContains} matching values`;
        case errors_1.ValueErrorType.ArrayMinContains:
            return `Expected array to contain at least ${schema.minContains} matching values`;
        case errors_1.ValueErrorType.ArrayMaxItems:
            return `Expected array length to be less or equal to ${schema.maxItems}`;
        case errors_1.ValueErrorType.ArrayMinItems:
            return `Expected array length to be greater or equal to ${schema.minItems}`;
        case errors_1.ValueErrorType.ArrayUniqueItems:
            return 'Expected array elements to be unique';
        case errors_1.ValueErrorType.Array:
            return 'Expected array';
        case errors_1.ValueErrorType.AsyncIterator:
            return 'Expected AsyncIterator';
        case errors_1.ValueErrorType.BigIntExclusiveMaximum:
            return `Expected bigint to be less than ${schema.exclusiveMaximum}`;
        case errors_1.ValueErrorType.BigIntExclusiveMinimum:
            return `Expected bigint to be greater than ${schema.exclusiveMinimum}`;
        case errors_1.ValueErrorType.BigIntMaximum:
            return `Expected bigint to be less or equal to ${schema.maximum}`;
        case errors_1.ValueErrorType.BigIntMinimum:
            return `Expected bigint to be greater or equal to ${schema.minimum}`;
        case errors_1.ValueErrorType.BigIntMultipleOf:
            return `Expected bigint to be a multiple of ${schema.multipleOf}`;
        case errors_1.ValueErrorType.BigInt:
            return 'Expected bigint';
        case errors_1.ValueErrorType.Boolean:
            return 'Expected boolean';
        case errors_1.ValueErrorType.DateExclusiveMinimumTimestamp:
            return `Expected Date timestamp to be greater than ${schema.exclusiveMinimumTimestamp}`;
        case errors_1.ValueErrorType.DateExclusiveMaximumTimestamp:
            return `Expected Date timestamp to be less than ${schema.exclusiveMaximumTimestamp}`;
        case errors_1.ValueErrorType.DateMinimumTimestamp:
            return `Expected Date timestamp to be greater or equal to ${schema.minimumTimestamp}`;
        case errors_1.ValueErrorType.DateMaximumTimestamp:
            return `Expected Date timestamp to be less or equal to ${schema.maximumTimestamp}`;
        case errors_1.ValueErrorType.DateMultipleOfTimestamp:
            return `Expected Date timestamp to be a multiple of ${schema.multipleOfTimestamp}`;
        case errors_1.ValueErrorType.Date:
            return 'Expected Date';
        case errors_1.ValueErrorType.Function:
            return 'Expected function';
        case errors_1.ValueErrorType.IntegerExclusiveMaximum:
            return `Expected integer to be less than ${schema.exclusiveMaximum}`;
        case errors_1.ValueErrorType.IntegerExclusiveMinimum:
            return `Expected integer to be greater than ${schema.exclusiveMinimum}`;
        case errors_1.ValueErrorType.IntegerMaximum:
            return `Expected integer to be less or equal to ${schema.maximum}`;
        case errors_1.ValueErrorType.IntegerMinimum:
            return `Expected integer to be greater or equal to ${schema.minimum}`;
        case errors_1.ValueErrorType.IntegerMultipleOf:
            return `Expected integer to be a multiple of ${schema.multipleOf}`;
        case errors_1.ValueErrorType.Integer:
            return 'Expected integer';
        case errors_1.ValueErrorType.IntersectUnevaluatedProperties:
            return 'Unexpected property';
        case errors_1.ValueErrorType.Intersect:
            return 'Expected all values to match';
        case errors_1.ValueErrorType.Iterator:
            return 'Expected Iterator';
        case errors_1.ValueErrorType.Literal:
            return `Expected ${typeof schema.const === 'string' ? `'${schema.const}'` : schema.const}`;
        case errors_1.ValueErrorType.Never:
            return 'Never';
        case errors_1.ValueErrorType.Not:
            return 'Value should not match';
        case errors_1.ValueErrorType.Null:
            return 'Expected null';
        case errors_1.ValueErrorType.NumberExclusiveMaximum:
            return `Expected number to be less than ${schema.exclusiveMaximum}`;
        case errors_1.ValueErrorType.NumberExclusiveMinimum:
            return `Expected number to be greater than ${schema.exclusiveMinimum}`;
        case errors_1.ValueErrorType.NumberMaximum:
            return `Expected number to be less or equal to ${schema.maximum}`;
        case errors_1.ValueErrorType.NumberMinimum:
            return `Expected number to be greater or equal to ${schema.minimum}`;
        case errors_1.ValueErrorType.NumberMultipleOf:
            return `Expected number to be a multiple of ${schema.multipleOf}`;
        case errors_1.ValueErrorType.Number:
            return 'Expected number';
        case errors_1.ValueErrorType.Object:
            return 'Expected object';
        case errors_1.ValueErrorType.ObjectAdditionalProperties:
            return 'Unexpected property';
        case errors_1.ValueErrorType.ObjectMaxProperties:
            return `Expected object to have no more than ${schema.maxProperties} properties`;
        case errors_1.ValueErrorType.ObjectMinProperties:
            return `Expected object to have at least ${schema.minProperties} properties`;
        case errors_1.ValueErrorType.ObjectRequiredProperty:
            return 'Required property';
        case errors_1.ValueErrorType.Promise:
            return 'Expected Promise';
        case errors_1.ValueErrorType.StringFormatUnknown:
            return `Unknown format '${schema.format}'`;
        case errors_1.ValueErrorType.StringFormat:
            return `Expected string to match '${schema.format}' format`;
        case errors_1.ValueErrorType.StringMaxLength:
            return `Expected string length less or equal to ${schema.maxLength}`;
        case errors_1.ValueErrorType.StringMinLength:
            return `Expected string length greater or equal to ${schema.minLength}`;
        case errors_1.ValueErrorType.StringPattern:
            return `Expected string to match '${schema.pattern}'`;
        case errors_1.ValueErrorType.String:
            return 'Expected string';
        case errors_1.ValueErrorType.Symbol:
            return 'Expected symbol';
        case errors_1.ValueErrorType.TupleLength:
            return `Expected tuple to have ${schema.maxItems || 0} elements`;
        case errors_1.ValueErrorType.Tuple:
            return 'Expected tuple';
        case errors_1.ValueErrorType.Uint8ArrayMaxByteLength:
            return `Expected byte length less or equal to ${schema.maxByteLength}`;
        case errors_1.ValueErrorType.Uint8ArrayMinByteLength:
            return `Expected byte length greater or equal to ${schema.minByteLength}`;
        case errors_1.ValueErrorType.Uint8Array:
            return 'Expected Uint8Array';
        case errors_1.ValueErrorType.Undefined:
            return 'Expected undefined';
        case errors_1.ValueErrorType.Union:
            return 'Expected union value';
        case errors_1.ValueErrorType.Void:
            return 'Expected void';
        case errors_1.ValueErrorType.Kind:
            return `Expected kind '${schema[Types.Kind]}'`;
        default:
            return 'Unknown error type';
    }
}
exports.DefaultErrorFunction = DefaultErrorFunction;
