"use strict";
/*--------------------------------------------------------------------------

@sinclair/typebox

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
exports.Type = exports.JsonType = exports.JavaScriptTypeBuilder = exports.JsonTypeBuilder = exports.TypeBuilder = exports.TypeBuilderError = exports.TransformEncodeBuilder = exports.TransformDecodeBuilder = exports.TemplateLiteralDslParser = exports.TemplateLiteralGenerator = exports.TemplateLiteralGeneratorError = exports.TemplateLiteralFinite = exports.TemplateLiteralFiniteError = exports.TemplateLiteralParser = exports.TemplateLiteralParserError = exports.TemplateLiteralResolver = exports.TemplateLiteralPattern = exports.TemplateLiteralPatternError = exports.UnionResolver = exports.KeyArrayResolver = exports.KeyArrayResolverError = exports.KeyResolver = exports.ObjectMap = exports.Intrinsic = exports.IndexedAccessor = exports.TypeClone = exports.TypeExtends = exports.TypeExtendsResult = exports.TypeExtendsError = exports.ExtendsUndefined = exports.TypeGuard = exports.TypeGuardUnknownTypeError = exports.ValueGuard = exports.FormatRegistry = exports.TypeBoxError = exports.TypeRegistry = exports.PatternStringExact = exports.PatternNumberExact = exports.PatternBooleanExact = exports.PatternString = exports.PatternNumber = exports.PatternBoolean = exports.Kind = exports.Hint = exports.Optional = exports.Readonly = exports.Transform = void 0;
// --------------------------------------------------------------------------
// Symbols
// --------------------------------------------------------------------------
exports.Transform = Symbol.for('TypeBox.Transform');
exports.Readonly = Symbol.for('TypeBox.Readonly');
exports.Optional = Symbol.for('TypeBox.Optional');
exports.Hint = Symbol.for('TypeBox.Hint');
exports.Kind = Symbol.for('TypeBox.Kind');
// --------------------------------------------------------------------------
// Patterns
// --------------------------------------------------------------------------
exports.PatternBoolean = '(true|false)';
exports.PatternNumber = '(0|[1-9][0-9]*)';
exports.PatternString = '(.*)';
exports.PatternBooleanExact = `^${exports.PatternBoolean}$`;
exports.PatternNumberExact = `^${exports.PatternNumber}$`;
exports.PatternStringExact = `^${exports.PatternString}$`;
/** A registry for user defined types */
var TypeRegistry;
(function (TypeRegistry) {
    const map = new Map();
    /** Returns the entries in this registry */
    function Entries() {
        return new Map(map);
    }
    TypeRegistry.Entries = Entries;
    /** Clears all user defined types */
    function Clear() {
        return map.clear();
    }
    TypeRegistry.Clear = Clear;
    /** Deletes a registered type */
    function Delete(kind) {
        return map.delete(kind);
    }
    TypeRegistry.Delete = Delete;
    /** Returns true if this registry contains this kind */
    function Has(kind) {
        return map.has(kind);
    }
    TypeRegistry.Has = Has;
    /** Sets a validation function for a user defined type */
    function Set(kind, func) {
        map.set(kind, func);
    }
    TypeRegistry.Set = Set;
    /** Gets a custom validation function for a user defined type */
    function Get(kind) {
        return map.get(kind);
    }
    TypeRegistry.Get = Get;
})(TypeRegistry || (exports.TypeRegistry = TypeRegistry = {}));
// --------------------------------------------------------------------------
// TypeBoxError
// --------------------------------------------------------------------------
class TypeBoxError extends Error {
    constructor(message) {
        super(message);
    }
}
exports.TypeBoxError = TypeBoxError;
/** A registry for user defined string formats */
var FormatRegistry;
(function (FormatRegistry) {
    const map = new Map();
    /** Returns the entries in this registry */
    function Entries() {
        return new Map(map);
    }
    FormatRegistry.Entries = Entries;
    /** Clears all user defined string formats */
    function Clear() {
        return map.clear();
    }
    FormatRegistry.Clear = Clear;
    /** Deletes a registered format */
    function Delete(format) {
        return map.delete(format);
    }
    FormatRegistry.Delete = Delete;
    /** Returns true if the user defined string format exists */
    function Has(format) {
        return map.has(format);
    }
    FormatRegistry.Has = Has;
    /** Sets a validation function for a user defined string format */
    function Set(format, func) {
        map.set(format, func);
    }
    FormatRegistry.Set = Set;
    /** Gets a validation function for a user defined string format */
    function Get(format) {
        return map.get(format);
    }
    FormatRegistry.Get = Get;
})(FormatRegistry || (exports.FormatRegistry = FormatRegistry = {}));
// --------------------------------------------------------------------------
// ValueGuard
// --------------------------------------------------------------------------
/** Provides functions to type guard raw JavaScript values */
var ValueGuard;
(function (ValueGuard) {
    /** Returns true if this value is an array */
    function IsArray(value) {
        return Array.isArray(value);
    }
    ValueGuard.IsArray = IsArray;
    /** Returns true if this value is bigint */
    function IsBigInt(value) {
        return typeof value === 'bigint';
    }
    ValueGuard.IsBigInt = IsBigInt;
    /** Returns true if this value is a boolean */
    function IsBoolean(value) {
        return typeof value === 'boolean';
    }
    ValueGuard.IsBoolean = IsBoolean;
    /** Returns true if this value is a Date object */
    function IsDate(value) {
        return value instanceof globalThis.Date;
    }
    ValueGuard.IsDate = IsDate;
    /** Returns true if this value is null */
    function IsNull(value) {
        return value === null;
    }
    ValueGuard.IsNull = IsNull;
    /** Returns true if this value is number */
    function IsNumber(value) {
        return typeof value === 'number';
    }
    ValueGuard.IsNumber = IsNumber;
    /** Returns true if this value is an object */
    function IsObject(value) {
        return typeof value === 'object' && value !== null;
    }
    ValueGuard.IsObject = IsObject;
    /** Returns true if this value is string */
    function IsString(value) {
        return typeof value === 'string';
    }
    ValueGuard.IsString = IsString;
    /** Returns true if this value is a Uint8Array */
    function IsUint8Array(value) {
        return value instanceof globalThis.Uint8Array;
    }
    ValueGuard.IsUint8Array = IsUint8Array;
    /** Returns true if this value is undefined */
    function IsUndefined(value) {
        return value === undefined;
    }
    ValueGuard.IsUndefined = IsUndefined;
})(ValueGuard || (exports.ValueGuard = ValueGuard = {}));
// --------------------------------------------------------------------------
// TypeGuard
// --------------------------------------------------------------------------
class TypeGuardUnknownTypeError extends TypeBoxError {
}
exports.TypeGuardUnknownTypeError = TypeGuardUnknownTypeError;
/** Provides functions to test if JavaScript values are TypeBox types */
var TypeGuard;
(function (TypeGuard) {
    function IsPattern(value) {
        try {
            new RegExp(value);
            return true;
        }
        catch {
            return false;
        }
    }
    function IsControlCharacterFree(value) {
        if (!ValueGuard.IsString(value))
            return false;
        for (let i = 0; i < value.length; i++) {
            const code = value.charCodeAt(i);
            if ((code >= 7 && code <= 13) || code === 27 || code === 127) {
                return false;
            }
        }
        return true;
    }
    function IsAdditionalProperties(value) {
        return IsOptionalBoolean(value) || TSchema(value);
    }
    function IsOptionalBigInt(value) {
        return ValueGuard.IsUndefined(value) || ValueGuard.IsBigInt(value);
    }
    function IsOptionalNumber(value) {
        return ValueGuard.IsUndefined(value) || ValueGuard.IsNumber(value);
    }
    function IsOptionalBoolean(value) {
        return ValueGuard.IsUndefined(value) || ValueGuard.IsBoolean(value);
    }
    function IsOptionalString(value) {
        return ValueGuard.IsUndefined(value) || ValueGuard.IsString(value);
    }
    function IsOptionalPattern(value) {
        return ValueGuard.IsUndefined(value) || (ValueGuard.IsString(value) && IsControlCharacterFree(value) && IsPattern(value));
    }
    function IsOptionalFormat(value) {
        return ValueGuard.IsUndefined(value) || (ValueGuard.IsString(value) && IsControlCharacterFree(value));
    }
    function IsOptionalSchema(value) {
        return ValueGuard.IsUndefined(value) || TSchema(value);
    }
    // ----------------------------------------------------------------
    // Types
    // ----------------------------------------------------------------
    /** Returns true if the given value is TAny */
    function TAny(schema) {
        // prettier-ignore
        return (TKindOf(schema, 'Any') &&
            IsOptionalString(schema.$id));
    }
    TypeGuard.TAny = TAny;
    /** Returns true if the given value is TArray */
    function TArray(schema) {
        return (TKindOf(schema, 'Array') &&
            schema.type === 'array' &&
            IsOptionalString(schema.$id) &&
            TSchema(schema.items) &&
            IsOptionalNumber(schema.minItems) &&
            IsOptionalNumber(schema.maxItems) &&
            IsOptionalBoolean(schema.uniqueItems) &&
            IsOptionalSchema(schema.contains) &&
            IsOptionalNumber(schema.minContains) &&
            IsOptionalNumber(schema.maxContains));
    }
    TypeGuard.TArray = TArray;
    /** Returns true if the given value is TAsyncIterator */
    function TAsyncIterator(schema) {
        // prettier-ignore
        return (TKindOf(schema, 'AsyncIterator') &&
            schema.type === 'AsyncIterator' &&
            IsOptionalString(schema.$id) &&
            TSchema(schema.items));
    }
    TypeGuard.TAsyncIterator = TAsyncIterator;
    /** Returns true if the given value is TBigInt */
    function TBigInt(schema) {
        // prettier-ignore
        return (TKindOf(schema, 'BigInt') &&
            schema.type === 'bigint' &&
            IsOptionalString(schema.$id) &&
            IsOptionalBigInt(schema.exclusiveMaximum) &&
            IsOptionalBigInt(schema.exclusiveMinimum) &&
            IsOptionalBigInt(schema.maximum) &&
            IsOptionalBigInt(schema.minimum) &&
            IsOptionalBigInt(schema.multipleOf));
    }
    TypeGuard.TBigInt = TBigInt;
    /** Returns true if the given value is TBoolean */
    function TBoolean(schema) {
        // prettier-ignore
        return (TKindOf(schema, 'Boolean') &&
            schema.type === 'boolean' &&
            IsOptionalString(schema.$id));
    }
    TypeGuard.TBoolean = TBoolean;
    /** Returns true if the given value is TConstructor */
    function TConstructor(schema) {
        // prettier-ignore
        return (TKindOf(schema, 'Constructor') &&
            schema.type === 'Constructor' &&
            IsOptionalString(schema.$id) &&
            ValueGuard.IsArray(schema.parameters) &&
            schema.parameters.every(schema => TSchema(schema)) &&
            TSchema(schema.returns));
    }
    TypeGuard.TConstructor = TConstructor;
    /** Returns true if the given value is TDate */
    function TDate(schema) {
        return (TKindOf(schema, 'Date') &&
            schema.type === 'Date' &&
            IsOptionalString(schema.$id) &&
            IsOptionalNumber(schema.exclusiveMaximumTimestamp) &&
            IsOptionalNumber(schema.exclusiveMinimumTimestamp) &&
            IsOptionalNumber(schema.maximumTimestamp) &&
            IsOptionalNumber(schema.minimumTimestamp) &&
            IsOptionalNumber(schema.multipleOfTimestamp));
    }
    TypeGuard.TDate = TDate;
    /** Returns true if the given value is TFunction */
    function TFunction(schema) {
        // prettier-ignore
        return (TKindOf(schema, 'Function') &&
            schema.type === 'Function' &&
            IsOptionalString(schema.$id) &&
            ValueGuard.IsArray(schema.parameters) &&
            schema.parameters.every(schema => TSchema(schema)) &&
            TSchema(schema.returns));
    }
    TypeGuard.TFunction = TFunction;
    /** Returns true if the given value is TInteger */
    function TInteger(schema) {
        return (TKindOf(schema, 'Integer') &&
            schema.type === 'integer' &&
            IsOptionalString(schema.$id) &&
            IsOptionalNumber(schema.exclusiveMaximum) &&
            IsOptionalNumber(schema.exclusiveMinimum) &&
            IsOptionalNumber(schema.maximum) &&
            IsOptionalNumber(schema.minimum) &&
            IsOptionalNumber(schema.multipleOf));
    }
    TypeGuard.TInteger = TInteger;
    /** Returns true if the given value is TIntersect */
    function TIntersect(schema) {
        // prettier-ignore
        return (TKindOf(schema, 'Intersect') &&
            (ValueGuard.IsString(schema.type) && schema.type !== 'object' ? false : true) &&
            ValueGuard.IsArray(schema.allOf) &&
            schema.allOf.every(schema => TSchema(schema) && !TTransform(schema)) &&
            IsOptionalString(schema.type) &&
            (IsOptionalBoolean(schema.unevaluatedProperties) || IsOptionalSchema(schema.unevaluatedProperties)) &&
            IsOptionalString(schema.$id));
    }
    TypeGuard.TIntersect = TIntersect;
    /** Returns true if the given value is TIterator */
    function TIterator(schema) {
        // prettier-ignore
        return (TKindOf(schema, 'Iterator') &&
            schema.type === 'Iterator' &&
            IsOptionalString(schema.$id) &&
            TSchema(schema.items));
    }
    TypeGuard.TIterator = TIterator;
    /** Returns true if the given value is a TKind with the given name. */
    function TKindOf(schema, kind) {
        return TKind(schema) && schema[exports.Kind] === kind;
    }
    TypeGuard.TKindOf = TKindOf;
    /** Returns true if the given value is TKind */
    function TKind(schema) {
        return ValueGuard.IsObject(schema) && exports.Kind in schema && ValueGuard.IsString(schema[exports.Kind]);
    }
    TypeGuard.TKind = TKind;
    /** Returns true if the given value is TLiteral<string> */
    function TLiteralString(schema) {
        return TLiteral(schema) && ValueGuard.IsString(schema.const);
    }
    TypeGuard.TLiteralString = TLiteralString;
    /** Returns true if the given value is TLiteral<number> */
    function TLiteralNumber(schema) {
        return TLiteral(schema) && ValueGuard.IsNumber(schema.const);
    }
    TypeGuard.TLiteralNumber = TLiteralNumber;
    /** Returns true if the given value is TLiteral<boolean> */
    function TLiteralBoolean(schema) {
        return TLiteral(schema) && ValueGuard.IsBoolean(schema.const);
    }
    TypeGuard.TLiteralBoolean = TLiteralBoolean;
    /** Returns true if the given value is TLiteral */
    function TLiteral(schema) {
        // prettier-ignore
        return (TKindOf(schema, 'Literal') &&
            IsOptionalString(schema.$id) && (ValueGuard.IsBoolean(schema.const) ||
            ValueGuard.IsNumber(schema.const) ||
            ValueGuard.IsString(schema.const)));
    }
    TypeGuard.TLiteral = TLiteral;
    /** Returns true if the given value is TNever */
    function TNever(schema) {
        // prettier-ignore
        return (TKindOf(schema, 'Never') &&
            ValueGuard.IsObject(schema.not) &&
            Object.getOwnPropertyNames(schema.not).length === 0);
    }
    TypeGuard.TNever = TNever;
    /** Returns true if the given value is TNot */
    function TNot(schema) {
        // prettier-ignore
        return (TKindOf(schema, 'Not') &&
            TSchema(schema.not));
    }
    TypeGuard.TNot = TNot;
    /** Returns true if the given value is TNull */
    function TNull(schema) {
        // prettier-ignore
        return (TKindOf(schema, 'Null') &&
            schema.type === 'null' &&
            IsOptionalString(schema.$id));
    }
    TypeGuard.TNull = TNull;
    /** Returns true if the given value is TNumber */
    function TNumber(schema) {
        return (TKindOf(schema, 'Number') &&
            schema.type === 'number' &&
            IsOptionalString(schema.$id) &&
            IsOptionalNumber(schema.exclusiveMaximum) &&
            IsOptionalNumber(schema.exclusiveMinimum) &&
            IsOptionalNumber(schema.maximum) &&
            IsOptionalNumber(schema.minimum) &&
            IsOptionalNumber(schema.multipleOf));
    }
    TypeGuard.TNumber = TNumber;
    /** Returns true if the given value is TObject */
    function TObject(schema) {
        // prettier-ignore
        return (TKindOf(schema, 'Object') &&
            schema.type === 'object' &&
            IsOptionalString(schema.$id) &&
            ValueGuard.IsObject(schema.properties) &&
            IsAdditionalProperties(schema.additionalProperties) &&
            IsOptionalNumber(schema.minProperties) &&
            IsOptionalNumber(schema.maxProperties) &&
            Object.entries(schema.properties).every(([key, schema]) => IsControlCharacterFree(key) && TSchema(schema)));
    }
    TypeGuard.TObject = TObject;
    /** Returns true if the given value is TPromise */
    function TPromise(schema) {
        // prettier-ignore
        return (TKindOf(schema, 'Promise') &&
            schema.type === 'Promise' &&
            IsOptionalString(schema.$id) &&
            TSchema(schema.item));
    }
    TypeGuard.TPromise = TPromise;
    /** Returns true if the given value is TRecord */
    function TRecord(schema) {
        // prettier-ignore
        return (TKindOf(schema, 'Record') &&
            schema.type === 'object' &&
            IsOptionalString(schema.$id) &&
            IsAdditionalProperties(schema.additionalProperties) &&
            ValueGuard.IsObject(schema.patternProperties) &&
            ((schema) => {
                const keys = Object.getOwnPropertyNames(schema.patternProperties);
                return (keys.length === 1 &&
                    IsPattern(keys[0]) &&
                    ValueGuard.IsObject(schema.patternProperties) &&
                    TSchema(schema.patternProperties[keys[0]]));
            })(schema));
    }
    TypeGuard.TRecord = TRecord;
    /** Returns true if this value is TRecursive */
    function TRecursive(schema) {
        return ValueGuard.IsObject(schema) && exports.Hint in schema && schema[exports.Hint] === 'Recursive';
    }
    TypeGuard.TRecursive = TRecursive;
    /** Returns true if the given value is TRef */
    function TRef(schema) {
        // prettier-ignore
        return (TKindOf(schema, 'Ref') &&
            IsOptionalString(schema.$id) &&
            ValueGuard.IsString(schema.$ref));
    }
    TypeGuard.TRef = TRef;
    /** Returns true if the given value is TString */
    function TString(schema) {
        // prettier-ignore
        return (TKindOf(schema, 'String') &&
            schema.type === 'string' &&
            IsOptionalString(schema.$id) &&
            IsOptionalNumber(schema.minLength) &&
            IsOptionalNumber(schema.maxLength) &&
            IsOptionalPattern(schema.pattern) &&
            IsOptionalFormat(schema.format));
    }
    TypeGuard.TString = TString;
    /** Returns true if the given value is TSymbol */
    function TSymbol(schema) {
        // prettier-ignore
        return (TKindOf(schema, 'Symbol') &&
            schema.type === 'symbol' &&
            IsOptionalString(schema.$id));
    }
    TypeGuard.TSymbol = TSymbol;
    /** Returns true if the given value is TTemplateLiteral */
    function TTemplateLiteral(schema) {
        // prettier-ignore
        return (TKindOf(schema, 'TemplateLiteral') &&
            schema.type === 'string' &&
            ValueGuard.IsString(schema.pattern) &&
            schema.pattern[0] === '^' &&
            schema.pattern[schema.pattern.length - 1] === '$');
    }
    TypeGuard.TTemplateLiteral = TTemplateLiteral;
    /** Returns true if the given value is TThis */
    function TThis(schema) {
        // prettier-ignore
        return (TKindOf(schema, 'This') &&
            IsOptionalString(schema.$id) &&
            ValueGuard.IsString(schema.$ref));
    }
    TypeGuard.TThis = TThis;
    /** Returns true of this value is TTransform */
    function TTransform(schema) {
        return ValueGuard.IsObject(schema) && exports.Transform in schema;
    }
    TypeGuard.TTransform = TTransform;
    /** Returns true if the given value is TTuple */
    function TTuple(schema) {
        // prettier-ignore
        return (TKindOf(schema, 'Tuple') &&
            schema.type === 'array' &&
            IsOptionalString(schema.$id) &&
            ValueGuard.IsNumber(schema.minItems) &&
            ValueGuard.IsNumber(schema.maxItems) &&
            schema.minItems === schema.maxItems &&
            (( // empty
            ValueGuard.IsUndefined(schema.items) &&
                ValueGuard.IsUndefined(schema.additionalItems) &&
                schema.minItems === 0) || (ValueGuard.IsArray(schema.items) &&
                schema.items.every(schema => TSchema(schema)))));
    }
    TypeGuard.TTuple = TTuple;
    /** Returns true if the given value is TUndefined */
    function TUndefined(schema) {
        // prettier-ignore
        return (TKindOf(schema, 'Undefined') &&
            schema.type === 'undefined' &&
            IsOptionalString(schema.$id));
    }
    TypeGuard.TUndefined = TUndefined;
    /** Returns true if the given value is TUnion<Literal<string | number>[]> */
    function TUnionLiteral(schema) {
        return TUnion(schema) && schema.anyOf.every((schema) => TLiteralString(schema) || TLiteralNumber(schema));
    }
    TypeGuard.TUnionLiteral = TUnionLiteral;
    /** Returns true if the given value is TUnion */
    function TUnion(schema) {
        // prettier-ignore
        return (TKindOf(schema, 'Union') &&
            IsOptionalString(schema.$id) &&
            ValueGuard.IsObject(schema) &&
            ValueGuard.IsArray(schema.anyOf) &&
            schema.anyOf.every(schema => TSchema(schema)));
    }
    TypeGuard.TUnion = TUnion;
    /** Returns true if the given value is TUint8Array */
    function TUint8Array(schema) {
        // prettier-ignore
        return (TKindOf(schema, 'Uint8Array') &&
            schema.type === 'Uint8Array' &&
            IsOptionalString(schema.$id) &&
            IsOptionalNumber(schema.minByteLength) &&
            IsOptionalNumber(schema.maxByteLength));
    }
    TypeGuard.TUint8Array = TUint8Array;
    /** Returns true if the given value is TUnknown */
    function TUnknown(schema) {
        // prettier-ignore
        return (TKindOf(schema, 'Unknown') &&
            IsOptionalString(schema.$id));
    }
    TypeGuard.TUnknown = TUnknown;
    /** Returns true if the given value is a raw TUnsafe */
    function TUnsafe(schema) {
        return TKindOf(schema, 'Unsafe');
    }
    TypeGuard.TUnsafe = TUnsafe;
    /** Returns true if the given value is TVoid */
    function TVoid(schema) {
        // prettier-ignore
        return (TKindOf(schema, 'Void') &&
            schema.type === 'void' &&
            IsOptionalString(schema.$id));
    }
    TypeGuard.TVoid = TVoid;
    /** Returns true if this value has a Readonly symbol */
    function TReadonly(schema) {
        return ValueGuard.IsObject(schema) && schema[exports.Readonly] === 'Readonly';
    }
    TypeGuard.TReadonly = TReadonly;
    /** Returns true if this value has a Optional symbol */
    function TOptional(schema) {
        return ValueGuard.IsObject(schema) && schema[exports.Optional] === 'Optional';
    }
    TypeGuard.TOptional = TOptional;
    /** Returns true if the given value is TSchema */
    function TSchema(schema) {
        // prettier-ignore
        return (ValueGuard.IsObject(schema)) && (TAny(schema) ||
            TArray(schema) ||
            TBoolean(schema) ||
            TBigInt(schema) ||
            TAsyncIterator(schema) ||
            TConstructor(schema) ||
            TDate(schema) ||
            TFunction(schema) ||
            TInteger(schema) ||
            TIntersect(schema) ||
            TIterator(schema) ||
            TLiteral(schema) ||
            TNever(schema) ||
            TNot(schema) ||
            TNull(schema) ||
            TNumber(schema) ||
            TObject(schema) ||
            TPromise(schema) ||
            TRecord(schema) ||
            TRef(schema) ||
            TString(schema) ||
            TSymbol(schema) ||
            TTemplateLiteral(schema) ||
            TThis(schema) ||
            TTuple(schema) ||
            TUndefined(schema) ||
            TUnion(schema) ||
            TUint8Array(schema) ||
            TUnknown(schema) ||
            TUnsafe(schema) ||
            TVoid(schema) ||
            (TKind(schema) && TypeRegistry.Has(schema[exports.Kind])));
    }
    TypeGuard.TSchema = TSchema;
})(TypeGuard || (exports.TypeGuard = TypeGuard = {}));
// --------------------------------------------------------------------------
// ExtendsUndefined
// --------------------------------------------------------------------------
/** Fast undefined check used for properties of type undefined */
var ExtendsUndefined;
(function (ExtendsUndefined) {
    function Check(schema) {
        return schema[exports.Kind] === 'Intersect'
            ? schema.allOf.every((schema) => Check(schema))
            : schema[exports.Kind] === 'Union'
                ? schema.anyOf.some((schema) => Check(schema))
                : schema[exports.Kind] === 'Undefined'
                    ? true
                    : schema[exports.Kind] === 'Not'
                        ? !Check(schema.not)
                        : false;
    }
    ExtendsUndefined.Check = Check;
})(ExtendsUndefined || (exports.ExtendsUndefined = ExtendsUndefined = {}));
// --------------------------------------------------------------------------
// TypeExtends
// --------------------------------------------------------------------------
class TypeExtendsError extends TypeBoxError {
}
exports.TypeExtendsError = TypeExtendsError;
var TypeExtendsResult;
(function (TypeExtendsResult) {
    TypeExtendsResult[TypeExtendsResult["Union"] = 0] = "Union";
    TypeExtendsResult[TypeExtendsResult["True"] = 1] = "True";
    TypeExtendsResult[TypeExtendsResult["False"] = 2] = "False";
})(TypeExtendsResult || (exports.TypeExtendsResult = TypeExtendsResult = {}));
var TypeExtends;
(function (TypeExtends) {
    // --------------------------------------------------------------------------
    // IntoBooleanResult
    // --------------------------------------------------------------------------
    function IntoBooleanResult(result) {
        return result === TypeExtendsResult.False ? result : TypeExtendsResult.True;
    }
    // --------------------------------------------------------------------------
    // Throw
    // --------------------------------------------------------------------------
    function Throw(message) {
        throw new TypeExtendsError(message);
    }
    // --------------------------------------------------------------------------
    // StructuralRight
    // --------------------------------------------------------------------------
    function IsStructuralRight(right) {
        // prettier-ignore
        return (TypeGuard.TNever(right) ||
            TypeGuard.TIntersect(right) ||
            TypeGuard.TUnion(right) ||
            TypeGuard.TUnknown(right) ||
            TypeGuard.TAny(right));
    }
    function StructuralRight(left, right) {
        // prettier-ignore
        return (TypeGuard.TNever(right) ? TNeverRight(left, right) :
            TypeGuard.TIntersect(right) ? TIntersectRight(left, right) :
                TypeGuard.TUnion(right) ? TUnionRight(left, right) :
                    TypeGuard.TUnknown(right) ? TUnknownRight(left, right) :
                        TypeGuard.TAny(right) ? TAnyRight(left, right) :
                            Throw('StructuralRight'));
    }
    // --------------------------------------------------------------------------
    // Any
    // --------------------------------------------------------------------------
    function TAnyRight(left, right) {
        return TypeExtendsResult.True;
    }
    function TAny(left, right) {
        // prettier-ignore
        return (TypeGuard.TIntersect(right) ? TIntersectRight(left, right) :
            (TypeGuard.TUnion(right) && right.anyOf.some((schema) => TypeGuard.TAny(schema) || TypeGuard.TUnknown(schema))) ? TypeExtendsResult.True :
                TypeGuard.TUnion(right) ? TypeExtendsResult.Union :
                    TypeGuard.TUnknown(right) ? TypeExtendsResult.True :
                        TypeGuard.TAny(right) ? TypeExtendsResult.True :
                            TypeExtendsResult.Union);
    }
    // --------------------------------------------------------------------------
    // Array
    // --------------------------------------------------------------------------
    function TArrayRight(left, right) {
        // prettier-ignore
        return (TypeGuard.TUnknown(left) ? TypeExtendsResult.False :
            TypeGuard.TAny(left) ? TypeExtendsResult.Union :
                TypeGuard.TNever(left) ? TypeExtendsResult.True :
                    TypeExtendsResult.False);
    }
    function TArray(left, right) {
        // prettier-ignore
        return (TypeGuard.TObject(right) && IsObjectArrayLike(right) ? TypeExtendsResult.True :
            IsStructuralRight(right) ? StructuralRight(left, right) :
                !TypeGuard.TArray(right) ? TypeExtendsResult.False :
                    IntoBooleanResult(Visit(left.items, right.items)));
    }
    // --------------------------------------------------------------------------
    // AsyncIterator
    // --------------------------------------------------------------------------
    function TAsyncIterator(left, right) {
        // prettier-ignore
        return (IsStructuralRight(right) ? StructuralRight(left, right) :
            !TypeGuard.TAsyncIterator(right) ? TypeExtendsResult.False :
                IntoBooleanResult(Visit(left.items, right.items)));
    }
    // --------------------------------------------------------------------------
    // BigInt
    // --------------------------------------------------------------------------
    function TBigInt(left, right) {
        // prettier-ignore
        return (IsStructuralRight(right) ? StructuralRight(left, right) :
            TypeGuard.TObject(right) ? TObjectRight(left, right) :
                TypeGuard.TRecord(right) ? TRecordRight(left, right) :
                    TypeGuard.TBigInt(right) ? TypeExtendsResult.True :
                        TypeExtendsResult.False);
    }
    // --------------------------------------------------------------------------
    // Boolean
    // --------------------------------------------------------------------------
    function TBooleanRight(left, right) {
        return TypeGuard.TLiteral(left) && ValueGuard.IsBoolean(left.const) ? TypeExtendsResult.True : TypeGuard.TBoolean(left) ? TypeExtendsResult.True : TypeExtendsResult.False;
    }
    function TBoolean(left, right) {
        // prettier-ignore
        return (IsStructuralRight(right) ? StructuralRight(left, right) :
            TypeGuard.TObject(right) ? TObjectRight(left, right) :
                TypeGuard.TRecord(right) ? TRecordRight(left, right) :
                    TypeGuard.TBoolean(right) ? TypeExtendsResult.True :
                        TypeExtendsResult.False);
    }
    // --------------------------------------------------------------------------
    // Constructor
    // --------------------------------------------------------------------------
    function TConstructor(left, right) {
        // prettier-ignore
        return (IsStructuralRight(right) ? StructuralRight(left, right) :
            TypeGuard.TObject(right) ? TObjectRight(left, right) :
                !TypeGuard.TConstructor(right) ? TypeExtendsResult.False :
                    left.parameters.length > right.parameters.length ? TypeExtendsResult.False :
                        (!left.parameters.every((schema, index) => IntoBooleanResult(Visit(right.parameters[index], schema)) === TypeExtendsResult.True)) ? TypeExtendsResult.False :
                            IntoBooleanResult(Visit(left.returns, right.returns)));
    }
    // --------------------------------------------------------------------------
    // Date
    // --------------------------------------------------------------------------
    function TDate(left, right) {
        // prettier-ignore
        return (IsStructuralRight(right) ? StructuralRight(left, right) :
            TypeGuard.TObject(right) ? TObjectRight(left, right) :
                TypeGuard.TRecord(right) ? TRecordRight(left, right) :
                    TypeGuard.TDate(right) ? TypeExtendsResult.True :
                        TypeExtendsResult.False);
    }
    // --------------------------------------------------------------------------
    // Function
    // --------------------------------------------------------------------------
    function TFunction(left, right) {
        // prettier-ignore
        return (IsStructuralRight(right) ? StructuralRight(left, right) :
            TypeGuard.TObject(right) ? TObjectRight(left, right) :
                !TypeGuard.TFunction(right) ? TypeExtendsResult.False :
                    left.parameters.length > right.parameters.length ? TypeExtendsResult.False :
                        (!left.parameters.every((schema, index) => IntoBooleanResult(Visit(right.parameters[index], schema)) === TypeExtendsResult.True)) ? TypeExtendsResult.False :
                            IntoBooleanResult(Visit(left.returns, right.returns)));
    }
    // --------------------------------------------------------------------------
    // Integer
    // --------------------------------------------------------------------------
    function TIntegerRight(left, right) {
        // prettier-ignore
        return (TypeGuard.TLiteral(left) && ValueGuard.IsNumber(left.const) ? TypeExtendsResult.True :
            TypeGuard.TNumber(left) || TypeGuard.TInteger(left) ? TypeExtendsResult.True :
                TypeExtendsResult.False);
    }
    function TInteger(left, right) {
        // prettier-ignore
        return (TypeGuard.TInteger(right) || TypeGuard.TNumber(right) ? TypeExtendsResult.True :
            IsStructuralRight(right) ? StructuralRight(left, right) :
                TypeGuard.TObject(right) ? TObjectRight(left, right) :
                    TypeGuard.TRecord(right) ? TRecordRight(left, right) :
                        TypeExtendsResult.False);
    }
    // --------------------------------------------------------------------------
    // Intersect
    // --------------------------------------------------------------------------
    function TIntersectRight(left, right) {
        // prettier-ignore
        return right.allOf.every((schema) => Visit(left, schema) === TypeExtendsResult.True)
            ? TypeExtendsResult.True
            : TypeExtendsResult.False;
    }
    function TIntersect(left, right) {
        // prettier-ignore
        return left.allOf.some((schema) => Visit(schema, right) === TypeExtendsResult.True)
            ? TypeExtendsResult.True
            : TypeExtendsResult.False;
    }
    // --------------------------------------------------------------------------
    // Iterator
    // --------------------------------------------------------------------------
    function TIterator(left, right) {
        // prettier-ignore
        return (IsStructuralRight(right) ? StructuralRight(left, right) :
            !TypeGuard.TIterator(right) ? TypeExtendsResult.False :
                IntoBooleanResult(Visit(left.items, right.items)));
    }
    // --------------------------------------------------------------------------
    // Literal
    // --------------------------------------------------------------------------
    function TLiteral(left, right) {
        // prettier-ignore
        return (TypeGuard.TLiteral(right) && right.const === left.const ? TypeExtendsResult.True :
            IsStructuralRight(right) ? StructuralRight(left, right) :
                TypeGuard.TObject(right) ? TObjectRight(left, right) :
                    TypeGuard.TRecord(right) ? TRecordRight(left, right) :
                        TypeGuard.TString(right) ? TStringRight(left, right) :
                            TypeGuard.TNumber(right) ? TNumberRight(left, right) :
                                TypeGuard.TInteger(right) ? TIntegerRight(left, right) :
                                    TypeGuard.TBoolean(right) ? TBooleanRight(left, right) :
                                        TypeExtendsResult.False);
    }
    // --------------------------------------------------------------------------
    // Never
    // --------------------------------------------------------------------------
    function TNeverRight(left, right) {
        return TypeExtendsResult.False;
    }
    function TNever(left, right) {
        return TypeExtendsResult.True;
    }
    // --------------------------------------------------------------------------
    // Not
    // --------------------------------------------------------------------------
    function UnwrapTNot(schema) {
        let [current, depth] = [schema, 0];
        while (true) {
            if (!TypeGuard.TNot(current))
                break;
            current = current.not;
            depth += 1;
        }
        return depth % 2 === 0 ? current : exports.Type.Unknown();
    }
    function TNot(left, right) {
        // TypeScript has no concept of negated types, and attempts to correctly check the negated
        // type at runtime would put TypeBox at odds with TypeScripts ability to statically infer
        // the type. Instead we unwrap to either unknown or T and continue evaluating.
        // prettier-ignore
        return (TypeGuard.TNot(left) ? Visit(UnwrapTNot(left), right) :
            TypeGuard.TNot(right) ? Visit(left, UnwrapTNot(right)) :
                Throw('Invalid fallthrough for Not'));
    }
    // --------------------------------------------------------------------------
    // Null
    // --------------------------------------------------------------------------
    function TNull(left, right) {
        // prettier-ignore
        return (IsStructuralRight(right) ? StructuralRight(left, right) :
            TypeGuard.TObject(right) ? TObjectRight(left, right) :
                TypeGuard.TRecord(right) ? TRecordRight(left, right) :
                    TypeGuard.TNull(right) ? TypeExtendsResult.True :
                        TypeExtendsResult.False);
    }
    // --------------------------------------------------------------------------
    // Number
    // --------------------------------------------------------------------------
    function TNumberRight(left, right) {
        // prettier-ignore
        return (TypeGuard.TLiteralNumber(left) ? TypeExtendsResult.True :
            TypeGuard.TNumber(left) || TypeGuard.TInteger(left) ? TypeExtendsResult.True :
                TypeExtendsResult.False);
    }
    function TNumber(left, right) {
        // prettier-ignore
        return (IsStructuralRight(right) ? StructuralRight(left, right) :
            TypeGuard.TObject(right) ? TObjectRight(left, right) :
                TypeGuard.TRecord(right) ? TRecordRight(left, right) :
                    TypeGuard.TInteger(right) || TypeGuard.TNumber(right) ? TypeExtendsResult.True :
                        TypeExtendsResult.False);
    }
    // --------------------------------------------------------------------------
    // Object
    // --------------------------------------------------------------------------
    function IsObjectPropertyCount(schema, count) {
        return Object.getOwnPropertyNames(schema.properties).length === count;
    }
    function IsObjectStringLike(schema) {
        return IsObjectArrayLike(schema);
    }
    function IsObjectSymbolLike(schema) {
        // prettier-ignore
        return IsObjectPropertyCount(schema, 0) || (IsObjectPropertyCount(schema, 1) && 'description' in schema.properties && TypeGuard.TUnion(schema.properties.description) && schema.properties.description.anyOf.length === 2 && ((TypeGuard.TString(schema.properties.description.anyOf[0]) &&
            TypeGuard.TUndefined(schema.properties.description.anyOf[1])) || (TypeGuard.TString(schema.properties.description.anyOf[1]) &&
            TypeGuard.TUndefined(schema.properties.description.anyOf[0]))));
    }
    function IsObjectNumberLike(schema) {
        return IsObjectPropertyCount(schema, 0);
    }
    function IsObjectBooleanLike(schema) {
        return IsObjectPropertyCount(schema, 0);
    }
    function IsObjectBigIntLike(schema) {
        return IsObjectPropertyCount(schema, 0);
    }
    function IsObjectDateLike(schema) {
        return IsObjectPropertyCount(schema, 0);
    }
    function IsObjectUint8ArrayLike(schema) {
        return IsObjectArrayLike(schema);
    }
    function IsObjectFunctionLike(schema) {
        const length = exports.Type.Number();
        return IsObjectPropertyCount(schema, 0) || (IsObjectPropertyCount(schema, 1) && 'length' in schema.properties && IntoBooleanResult(Visit(schema.properties['length'], length)) === TypeExtendsResult.True);
    }
    function IsObjectConstructorLike(schema) {
        return IsObjectPropertyCount(schema, 0);
    }
    function IsObjectArrayLike(schema) {
        const length = exports.Type.Number();
        return IsObjectPropertyCount(schema, 0) || (IsObjectPropertyCount(schema, 1) && 'length' in schema.properties && IntoBooleanResult(Visit(schema.properties['length'], length)) === TypeExtendsResult.True);
    }
    function IsObjectPromiseLike(schema) {
        const then = exports.Type.Function([exports.Type.Any()], exports.Type.Any());
        return IsObjectPropertyCount(schema, 0) || (IsObjectPropertyCount(schema, 1) && 'then' in schema.properties && IntoBooleanResult(Visit(schema.properties['then'], then)) === TypeExtendsResult.True);
    }
    // --------------------------------------------------------------------------
    // Property
    // --------------------------------------------------------------------------
    function Property(left, right) {
        // prettier-ignore
        return (Visit(left, right) === TypeExtendsResult.False ? TypeExtendsResult.False :
            TypeGuard.TOptional(left) && !TypeGuard.TOptional(right) ? TypeExtendsResult.False :
                TypeExtendsResult.True);
    }
    function TObjectRight(left, right) {
        // prettier-ignore
        return (TypeGuard.TUnknown(left) ? TypeExtendsResult.False :
            TypeGuard.TAny(left) ? TypeExtendsResult.Union : (TypeGuard.TNever(left) ||
                (TypeGuard.TLiteralString(left) && IsObjectStringLike(right)) ||
                (TypeGuard.TLiteralNumber(left) && IsObjectNumberLike(right)) ||
                (TypeGuard.TLiteralBoolean(left) && IsObjectBooleanLike(right)) ||
                (TypeGuard.TSymbol(left) && IsObjectSymbolLike(right)) ||
                (TypeGuard.TBigInt(left) && IsObjectBigIntLike(right)) ||
                (TypeGuard.TString(left) && IsObjectStringLike(right)) ||
                (TypeGuard.TSymbol(left) && IsObjectSymbolLike(right)) ||
                (TypeGuard.TNumber(left) && IsObjectNumberLike(right)) ||
                (TypeGuard.TInteger(left) && IsObjectNumberLike(right)) ||
                (TypeGuard.TBoolean(left) && IsObjectBooleanLike(right)) ||
                (TypeGuard.TUint8Array(left) && IsObjectUint8ArrayLike(right)) ||
                (TypeGuard.TDate(left) && IsObjectDateLike(right)) ||
                (TypeGuard.TConstructor(left) && IsObjectConstructorLike(right)) ||
                (TypeGuard.TFunction(left) && IsObjectFunctionLike(right))) ? TypeExtendsResult.True :
                (TypeGuard.TRecord(left) && TypeGuard.TString(RecordKey(left))) ? (() => {
                    // When expressing a Record with literal key values, the Record is converted into a Object with
                    // the Hint assigned as `Record`. This is used to invert the extends logic.
                    return right[exports.Hint] === 'Record' ? TypeExtendsResult.True : TypeExtendsResult.False;
                })() :
                    (TypeGuard.TRecord(left) && TypeGuard.TNumber(RecordKey(left))) ? (() => {
                        return IsObjectPropertyCount(right, 0)
                            ? TypeExtendsResult.True
                            : TypeExtendsResult.False;
                    })() :
                        TypeExtendsResult.False);
    }
    function TObject(left, right) {
        // prettier-ignore
        return (IsStructuralRight(right) ? StructuralRight(left, right) :
            TypeGuard.TRecord(right) ? TRecordRight(left, right) :
                !TypeGuard.TObject(right) ? TypeExtendsResult.False :
                    (() => {
                        for (const key of Object.getOwnPropertyNames(right.properties)) {
                            if (!(key in left.properties) && !TypeGuard.TOptional(right.properties[key])) {
                                return TypeExtendsResult.False;
                            }
                            if (TypeGuard.TOptional(right.properties[key])) {
                                return TypeExtendsResult.True;
                            }
                            if (Property(left.properties[key], right.properties[key]) === TypeExtendsResult.False) {
                                return TypeExtendsResult.False;
                            }
                        }
                        return TypeExtendsResult.True;
                    })());
    }
    // --------------------------------------------------------------------------
    // Promise
    // --------------------------------------------------------------------------
    function TPromise(left, right) {
        // prettier-ignore
        return (IsStructuralRight(right) ? StructuralRight(left, right) :
            TypeGuard.TObject(right) && IsObjectPromiseLike(right) ? TypeExtendsResult.True :
                !TypeGuard.TPromise(right) ? TypeExtendsResult.False :
                    IntoBooleanResult(Visit(left.item, right.item)));
    }
    // --------------------------------------------------------------------------
    // Record
    // --------------------------------------------------------------------------
    function RecordKey(schema) {
        // prettier-ignore
        return (exports.PatternNumberExact in schema.patternProperties ? exports.Type.Number() :
            exports.PatternStringExact in schema.patternProperties ? exports.Type.String() :
                Throw('Unknown record key pattern'));
    }
    function RecordValue(schema) {
        // prettier-ignore
        return (exports.PatternNumberExact in schema.patternProperties ? schema.patternProperties[exports.PatternNumberExact] :
            exports.PatternStringExact in schema.patternProperties ? schema.patternProperties[exports.PatternStringExact] :
                Throw('Unable to get record value schema'));
    }
    function TRecordRight(left, right) {
        const [Key, Value] = [RecordKey(right), RecordValue(right)];
        // prettier-ignore
        return ((TypeGuard.TLiteralString(left) && TypeGuard.TNumber(Key) && IntoBooleanResult(Visit(left, Value)) === TypeExtendsResult.True) ? TypeExtendsResult.True :
            TypeGuard.TUint8Array(left) && TypeGuard.TNumber(Key) ? Visit(left, Value) :
                TypeGuard.TString(left) && TypeGuard.TNumber(Key) ? Visit(left, Value) :
                    TypeGuard.TArray(left) && TypeGuard.TNumber(Key) ? Visit(left, Value) :
                        TypeGuard.TObject(left) ? (() => {
                            for (const key of Object.getOwnPropertyNames(left.properties)) {
                                if (Property(Value, left.properties[key]) === TypeExtendsResult.False) {
                                    return TypeExtendsResult.False;
                                }
                            }
                            return TypeExtendsResult.True;
                        })() :
                            TypeExtendsResult.False);
    }
    function TRecord(left, right) {
        // prettier-ignore
        return (IsStructuralRight(right) ? StructuralRight(left, right) :
            TypeGuard.TObject(right) ? TObjectRight(left, right) :
                !TypeGuard.TRecord(right) ? TypeExtendsResult.False :
                    Visit(RecordValue(left), RecordValue(right)));
    }
    // --------------------------------------------------------------------------
    // String
    // --------------------------------------------------------------------------
    function TStringRight(left, right) {
        // prettier-ignore
        return (TypeGuard.TLiteral(left) && ValueGuard.IsString(left.const) ? TypeExtendsResult.True :
            TypeGuard.TString(left) ? TypeExtendsResult.True :
                TypeExtendsResult.False);
    }
    function TString(left, right) {
        // prettier-ignore
        return (IsStructuralRight(right) ? StructuralRight(left, right) :
            TypeGuard.TObject(right) ? TObjectRight(left, right) :
                TypeGuard.TRecord(right) ? TRecordRight(left, right) :
                    TypeGuard.TString(right) ? TypeExtendsResult.True :
                        TypeExtendsResult.False);
    }
    // --------------------------------------------------------------------------
    // Symbol
    // --------------------------------------------------------------------------
    function TSymbol(left, right) {
        // prettier-ignore
        return (IsStructuralRight(right) ? StructuralRight(left, right) :
            TypeGuard.TObject(right) ? TObjectRight(left, right) :
                TypeGuard.TRecord(right) ? TRecordRight(left, right) :
                    TypeGuard.TSymbol(right) ? TypeExtendsResult.True :
                        TypeExtendsResult.False);
    }
    // --------------------------------------------------------------------------
    // TemplateLiteral
    // --------------------------------------------------------------------------
    function TTemplateLiteral(left, right) {
        // TemplateLiteral types are resolved to either unions for finite expressions or string
        // for infinite expressions. Here we call to TemplateLiteralResolver to resolve for
        // either type and continue evaluating.
        // prettier-ignore
        return (TypeGuard.TTemplateLiteral(left) ? Visit(TemplateLiteralResolver.Resolve(left), right) :
            TypeGuard.TTemplateLiteral(right) ? Visit(left, TemplateLiteralResolver.Resolve(right)) :
                Throw('Invalid fallthrough for TemplateLiteral'));
    }
    // --------------------------------------------------------------------------
    // Tuple
    // --------------------------------------------------------------------------
    function IsArrayOfTuple(left, right) {
        // prettier-ignore
        return (TypeGuard.TArray(right) &&
            left.items !== undefined &&
            left.items.every((schema) => Visit(schema, right.items) === TypeExtendsResult.True));
    }
    function TTupleRight(left, right) {
        // prettier-ignore
        return (TypeGuard.TNever(left) ? TypeExtendsResult.True :
            TypeGuard.TUnknown(left) ? TypeExtendsResult.False :
                TypeGuard.TAny(left) ? TypeExtendsResult.Union :
                    TypeExtendsResult.False);
    }
    function TTuple(left, right) {
        // prettier-ignore
        return (IsStructuralRight(right) ? StructuralRight(left, right) :
            TypeGuard.TObject(right) && IsObjectArrayLike(right) ? TypeExtendsResult.True :
                TypeGuard.TArray(right) && IsArrayOfTuple(left, right) ? TypeExtendsResult.True :
                    !TypeGuard.TTuple(right) ? TypeExtendsResult.False :
                        (ValueGuard.IsUndefined(left.items) && !ValueGuard.IsUndefined(right.items)) || (!ValueGuard.IsUndefined(left.items) && ValueGuard.IsUndefined(right.items)) ? TypeExtendsResult.False :
                            (ValueGuard.IsUndefined(left.items) && !ValueGuard.IsUndefined(right.items)) ? TypeExtendsResult.True :
                                left.items.every((schema, index) => Visit(schema, right.items[index]) === TypeExtendsResult.True) ? TypeExtendsResult.True :
                                    TypeExtendsResult.False);
    }
    // --------------------------------------------------------------------------
    // Uint8Array
    // --------------------------------------------------------------------------
    function TUint8Array(left, right) {
        // prettier-ignore
        return (IsStructuralRight(right) ? StructuralRight(left, right) :
            TypeGuard.TObject(right) ? TObjectRight(left, right) :
                TypeGuard.TRecord(right) ? TRecordRight(left, right) :
                    TypeGuard.TUint8Array(right) ? TypeExtendsResult.True :
                        TypeExtendsResult.False);
    }
    // --------------------------------------------------------------------------
    // Undefined
    // --------------------------------------------------------------------------
    function TUndefined(left, right) {
        // prettier-ignore
        return (IsStructuralRight(right) ? StructuralRight(left, right) :
            TypeGuard.TObject(right) ? TObjectRight(left, right) :
                TypeGuard.TRecord(right) ? TRecordRight(left, right) :
                    TypeGuard.TVoid(right) ? VoidRight(left, right) :
                        TypeGuard.TUndefined(right) ? TypeExtendsResult.True :
                            TypeExtendsResult.False);
    }
    // --------------------------------------------------------------------------
    // Union
    // --------------------------------------------------------------------------
    function TUnionRight(left, right) {
        // prettier-ignore
        return right.anyOf.some((schema) => Visit(left, schema) === TypeExtendsResult.True)
            ? TypeExtendsResult.True
            : TypeExtendsResult.False;
    }
    function TUnion(left, right) {
        // prettier-ignore
        return left.anyOf.every((schema) => Visit(schema, right) === TypeExtendsResult.True)
            ? TypeExtendsResult.True
            : TypeExtendsResult.False;
    }
    // --------------------------------------------------------------------------
    // Unknown
    // --------------------------------------------------------------------------
    function TUnknownRight(left, right) {
        return TypeExtendsResult.True;
    }
    function TUnknown(left, right) {
        // prettier-ignore
        return (TypeGuard.TNever(right) ? TNeverRight(left, right) :
            TypeGuard.TIntersect(right) ? TIntersectRight(left, right) :
                TypeGuard.TUnion(right) ? TUnionRight(left, right) :
                    TypeGuard.TAny(right) ? TAnyRight(left, right) :
                        TypeGuard.TString(right) ? TStringRight(left, right) :
                            TypeGuard.TNumber(right) ? TNumberRight(left, right) :
                                TypeGuard.TInteger(right) ? TIntegerRight(left, right) :
                                    TypeGuard.TBoolean(right) ? TBooleanRight(left, right) :
                                        TypeGuard.TArray(right) ? TArrayRight(left, right) :
                                            TypeGuard.TTuple(right) ? TTupleRight(left, right) :
                                                TypeGuard.TObject(right) ? TObjectRight(left, right) :
                                                    TypeGuard.TUnknown(right) ? TypeExtendsResult.True :
                                                        TypeExtendsResult.False);
    }
    // --------------------------------------------------------------------------
    // Void
    // --------------------------------------------------------------------------
    function VoidRight(left, right) {
        // prettier-ignore
        return TypeGuard.TUndefined(left) ? TypeExtendsResult.True :
            TypeGuard.TUndefined(left) ? TypeExtendsResult.True :
                TypeExtendsResult.False;
    }
    function TVoid(left, right) {
        // prettier-ignore
        return TypeGuard.TIntersect(right) ? TIntersectRight(left, right) :
            TypeGuard.TUnion(right) ? TUnionRight(left, right) :
                TypeGuard.TUnknown(right) ? TUnknownRight(left, right) :
                    TypeGuard.TAny(right) ? TAnyRight(left, right) :
                        TypeGuard.TObject(right) ? TObjectRight(left, right) :
                            TypeGuard.TVoid(right) ? TypeExtendsResult.True :
                                TypeExtendsResult.False;
    }
    function Visit(left, right) {
        // prettier-ignore
        return (
        // resolvable
        (TypeGuard.TTemplateLiteral(left) || TypeGuard.TTemplateLiteral(right)) ? TTemplateLiteral(left, right) :
            (TypeGuard.TNot(left) || TypeGuard.TNot(right)) ? TNot(left, right) :
                // standard
                TypeGuard.TAny(left) ? TAny(left, right) :
                    TypeGuard.TArray(left) ? TArray(left, right) :
                        TypeGuard.TBigInt(left) ? TBigInt(left, right) :
                            TypeGuard.TBoolean(left) ? TBoolean(left, right) :
                                TypeGuard.TAsyncIterator(left) ? TAsyncIterator(left, right) :
                                    TypeGuard.TConstructor(left) ? TConstructor(left, right) :
                                        TypeGuard.TDate(left) ? TDate(left, right) :
                                            TypeGuard.TFunction(left) ? TFunction(left, right) :
                                                TypeGuard.TInteger(left) ? TInteger(left, right) :
                                                    TypeGuard.TIntersect(left) ? TIntersect(left, right) :
                                                        TypeGuard.TIterator(left) ? TIterator(left, right) :
                                                            TypeGuard.TLiteral(left) ? TLiteral(left, right) :
                                                                TypeGuard.TNever(left) ? TNever(left, right) :
                                                                    TypeGuard.TNull(left) ? TNull(left, right) :
                                                                        TypeGuard.TNumber(left) ? TNumber(left, right) :
                                                                            TypeGuard.TObject(left) ? TObject(left, right) :
                                                                                TypeGuard.TRecord(left) ? TRecord(left, right) :
                                                                                    TypeGuard.TString(left) ? TString(left, right) :
                                                                                        TypeGuard.TSymbol(left) ? TSymbol(left, right) :
                                                                                            TypeGuard.TTuple(left) ? TTuple(left, right) :
                                                                                                TypeGuard.TPromise(left) ? TPromise(left, right) :
                                                                                                    TypeGuard.TUint8Array(left) ? TUint8Array(left, right) :
                                                                                                        TypeGuard.TUndefined(left) ? TUndefined(left, right) :
                                                                                                            TypeGuard.TUnion(left) ? TUnion(left, right) :
                                                                                                                TypeGuard.TUnknown(left) ? TUnknown(left, right) :
                                                                                                                    TypeGuard.TVoid(left) ? TVoid(left, right) :
                                                                                                                        Throw(`Unknown left type operand '${left[exports.Kind]}'`));
    }
    function Extends(left, right) {
        return Visit(left, right);
    }
    TypeExtends.Extends = Extends;
})(TypeExtends || (exports.TypeExtends = TypeExtends = {}));
// --------------------------------------------------------------------------
// TypeClone
// --------------------------------------------------------------------------
/** Specialized Clone for Types */
var TypeClone;
(function (TypeClone) {
    function ArrayType(value) {
        return value.map((value) => Visit(value));
    }
    function DateType(value) {
        return new Date(value.getTime());
    }
    function Uint8ArrayType(value) {
        return new Uint8Array(value);
    }
    function ObjectType(value) {
        const clonedProperties = Object.getOwnPropertyNames(value).reduce((acc, key) => ({ ...acc, [key]: Visit(value[key]) }), {});
        const clonedSymbols = Object.getOwnPropertySymbols(value).reduce((acc, key) => ({ ...acc, [key]: Visit(value[key]) }), {});
        return { ...clonedProperties, ...clonedSymbols };
    }
    function Visit(value) {
        // prettier-ignore
        return (ValueGuard.IsArray(value) ? ArrayType(value) :
            ValueGuard.IsDate(value) ? DateType(value) :
                ValueGuard.IsUint8Array(value) ? Uint8ArrayType(value) :
                    ValueGuard.IsObject(value) ? ObjectType(value) :
                        value);
    }
    /** Clones a Rest */
    function Rest(schemas) {
        return schemas.map((schema) => Type(schema));
    }
    TypeClone.Rest = Rest;
    /** Clones a Type */
    function Type(schema, options = {}) {
        return { ...Visit(schema), ...options };
    }
    TypeClone.Type = Type;
})(TypeClone || (exports.TypeClone = TypeClone = {}));
// --------------------------------------------------------------------------
// IndexedAccessor
// --------------------------------------------------------------------------
var IndexedAccessor;
(function (IndexedAccessor) {
    function OptionalUnwrap(schema) {
        return schema.map((schema) => {
            const { [exports.Optional]: _, ...clone } = TypeClone.Type(schema);
            return clone;
        });
    }
    function IsIntersectOptional(schema) {
        return schema.every((schema) => TypeGuard.TOptional(schema));
    }
    function IsUnionOptional(schema) {
        return schema.some((schema) => TypeGuard.TOptional(schema));
    }
    function ResolveIntersect(schema) {
        return IsIntersectOptional(schema.allOf) ? exports.Type.Optional(exports.Type.Intersect(OptionalUnwrap(schema.allOf))) : schema;
    }
    function ResolveUnion(schema) {
        return IsUnionOptional(schema.anyOf) ? exports.Type.Optional(exports.Type.Union(OptionalUnwrap(schema.anyOf))) : schema;
    }
    function ResolveOptional(schema) {
        // prettier-ignore
        return schema[exports.Kind] === 'Intersect' ? ResolveIntersect(schema) :
            schema[exports.Kind] === 'Union' ? ResolveUnion(schema) :
                schema;
    }
    function TIntersect(schema, key) {
        const resolved = schema.allOf.reduce((acc, schema) => {
            const indexed = Visit(schema, key);
            return indexed[exports.Kind] === 'Never' ? acc : [...acc, indexed];
        }, []);
        return ResolveOptional(exports.Type.Intersect(resolved));
    }
    function TUnion(schema, key) {
        const resolved = schema.anyOf.map((schema) => Visit(schema, key));
        return ResolveOptional(exports.Type.Union(resolved));
    }
    function TObject(schema, key) {
        const property = schema.properties[key];
        return ValueGuard.IsUndefined(property) ? exports.Type.Never() : exports.Type.Union([property]);
    }
    function TTuple(schema, key) {
        const items = schema.items;
        if (ValueGuard.IsUndefined(items))
            return exports.Type.Never();
        const element = items[key]; //
        if (ValueGuard.IsUndefined(element))
            return exports.Type.Never();
        return element;
    }
    function Visit(schema, key) {
        // prettier-ignore
        return schema[exports.Kind] === 'Intersect' ? TIntersect(schema, key) :
            schema[exports.Kind] === 'Union' ? TUnion(schema, key) :
                schema[exports.Kind] === 'Object' ? TObject(schema, key) :
                    schema[exports.Kind] === 'Tuple' ? TTuple(schema, key) :
                        exports.Type.Never();
    }
    function Resolve(schema, keys, options = {}) {
        const resolved = keys.map((key) => Visit(schema, key.toString()));
        return ResolveOptional(exports.Type.Union(resolved, options));
    }
    IndexedAccessor.Resolve = Resolve;
})(IndexedAccessor || (exports.IndexedAccessor = IndexedAccessor = {}));
// --------------------------------------------------------------------------
// Intrinsic
// --------------------------------------------------------------------------
var Intrinsic;
(function (Intrinsic) {
    function Uncapitalize(value) {
        const [first, rest] = [value.slice(0, 1), value.slice(1)];
        return `${first.toLowerCase()}${rest}`;
    }
    function Capitalize(value) {
        const [first, rest] = [value.slice(0, 1), value.slice(1)];
        return `${first.toUpperCase()}${rest}`;
    }
    function Uppercase(value) {
        return value.toUpperCase();
    }
    function Lowercase(value) {
        return value.toLowerCase();
    }
    function IntrinsicTemplateLiteral(schema, mode) {
        // note: template literals require special runtime handling as they are encoded in string patterns.
        // This diverges from the mapped type which would otherwise map on the template literal kind.
        const expression = TemplateLiteralParser.ParseExact(schema.pattern);
        const finite = TemplateLiteralFinite.Check(expression);
        if (!finite)
            return { ...schema, pattern: IntrinsicLiteral(schema.pattern, mode) };
        const strings = [...TemplateLiteralGenerator.Generate(expression)];
        const literals = strings.map((value) => exports.Type.Literal(value));
        const mapped = IntrinsicRest(literals, mode);
        const union = exports.Type.Union(mapped);
        return exports.Type.TemplateLiteral([union]);
    }
    function IntrinsicLiteral(value, mode) {
        // prettier-ignore
        return typeof value === 'string' ? (mode === 'Uncapitalize' ? Uncapitalize(value) :
            mode === 'Capitalize' ? Capitalize(value) :
                mode === 'Uppercase' ? Uppercase(value) :
                    mode === 'Lowercase' ? Lowercase(value) :
                        value) : value.toString();
    }
    function IntrinsicRest(schema, mode) {
        if (schema.length === 0)
            return [];
        const [L, ...R] = schema;
        return [Map(L, mode), ...IntrinsicRest(R, mode)];
    }
    function Visit(schema, mode) {
        // prettier-ignore
        return TypeGuard.TTemplateLiteral(schema) ? IntrinsicTemplateLiteral(schema, mode) :
            TypeGuard.TUnion(schema) ? exports.Type.Union(IntrinsicRest(schema.anyOf, mode)) :
                TypeGuard.TLiteral(schema) ? exports.Type.Literal(IntrinsicLiteral(schema.const, mode)) :
                    schema;
    }
    /** Applies an intrinsic string manipulation to the given type. */
    function Map(schema, mode) {
        return Visit(schema, mode);
    }
    Intrinsic.Map = Map;
})(Intrinsic || (exports.Intrinsic = Intrinsic = {}));
// --------------------------------------------------------------------------
// ObjectMap
// --------------------------------------------------------------------------
var ObjectMap;
(function (ObjectMap) {
    function TIntersect(schema, callback) {
        // prettier-ignore
        return exports.Type.Intersect(schema.allOf.map((inner) => Visit(inner, callback)), { ...schema });
    }
    function TUnion(schema, callback) {
        // prettier-ignore
        return exports.Type.Union(schema.anyOf.map((inner) => Visit(inner, callback)), { ...schema });
    }
    function TObject(schema, callback) {
        return callback(schema);
    }
    function Visit(schema, callback) {
        // There are cases where users need to map objects with unregistered kinds. Using a TypeGuard here would
        // prevent sub schema mapping as unregistered kinds will not pass TSchema checks. This is notable in the
        // case of TObject where unregistered property kinds cause the TObject check to fail. As mapping is only
        // used for composition, we use explicit checks instead.
        // prettier-ignore
        return (schema[exports.Kind] === 'Intersect' ? TIntersect(schema, callback) :
            schema[exports.Kind] === 'Union' ? TUnion(schema, callback) :
                schema[exports.Kind] === 'Object' ? TObject(schema, callback) :
                    schema);
    }
    function Map(schema, callback, options) {
        return { ...Visit(TypeClone.Type(schema), callback), ...options };
    }
    ObjectMap.Map = Map;
})(ObjectMap || (exports.ObjectMap = ObjectMap = {}));
var KeyResolver;
(function (KeyResolver) {
    function UnwrapPattern(key) {
        return key[0] === '^' && key[key.length - 1] === '$' ? key.slice(1, key.length - 1) : key;
    }
    function TIntersect(schema, options) {
        return schema.allOf.reduce((acc, schema) => [...acc, ...Visit(schema, options)], []);
    }
    function TUnion(schema, options) {
        const sets = schema.anyOf.map((inner) => Visit(inner, options));
        return [...sets.reduce((set, outer) => outer.map((key) => (sets.every((inner) => inner.includes(key)) ? set.add(key) : set))[0], new Set())];
    }
    function TObject(schema, options) {
        return Object.getOwnPropertyNames(schema.properties);
    }
    function TRecord(schema, options) {
        return options.includePatterns ? Object.getOwnPropertyNames(schema.patternProperties) : [];
    }
    function Visit(schema, options) {
        // prettier-ignore
        return (TypeGuard.TIntersect(schema) ? TIntersect(schema, options) :
            TypeGuard.TUnion(schema) ? TUnion(schema, options) :
                TypeGuard.TObject(schema) ? TObject(schema, options) :
                    TypeGuard.TRecord(schema) ? TRecord(schema, options) :
                        []);
    }
    /** Resolves an array of keys in this schema */
    function ResolveKeys(schema, options) {
        return [...new Set(Visit(schema, options))];
    }
    KeyResolver.ResolveKeys = ResolveKeys;
    /** Resolves a regular expression pattern matching all keys in this schema */
    function ResolvePattern(schema) {
        const keys = ResolveKeys(schema, { includePatterns: true });
        const pattern = keys.map((key) => `(${UnwrapPattern(key)})`);
        return `^(${pattern.join('|')})$`;
    }
    KeyResolver.ResolvePattern = ResolvePattern;
})(KeyResolver || (exports.KeyResolver = KeyResolver = {}));
// --------------------------------------------------------------------------
// KeyArrayResolver
// --------------------------------------------------------------------------
class KeyArrayResolverError extends TypeBoxError {
}
exports.KeyArrayResolverError = KeyArrayResolverError;
var KeyArrayResolver;
(function (KeyArrayResolver) {
    /** Resolves an array of string[] keys from the given schema or array type. */
    function Resolve(schema) {
        // prettier-ignore
        return Array.isArray(schema) ? schema :
            TypeGuard.TUnionLiteral(schema) ? schema.anyOf.map((schema) => schema.const.toString()) :
                TypeGuard.TLiteral(schema) ? [schema.const] :
                    TypeGuard.TTemplateLiteral(schema) ? (() => {
                        const expression = TemplateLiteralParser.ParseExact(schema.pattern);
                        if (!TemplateLiteralFinite.Check(expression))
                            throw new KeyArrayResolverError('Cannot resolve keys from infinite template expression');
                        return [...TemplateLiteralGenerator.Generate(expression)];
                    })() : [];
    }
    KeyArrayResolver.Resolve = Resolve;
})(KeyArrayResolver || (exports.KeyArrayResolver = KeyArrayResolver = {}));
// --------------------------------------------------------------------------
// UnionResolver
// --------------------------------------------------------------------------
var UnionResolver;
(function (UnionResolver) {
    function* TUnion(union) {
        for (const schema of union.anyOf) {
            if (schema[exports.Kind] === 'Union') {
                yield* TUnion(schema);
            }
            else {
                yield schema;
            }
        }
    }
    /** Returns a resolved union with interior unions flattened */
    function Resolve(union) {
        return exports.Type.Union([...TUnion(union)], { ...union });
    }
    UnionResolver.Resolve = Resolve;
})(UnionResolver || (exports.UnionResolver = UnionResolver = {}));
// --------------------------------------------------------------------------
// TemplateLiteralPattern
// --------------------------------------------------------------------------
class TemplateLiteralPatternError extends TypeBoxError {
}
exports.TemplateLiteralPatternError = TemplateLiteralPatternError;
var TemplateLiteralPattern;
(function (TemplateLiteralPattern) {
    function Throw(message) {
        throw new TemplateLiteralPatternError(message);
    }
    function Escape(value) {
        return value.replace(/[.*+?^${}()|[\]\\]/g, '\\$&');
    }
    function Visit(schema, acc) {
        // prettier-ignore
        return (TypeGuard.TTemplateLiteral(schema) ? schema.pattern.slice(1, schema.pattern.length - 1) :
            TypeGuard.TUnion(schema) ? `(${schema.anyOf.map((schema) => Visit(schema, acc)).join('|')})` :
                TypeGuard.TNumber(schema) ? `${acc}${exports.PatternNumber}` :
                    TypeGuard.TInteger(schema) ? `${acc}${exports.PatternNumber}` :
                        TypeGuard.TBigInt(schema) ? `${acc}${exports.PatternNumber}` :
                            TypeGuard.TString(schema) ? `${acc}${exports.PatternString}` :
                                TypeGuard.TLiteral(schema) ? `${acc}${Escape(schema.const.toString())}` :
                                    TypeGuard.TBoolean(schema) ? `${acc}${exports.PatternBoolean}` :
                                        Throw(`Unexpected Kind '${schema[exports.Kind]}'`));
    }
    function Create(kinds) {
        return `^${kinds.map((schema) => Visit(schema, '')).join('')}\$`;
    }
    TemplateLiteralPattern.Create = Create;
})(TemplateLiteralPattern || (exports.TemplateLiteralPattern = TemplateLiteralPattern = {}));
// --------------------------------------------------------------------------------------
// TemplateLiteralResolver
// --------------------------------------------------------------------------------------
var TemplateLiteralResolver;
(function (TemplateLiteralResolver) {
    /** Resolves a template literal as a TUnion */
    function Resolve(template) {
        const expression = TemplateLiteralParser.ParseExact(template.pattern);
        if (!TemplateLiteralFinite.Check(expression))
            return exports.Type.String();
        const literals = [...TemplateLiteralGenerator.Generate(expression)].map((value) => exports.Type.Literal(value));
        return exports.Type.Union(literals);
    }
    TemplateLiteralResolver.Resolve = Resolve;
})(TemplateLiteralResolver || (exports.TemplateLiteralResolver = TemplateLiteralResolver = {}));
// --------------------------------------------------------------------------------------
// TemplateLiteralParser
// --------------------------------------------------------------------------------------
class TemplateLiteralParserError extends TypeBoxError {
}
exports.TemplateLiteralParserError = TemplateLiteralParserError;
var TemplateLiteralParser;
(function (TemplateLiteralParser) {
    function IsNonEscaped(pattern, index, char) {
        return pattern[index] === char && pattern.charCodeAt(index - 1) !== 92;
    }
    function IsOpenParen(pattern, index) {
        return IsNonEscaped(pattern, index, '(');
    }
    function IsCloseParen(pattern, index) {
        return IsNonEscaped(pattern, index, ')');
    }
    function IsSeparator(pattern, index) {
        return IsNonEscaped(pattern, index, '|');
    }
    function IsGroup(pattern) {
        if (!(IsOpenParen(pattern, 0) && IsCloseParen(pattern, pattern.length - 1)))
            return false;
        let count = 0;
        for (let index = 0; index < pattern.length; index++) {
            if (IsOpenParen(pattern, index))
                count += 1;
            if (IsCloseParen(pattern, index))
                count -= 1;
            if (count === 0 && index !== pattern.length - 1)
                return false;
        }
        return true;
    }
    function InGroup(pattern) {
        return pattern.slice(1, pattern.length - 1);
    }
    function IsPrecedenceOr(pattern) {
        let count = 0;
        for (let index = 0; index < pattern.length; index++) {
            if (IsOpenParen(pattern, index))
                count += 1;
            if (IsCloseParen(pattern, index))
                count -= 1;
            if (IsSeparator(pattern, index) && count === 0)
                return true;
        }
        return false;
    }
    function IsPrecedenceAnd(pattern) {
        for (let index = 0; index < pattern.length; index++) {
            if (IsOpenParen(pattern, index))
                return true;
        }
        return false;
    }
    function Or(pattern) {
        let [count, start] = [0, 0];
        const expressions = [];
        for (let index = 0; index < pattern.length; index++) {
            if (IsOpenParen(pattern, index))
                count += 1;
            if (IsCloseParen(pattern, index))
                count -= 1;
            if (IsSeparator(pattern, index) && count === 0) {
                const range = pattern.slice(start, index);
                if (range.length > 0)
                    expressions.push(Parse(range));
                start = index + 1;
            }
        }
        const range = pattern.slice(start);
        if (range.length > 0)
            expressions.push(Parse(range));
        if (expressions.length === 0)
            return { type: 'const', const: '' };
        if (expressions.length === 1)
            return expressions[0];
        return { type: 'or', expr: expressions };
    }
    function And(pattern) {
        function Group(value, index) {
            if (!IsOpenParen(value, index))
                throw new TemplateLiteralParserError(`TemplateLiteralParser: Index must point to open parens`);
            let count = 0;
            for (let scan = index; scan < value.length; scan++) {
                if (IsOpenParen(value, scan))
                    count += 1;
                if (IsCloseParen(value, scan))
                    count -= 1;
                if (count === 0)
                    return [index, scan];
            }
            throw new TemplateLiteralParserError(`TemplateLiteralParser: Unclosed group parens in expression`);
        }
        function Range(pattern, index) {
            for (let scan = index; scan < pattern.length; scan++) {
                if (IsOpenParen(pattern, scan))
                    return [index, scan];
            }
            return [index, pattern.length];
        }
        const expressions = [];
        for (let index = 0; index < pattern.length; index++) {
            if (IsOpenParen(pattern, index)) {
                const [start, end] = Group(pattern, index);
                const range = pattern.slice(start, end + 1);
                expressions.push(Parse(range));
                index = end;
            }
            else {
                const [start, end] = Range(pattern, index);
                const range = pattern.slice(start, end);
                if (range.length > 0)
                    expressions.push(Parse(range));
                index = end - 1;
            }
        }
        // prettier-ignore
        return (expressions.length === 0) ? { type: 'const', const: '' } :
            (expressions.length === 1) ? expressions[0] :
                { type: 'and', expr: expressions };
    }
    /** Parses a pattern and returns an expression tree */
    function Parse(pattern) {
        // prettier-ignore
        return IsGroup(pattern) ? Parse(InGroup(pattern)) :
            IsPrecedenceOr(pattern) ? Or(pattern) :
                IsPrecedenceAnd(pattern) ? And(pattern) :
                    { type: 'const', const: pattern };
    }
    TemplateLiteralParser.Parse = Parse;
    /** Parses a pattern and strips forward and trailing ^ and $ */
    function ParseExact(pattern) {
        return Parse(pattern.slice(1, pattern.length - 1));
    }
    TemplateLiteralParser.ParseExact = ParseExact;
})(TemplateLiteralParser || (exports.TemplateLiteralParser = TemplateLiteralParser = {}));
// --------------------------------------------------------------------------------------
// TemplateLiteralFinite
// --------------------------------------------------------------------------------------
class TemplateLiteralFiniteError extends TypeBoxError {
}
exports.TemplateLiteralFiniteError = TemplateLiteralFiniteError;
var TemplateLiteralFinite;
(function (TemplateLiteralFinite) {
    function Throw(message) {
        throw new TemplateLiteralFiniteError(message);
    }
    function IsNumber(expression) {
        // prettier-ignore
        return (expression.type === 'or' &&
            expression.expr.length === 2 &&
            expression.expr[0].type === 'const' &&
            expression.expr[0].const === '0' &&
            expression.expr[1].type === 'const' &&
            expression.expr[1].const === '[1-9][0-9]*');
    }
    function IsBoolean(expression) {
        // prettier-ignore
        return (expression.type === 'or' &&
            expression.expr.length === 2 &&
            expression.expr[0].type === 'const' &&
            expression.expr[0].const === 'true' &&
            expression.expr[1].type === 'const' &&
            expression.expr[1].const === 'false');
    }
    function IsString(expression) {
        return expression.type === 'const' && expression.const === '.*';
    }
    function Check(expression) {
        // prettier-ignore
        return IsBoolean(expression) ? true :
            IsNumber(expression) || IsString(expression) ? false :
                (expression.type === 'and') ? expression.expr.every((expr) => Check(expr)) :
                    (expression.type === 'or') ? expression.expr.every((expr) => Check(expr)) :
                        (expression.type === 'const') ? true :
                            Throw(`Unknown expression type`);
    }
    TemplateLiteralFinite.Check = Check;
})(TemplateLiteralFinite || (exports.TemplateLiteralFinite = TemplateLiteralFinite = {}));
// --------------------------------------------------------------------------------------
// TemplateLiteralGenerator
// --------------------------------------------------------------------------------------
class TemplateLiteralGeneratorError extends TypeBoxError {
}
exports.TemplateLiteralGeneratorError = TemplateLiteralGeneratorError;
var TemplateLiteralGenerator;
(function (TemplateLiteralGenerator) {
    function* Reduce(buffer) {
        if (buffer.length === 1)
            return yield* buffer[0];
        for (const left of buffer[0]) {
            for (const right of Reduce(buffer.slice(1))) {
                yield `${left}${right}`;
            }
        }
    }
    function* And(expression) {
        return yield* Reduce(expression.expr.map((expr) => [...Generate(expr)]));
    }
    function* Or(expression) {
        for (const expr of expression.expr)
            yield* Generate(expr);
    }
    function* Const(expression) {
        return yield expression.const;
    }
    function* Generate(expression) {
        // prettier-ignore
        return (expression.type === 'and' ? yield* And(expression) :
            expression.type === 'or' ? yield* Or(expression) :
                expression.type === 'const' ? yield* Const(expression) :
                    (() => { throw new TemplateLiteralGeneratorError('Unknown expression'); })());
    }
    TemplateLiteralGenerator.Generate = Generate;
})(TemplateLiteralGenerator || (exports.TemplateLiteralGenerator = TemplateLiteralGenerator = {}));
// ---------------------------------------------------------------------
// TemplateLiteralDslParser
// ---------------------------------------------------------------------
var TemplateLiteralDslParser;
(function (TemplateLiteralDslParser) {
    function* ParseUnion(template) {
        const trim = template.trim().replace(/"|'/g, '');
        // prettier-ignore
        return (trim === 'boolean' ? yield exports.Type.Boolean() :
            trim === 'number' ? yield exports.Type.Number() :
                trim === 'bigint' ? yield exports.Type.BigInt() :
                    trim === 'string' ? yield exports.Type.String() :
                        yield (() => {
                            const literals = trim.split('|').map((literal) => exports.Type.Literal(literal.trim()));
                            return (literals.length === 0 ? exports.Type.Never() :
                                literals.length === 1 ? literals[0] :
                                    exports.Type.Union(literals));
                        })());
    }
    function* ParseTerminal(template) {
        if (template[1] !== '{') {
            const L = exports.Type.Literal('$');
            const R = ParseLiteral(template.slice(1));
            return yield* [L, ...R];
        }
        for (let i = 2; i < template.length; i++) {
            if (template[i] === '}') {
                const L = ParseUnion(template.slice(2, i));
                const R = ParseLiteral(template.slice(i + 1));
                return yield* [...L, ...R];
            }
        }
        yield exports.Type.Literal(template);
    }
    function* ParseLiteral(template) {
        for (let i = 0; i < template.length; i++) {
            if (template[i] === '$') {
                const L = exports.Type.Literal(template.slice(0, i));
                const R = ParseTerminal(template.slice(i));
                return yield* [L, ...R];
            }
        }
        yield exports.Type.Literal(template);
    }
    function Parse(template_dsl) {
        return [...ParseLiteral(template_dsl)];
    }
    TemplateLiteralDslParser.Parse = Parse;
})(TemplateLiteralDslParser || (exports.TemplateLiteralDslParser = TemplateLiteralDslParser = {}));
// ---------------------------------------------------------------------
// TransformBuilder
// ---------------------------------------------------------------------
class TransformDecodeBuilder {
    constructor(schema) {
        this.schema = schema;
    }
    Decode(decode) {
        return new TransformEncodeBuilder(this.schema, decode);
    }
}
exports.TransformDecodeBuilder = TransformDecodeBuilder;
class TransformEncodeBuilder {
    constructor(schema, decode) {
        this.schema = schema;
        this.decode = decode;
    }
    Encode(encode) {
        const schema = TypeClone.Type(this.schema);
        // prettier-ignore
        return (TypeGuard.TTransform(schema) ? (() => {
            const Encode = (value) => schema[exports.Transform].Encode(encode(value));
            const Decode = (value) => this.decode(schema[exports.Transform].Decode(value));
            const Codec = { Encode: Encode, Decode: Decode };
            return { ...schema, [exports.Transform]: Codec };
        })() : (() => {
            const Codec = { Decode: this.decode, Encode: encode };
            return { ...schema, [exports.Transform]: Codec };
        })());
    }
}
exports.TransformEncodeBuilder = TransformEncodeBuilder;
// --------------------------------------------------------------------------
// TypeOrdinal: Used for auto $id generation
// --------------------------------------------------------------------------
let TypeOrdinal = 0;
// --------------------------------------------------------------------------
// TypeBuilder
// --------------------------------------------------------------------------
class TypeBuilderError extends TypeBoxError {
}
exports.TypeBuilderError = TypeBuilderError;
class TypeBuilder {
    /** `[Internal]` Creates a schema without `static` and `params` types */
    Create(schema) {
        return schema;
    }
    /** `[Internal]` Throws a TypeBuilder error with the given message */
    Throw(message) {
        throw new TypeBuilderError(message);
    }
    /** `[Internal]` Discards property keys from the given record type */
    Discard(record, keys) {
        return keys.reduce((acc, key) => {
            const { [key]: _, ...rest } = acc;
            return rest;
        }, record);
    }
    /** `[Json]` Omits compositing symbols from this schema */
    Strict(schema) {
        return JSON.parse(JSON.stringify(schema));
    }
}
exports.TypeBuilder = TypeBuilder;
// --------------------------------------------------------------------------
// JsonTypeBuilder
// --------------------------------------------------------------------------
class JsonTypeBuilder extends TypeBuilder {
    // ------------------------------------------------------------------------
    // Modifiers
    // ------------------------------------------------------------------------
    /** `[Json]` Creates a Readonly and Optional property */
    ReadonlyOptional(schema) {
        return this.Readonly(this.Optional(schema));
    }
    /** `[Json]` Creates a Readonly property */
    Readonly(schema) {
        return { ...TypeClone.Type(schema), [exports.Readonly]: 'Readonly' };
    }
    /** `[Json]` Creates an Optional property */
    Optional(schema) {
        return { ...TypeClone.Type(schema), [exports.Optional]: 'Optional' };
    }
    // ------------------------------------------------------------------------
    // Types
    // ------------------------------------------------------------------------
    /** `[Json]` Creates an Any type */
    Any(options = {}) {
        return this.Create({ ...options, [exports.Kind]: 'Any' });
    }
    /** `[Json]` Creates an Array type */
    Array(schema, options = {}) {
        return this.Create({ ...options, [exports.Kind]: 'Array', type: 'array', items: TypeClone.Type(schema) });
    }
    /** `[Json]` Creates a Boolean type */
    Boolean(options = {}) {
        return this.Create({ ...options, [exports.Kind]: 'Boolean', type: 'boolean' });
    }
    /** `[Json]` Intrinsic function to Capitalize LiteralString types */
    Capitalize(schema, options = {}) {
        return { ...Intrinsic.Map(TypeClone.Type(schema), 'Capitalize'), ...options };
    }
    /** `[Json]` Creates a Composite object type */
    Composite(objects, options) {
        const intersect = exports.Type.Intersect(objects, {});
        const keys = KeyResolver.ResolveKeys(intersect, { includePatterns: false });
        const properties = keys.reduce((acc, key) => ({ ...acc, [key]: exports.Type.Index(intersect, [key]) }), {});
        return exports.Type.Object(properties, options);
    }
    /** `[Json]` Creates a Enum type */
    Enum(item, options = {}) {
        if (ValueGuard.IsUndefined(item))
            return this.Throw('Enum undefined or empty');
        // prettier-ignore
        const values1 = Object.getOwnPropertyNames(item).filter((key) => isNaN(key)).map((key) => item[key]);
        const values2 = [...new Set(values1)];
        const anyOf = values2.map((value) => exports.Type.Literal(value));
        return this.Union(anyOf, { ...options, [exports.Hint]: 'Enum' });
    }
    /** `[Json]` Creates a Conditional type */
    Extends(left, right, trueType, falseType, options = {}) {
        switch (TypeExtends.Extends(left, right)) {
            case TypeExtendsResult.Union:
                return this.Union([TypeClone.Type(trueType, options), TypeClone.Type(falseType, options)]);
            case TypeExtendsResult.True:
                return TypeClone.Type(trueType, options);
            case TypeExtendsResult.False:
                return TypeClone.Type(falseType, options);
        }
    }
    /** `[Json]` Constructs a type by excluding from unionType all union members that are assignable to excludedMembers */
    Exclude(unionType, excludedMembers, options = {}) {
        // prettier-ignore
        return (TypeGuard.TTemplateLiteral(unionType) ? this.Exclude(TemplateLiteralResolver.Resolve(unionType), excludedMembers, options) :
            TypeGuard.TTemplateLiteral(excludedMembers) ? this.Exclude(unionType, TemplateLiteralResolver.Resolve(excludedMembers), options) :
                TypeGuard.TUnion(unionType) ? (() => {
                    const narrowed = unionType.anyOf.filter((inner) => TypeExtends.Extends(inner, excludedMembers) === TypeExtendsResult.False);
                    return (narrowed.length === 1 ? TypeClone.Type(narrowed[0], options) : this.Union(narrowed, options));
                })() :
                    TypeExtends.Extends(unionType, excludedMembers) !== TypeExtendsResult.False ? this.Never(options) :
                        TypeClone.Type(unionType, options));
    }
    /** `[Json]` Constructs a type by extracting from type all union members that are assignable to union */
    Extract(type, union, options = {}) {
        // prettier-ignore
        return (TypeGuard.TTemplateLiteral(type) ? this.Extract(TemplateLiteralResolver.Resolve(type), union, options) :
            TypeGuard.TTemplateLiteral(union) ? this.Extract(type, TemplateLiteralResolver.Resolve(union), options) :
                TypeGuard.TUnion(type) ? (() => {
                    const narrowed = type.anyOf.filter((inner) => TypeExtends.Extends(inner, union) !== TypeExtendsResult.False);
                    return (narrowed.length === 1 ? TypeClone.Type(narrowed[0], options) : this.Union(narrowed, options));
                })() :
                    TypeExtends.Extends(type, union) !== TypeExtendsResult.False ? TypeClone.Type(type, options) :
                        this.Never(options));
    }
    /** `[Json]` Returns an Indexed property type for the given keys */
    Index(schema, unresolved, options = {}) {
        // prettier-ignore
        return (TypeGuard.TArray(schema) && TypeGuard.TNumber(unresolved) ? (() => {
            return TypeClone.Type(schema.items, options);
        })() :
            TypeGuard.TTuple(schema) && TypeGuard.TNumber(unresolved) ? (() => {
                const items = ValueGuard.IsUndefined(schema.items) ? [] : schema.items;
                const cloned = items.map((schema) => TypeClone.Type(schema));
                return this.Union(cloned, options);
            })() : (() => {
                const keys = KeyArrayResolver.Resolve(unresolved);
                const clone = TypeClone.Type(schema);
                return IndexedAccessor.Resolve(clone, keys, options);
            })());
    }
    /** `[Json]` Creates an Integer type */
    Integer(options = {}) {
        return this.Create({ ...options, [exports.Kind]: 'Integer', type: 'integer' });
    }
    /** `[Json]` Creates an Intersect type */
    Intersect(allOf, options = {}) {
        if (allOf.length === 0)
            return exports.Type.Never();
        if (allOf.length === 1)
            return TypeClone.Type(allOf[0], options);
        if (allOf.some((schema) => TypeGuard.TTransform(schema)))
            this.Throw('Cannot intersect transform types');
        const objects = allOf.every((schema) => TypeGuard.TObject(schema));
        const cloned = TypeClone.Rest(allOf);
        // prettier-ignore
        const clonedUnevaluatedProperties = TypeGuard.TSchema(options.unevaluatedProperties)
            ? { unevaluatedProperties: TypeClone.Type(options.unevaluatedProperties) }
            : {};
        return options.unevaluatedProperties === false || TypeGuard.TSchema(options.unevaluatedProperties) || objects
            ? this.Create({ ...options, ...clonedUnevaluatedProperties, [exports.Kind]: 'Intersect', type: 'object', allOf: cloned })
            : this.Create({ ...options, ...clonedUnevaluatedProperties, [exports.Kind]: 'Intersect', allOf: cloned });
    }
    /** `[Json]` Creates a KeyOf type */
    KeyOf(schema, options = {}) {
        // prettier-ignore
        return (TypeGuard.TRecord(schema) ? (() => {
            const pattern = Object.getOwnPropertyNames(schema.patternProperties)[0];
            return (pattern === exports.PatternNumberExact ? this.Number(options) :
                pattern === exports.PatternStringExact ? this.String(options) :
                    this.Throw('Unable to resolve key type from Record key pattern'));
        })() :
            TypeGuard.TTuple(schema) ? (() => {
                const items = ValueGuard.IsUndefined(schema.items) ? [] : schema.items;
                const literals = items.map((_, index) => exports.Type.Literal(index.toString()));
                return this.Union(literals, options);
            })() :
                TypeGuard.TArray(schema) ? (() => {
                    return this.Number(options);
                })() : (() => {
                    const keys = KeyResolver.ResolveKeys(schema, { includePatterns: false });
                    if (keys.length === 0)
                        return this.Never(options);
                    const literals = keys.map((key) => this.Literal(key));
                    return this.Union(literals, options);
                })());
    }
    /** `[Json]` Creates a Literal type */
    Literal(value, options = {}) {
        return this.Create({ ...options, [exports.Kind]: 'Literal', const: value, type: typeof value });
    }
    /** `[Json]` Intrinsic function to Lowercase LiteralString types */
    Lowercase(schema, options = {}) {
        return { ...Intrinsic.Map(TypeClone.Type(schema), 'Lowercase'), ...options };
    }
    /** `[Json]` Creates a Never type */
    Never(options = {}) {
        return this.Create({ ...options, [exports.Kind]: 'Never', not: {} });
    }
    /** `[Json]` Creates a Not type */
    Not(schema, options) {
        return this.Create({ ...options, [exports.Kind]: 'Not', not: TypeClone.Type(schema) });
    }
    /** `[Json]` Creates a Null type */
    Null(options = {}) {
        return this.Create({ ...options, [exports.Kind]: 'Null', type: 'null' });
    }
    /** `[Json]` Creates a Number type */
    Number(options = {}) {
        return this.Create({ ...options, [exports.Kind]: 'Number', type: 'number' });
    }
    /** `[Json]` Creates an Object type */
    Object(properties, options = {}) {
        const propertyKeys = Object.getOwnPropertyNames(properties);
        const optionalKeys = propertyKeys.filter((key) => TypeGuard.TOptional(properties[key]));
        const requiredKeys = propertyKeys.filter((name) => !optionalKeys.includes(name));
        const clonedAdditionalProperties = TypeGuard.TSchema(options.additionalProperties) ? { additionalProperties: TypeClone.Type(options.additionalProperties) } : {};
        const clonedProperties = propertyKeys.reduce((acc, key) => ({ ...acc, [key]: TypeClone.Type(properties[key]) }), {});
        return requiredKeys.length > 0
            ? this.Create({ ...options, ...clonedAdditionalProperties, [exports.Kind]: 'Object', type: 'object', properties: clonedProperties, required: requiredKeys })
            : this.Create({ ...options, ...clonedAdditionalProperties, [exports.Kind]: 'Object', type: 'object', properties: clonedProperties });
    }
    /** `[Json]` Constructs a type whose keys are omitted from the given type */
    Omit(schema, unresolved, options = {}) {
        const keys = KeyArrayResolver.Resolve(unresolved);
        // prettier-ignore
        return ObjectMap.Map(this.Discard(TypeClone.Type(schema), ['$id', exports.Transform]), (object) => {
            if (ValueGuard.IsArray(object.required)) {
                object.required = object.required.filter((key) => !keys.includes(key));
                if (object.required.length === 0)
                    delete object.required;
            }
            for (const key of Object.getOwnPropertyNames(object.properties)) {
                if (keys.includes(key))
                    delete object.properties[key];
            }
            return this.Create(object);
        }, options);
    }
    /** `[Json]` Constructs a type where all properties are optional */
    Partial(schema, options = {}) {
        // prettier-ignore
        return ObjectMap.Map(this.Discard(TypeClone.Type(schema), ['$id', exports.Transform]), (object) => {
            const properties = Object.getOwnPropertyNames(object.properties).reduce((acc, key) => {
                return { ...acc, [key]: this.Optional(object.properties[key]) };
            }, {});
            return this.Object(properties, this.Discard(object, ['required']) /* object used as options to retain other constraints */);
        }, options);
    }
    /** `[Json]` Constructs a type whose keys are picked from the given type */
    Pick(schema, unresolved, options = {}) {
        const keys = KeyArrayResolver.Resolve(unresolved);
        // prettier-ignore
        return ObjectMap.Map(this.Discard(TypeClone.Type(schema), ['$id', exports.Transform]), (object) => {
            if (ValueGuard.IsArray(object.required)) {
                object.required = object.required.filter((key) => keys.includes(key));
                if (object.required.length === 0)
                    delete object.required;
            }
            for (const key of Object.getOwnPropertyNames(object.properties)) {
                if (!keys.includes(key))
                    delete object.properties[key];
            }
            return this.Create(object);
        }, options);
    }
    /** `[Json]` Creates a Record type */
    Record(key, schema, options = {}) {
        // prettier-ignore
        return (TypeGuard.TTemplateLiteral(key) ? (() => {
            const expression = TemplateLiteralParser.ParseExact(key.pattern);
            // prettier-ignore
            return TemplateLiteralFinite.Check(expression)
                ? (this.Object([...TemplateLiteralGenerator.Generate(expression)].reduce((acc, key) => ({ ...acc, [key]: TypeClone.Type(schema) }), {}), options))
                : this.Create({ ...options, [exports.Kind]: 'Record', type: 'object', patternProperties: { [key.pattern]: TypeClone.Type(schema) } });
        })() :
            TypeGuard.TUnion(key) ? (() => {
                const union = UnionResolver.Resolve(key);
                if (TypeGuard.TUnionLiteral(union)) {
                    const properties = union.anyOf.reduce((acc, literal) => ({ ...acc, [literal.const]: TypeClone.Type(schema) }), {});
                    return this.Object(properties, { ...options, [exports.Hint]: 'Record' });
                }
                else
                    this.Throw('Record key of type union contains non-literal types');
            })() :
                TypeGuard.TLiteral(key) ? (() => {
                    // prettier-ignore
                    return (ValueGuard.IsString(key.const) || ValueGuard.IsNumber(key.const))
                        ? this.Object({ [key.const]: TypeClone.Type(schema) }, options)
                        : this.Throw('Record key of type literal is not of type string or number');
                })() :
                    TypeGuard.TInteger(key) || TypeGuard.TNumber(key) ? (() => {
                        return this.Create({ ...options, [exports.Kind]: 'Record', type: 'object', patternProperties: { [exports.PatternNumberExact]: TypeClone.Type(schema) } });
                    })() :
                        TypeGuard.TString(key) ? (() => {
                            const pattern = ValueGuard.IsUndefined(key.pattern) ? exports.PatternStringExact : key.pattern;
                            return this.Create({ ...options, [exports.Kind]: 'Record', type: 'object', patternProperties: { [pattern]: TypeClone.Type(schema) } });
                        })() :
                            this.Never());
    }
    /** `[Json]` Creates a Recursive type */
    Recursive(callback, options = {}) {
        if (ValueGuard.IsUndefined(options.$id))
            options.$id = `T${TypeOrdinal++}`;
        const thisType = callback({ [exports.Kind]: 'This', $ref: `${options.$id}` });
        thisType.$id = options.$id;
        return this.Create({ ...options, [exports.Hint]: 'Recursive', ...thisType });
    }
    /** `[Json]` Creates a Ref type. */
    Ref(unresolved, options = {}) {
        if (ValueGuard.IsString(unresolved))
            return this.Create({ ...options, [exports.Kind]: 'Ref', $ref: unresolved });
        if (ValueGuard.IsUndefined(unresolved.$id))
            this.Throw('Reference target type must specify an $id');
        return this.Create({ ...options, [exports.Kind]: 'Ref', $ref: unresolved.$id });
    }
    /** `[Json]` Constructs a type where all properties are required */
    Required(schema, options = {}) {
        // prettier-ignore
        return ObjectMap.Map(this.Discard(TypeClone.Type(schema), ['$id', exports.Transform]), (object) => {
            const properties = Object.getOwnPropertyNames(object.properties).reduce((acc, key) => {
                return { ...acc, [key]: this.Discard(object.properties[key], [exports.Optional]) };
            }, {});
            return this.Object(properties, object /* object used as options to retain other constraints  */);
        }, options);
    }
    /** `[Json]` Extracts interior Rest elements from Tuple, Intersect and Union types */
    Rest(schema) {
        return (TypeGuard.TTuple(schema) && !ValueGuard.IsUndefined(schema.items) ? TypeClone.Rest(schema.items) : TypeGuard.TIntersect(schema) ? TypeClone.Rest(schema.allOf) : TypeGuard.TUnion(schema) ? TypeClone.Rest(schema.anyOf) : []);
    }
    /** `[Json]` Creates a String type */
    String(options = {}) {
        return this.Create({ ...options, [exports.Kind]: 'String', type: 'string' });
    }
    /** `[Json]` Creates a TemplateLiteral type */
    TemplateLiteral(unresolved, options = {}) {
        // prettier-ignore
        const pattern = ValueGuard.IsString(unresolved)
            ? TemplateLiteralPattern.Create(TemplateLiteralDslParser.Parse(unresolved))
            : TemplateLiteralPattern.Create(unresolved);
        return this.Create({ ...options, [exports.Kind]: 'TemplateLiteral', type: 'string', pattern });
    }
    /** `[Json]` Creates a Transform type */
    Transform(schema) {
        return new TransformDecodeBuilder(schema);
    }
    /** `[Json]` Creates a Tuple type */
    Tuple(items, options = {}) {
        const [additionalItems, minItems, maxItems] = [false, items.length, items.length];
        const clonedItems = TypeClone.Rest(items);
        // prettier-ignore
        const schema = (items.length > 0 ?
            { ...options, [exports.Kind]: 'Tuple', type: 'array', items: clonedItems, additionalItems, minItems, maxItems } :
            { ...options, [exports.Kind]: 'Tuple', type: 'array', minItems, maxItems });
        return this.Create(schema);
    }
    /** `[Json]` Intrinsic function to Uncapitalize LiteralString types */
    Uncapitalize(schema, options = {}) {
        return { ...Intrinsic.Map(TypeClone.Type(schema), 'Uncapitalize'), ...options };
    }
    /** `[Json]` Creates a Union type */
    Union(union, options = {}) {
        // prettier-ignore
        return TypeGuard.TTemplateLiteral(union)
            ? TemplateLiteralResolver.Resolve(union)
            : (() => {
                const anyOf = union;
                if (anyOf.length === 0)
                    return this.Never(options);
                if (anyOf.length === 1)
                    return this.Create(TypeClone.Type(anyOf[0], options));
                const clonedAnyOf = TypeClone.Rest(anyOf);
                return this.Create({ ...options, [exports.Kind]: 'Union', anyOf: clonedAnyOf });
            })();
    }
    /** `[Json]` Creates an Unknown type */
    Unknown(options = {}) {
        return this.Create({ ...options, [exports.Kind]: 'Unknown' });
    }
    /** `[Json]` Creates a Unsafe type that will infers as the generic argument T */
    Unsafe(options = {}) {
        return this.Create({ ...options, [exports.Kind]: options[exports.Kind] || 'Unsafe' });
    }
    /** `[Json]` Intrinsic function to Uppercase LiteralString types */
    Uppercase(schema, options = {}) {
        return { ...Intrinsic.Map(TypeClone.Type(schema), 'Uppercase'), ...options };
    }
}
exports.JsonTypeBuilder = JsonTypeBuilder;
// --------------------------------------------------------------------------
// JavaScriptTypeBuilder
// --------------------------------------------------------------------------
class JavaScriptTypeBuilder extends JsonTypeBuilder {
    /** `[JavaScript]` Creates a AsyncIterator type */
    AsyncIterator(items, options = {}) {
        return this.Create({ ...options, [exports.Kind]: 'AsyncIterator', type: 'AsyncIterator', items: TypeClone.Type(items) });
    }
    /** `[JavaScript]` Constructs a type by recursively unwrapping Promise types */
    Awaited(schema, options = {}) {
        // prettier-ignore
        const Unwrap = (rest) => rest.length > 0 ? (() => {
            const [L, ...R] = rest;
            return [this.Awaited(L), ...Unwrap(R)];
        })() : rest;
        // prettier-ignore
        return (TypeGuard.TIntersect(schema) ? exports.Type.Intersect(Unwrap(schema.allOf)) :
            TypeGuard.TUnion(schema) ? exports.Type.Union(Unwrap(schema.anyOf)) :
                TypeGuard.TPromise(schema) ? this.Awaited(schema.item) :
                    TypeClone.Type(schema, options));
    }
    /** `[JavaScript]` Creates a BigInt type */
    BigInt(options = {}) {
        return this.Create({ ...options, [exports.Kind]: 'BigInt', type: 'bigint' });
    }
    /** `[JavaScript]` Extracts the ConstructorParameters from the given Constructor type */
    ConstructorParameters(schema, options = {}) {
        return this.Tuple([...schema.parameters], { ...options });
    }
    /** `[JavaScript]` Creates a Constructor type */
    Constructor(parameters, returns, options) {
        const [clonedParameters, clonedReturns] = [TypeClone.Rest(parameters), TypeClone.Type(returns)];
        return this.Create({ ...options, [exports.Kind]: 'Constructor', type: 'Constructor', parameters: clonedParameters, returns: clonedReturns });
    }
    /** `[JavaScript]` Creates a Date type */
    Date(options = {}) {
        return this.Create({ ...options, [exports.Kind]: 'Date', type: 'Date' });
    }
    /** `[JavaScript]` Creates a Function type */
    Function(parameters, returns, options) {
        const [clonedParameters, clonedReturns] = [TypeClone.Rest(parameters), TypeClone.Type(returns)];
        return this.Create({ ...options, [exports.Kind]: 'Function', type: 'Function', parameters: clonedParameters, returns: clonedReturns });
    }
    /** `[JavaScript]` Extracts the InstanceType from the given Constructor type */
    InstanceType(schema, options = {}) {
        return TypeClone.Type(schema.returns, options);
    }
    /** `[JavaScript]` Creates an Iterator type */
    Iterator(items, options = {}) {
        return this.Create({ ...options, [exports.Kind]: 'Iterator', type: 'Iterator', items: TypeClone.Type(items) });
    }
    /** `[JavaScript]` Extracts the Parameters from the given Function type */
    Parameters(schema, options = {}) {
        return this.Tuple(schema.parameters, { ...options });
    }
    /** `[JavaScript]` Creates a Promise type */
    Promise(item, options = {}) {
        return this.Create({ ...options, [exports.Kind]: 'Promise', type: 'Promise', item: TypeClone.Type(item) });
    }
    /** `[Extended]` Creates a String type */
    RegExp(unresolved, options = {}) {
        const pattern = ValueGuard.IsString(unresolved) ? unresolved : unresolved.source;
        return this.Create({ ...options, [exports.Kind]: 'String', type: 'string', pattern });
    }
    /**
     * @deprecated Use `Type.RegExp`
     */
    RegEx(regex, options = {}) {
        return this.RegExp(regex, options);
    }
    /** `[JavaScript]` Extracts the ReturnType from the given Function type */
    ReturnType(schema, options = {}) {
        return TypeClone.Type(schema.returns, options);
    }
    /** `[JavaScript]` Creates a Symbol type */
    Symbol(options) {
        return this.Create({ ...options, [exports.Kind]: 'Symbol', type: 'symbol' });
    }
    /** `[JavaScript]` Creates a Undefined type */
    Undefined(options = {}) {
        return this.Create({ ...options, [exports.Kind]: 'Undefined', type: 'undefined' });
    }
    /** `[JavaScript]` Creates a Uint8Array type */
    Uint8Array(options = {}) {
        return this.Create({ ...options, [exports.Kind]: 'Uint8Array', type: 'Uint8Array' });
    }
    /** `[JavaScript]` Creates a Void type */
    Void(options = {}) {
        return this.Create({ ...options, [exports.Kind]: 'Void', type: 'void' });
    }
}
exports.JavaScriptTypeBuilder = JavaScriptTypeBuilder;
/** Json Type Builder with Static Resolution for TypeScript */
exports.JsonType = new JsonTypeBuilder();
/** JavaScript Type Builder with Static Resolution for TypeScript */
exports.Type = new JavaScriptTypeBuilder();
