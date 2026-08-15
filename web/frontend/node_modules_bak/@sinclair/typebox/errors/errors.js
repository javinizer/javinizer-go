"use strict";
/*--------------------------------------------------------------------------

@sinclair/typebox/errors

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
exports.Errors = exports.ValueErrorIterator = exports.EscapeKey = exports.ValueErrorsUnknownTypeError = exports.ValueErrorType = void 0;
const guard_1 = require("../value/guard");
const system_1 = require("../system/system");
const deref_1 = require("../value/deref");
const hash_1 = require("../value/hash");
const Types = require("../typebox");
// --------------------------------------------------------------------------
// ValueErrorType
// --------------------------------------------------------------------------
var ValueErrorType;
(function (ValueErrorType) {
    ValueErrorType[ValueErrorType["ArrayContains"] = 0] = "ArrayContains";
    ValueErrorType[ValueErrorType["ArrayMaxContains"] = 1] = "ArrayMaxContains";
    ValueErrorType[ValueErrorType["ArrayMaxItems"] = 2] = "ArrayMaxItems";
    ValueErrorType[ValueErrorType["ArrayMinContains"] = 3] = "ArrayMinContains";
    ValueErrorType[ValueErrorType["ArrayMinItems"] = 4] = "ArrayMinItems";
    ValueErrorType[ValueErrorType["ArrayUniqueItems"] = 5] = "ArrayUniqueItems";
    ValueErrorType[ValueErrorType["Array"] = 6] = "Array";
    ValueErrorType[ValueErrorType["AsyncIterator"] = 7] = "AsyncIterator";
    ValueErrorType[ValueErrorType["BigIntExclusiveMaximum"] = 8] = "BigIntExclusiveMaximum";
    ValueErrorType[ValueErrorType["BigIntExclusiveMinimum"] = 9] = "BigIntExclusiveMinimum";
    ValueErrorType[ValueErrorType["BigIntMaximum"] = 10] = "BigIntMaximum";
    ValueErrorType[ValueErrorType["BigIntMinimum"] = 11] = "BigIntMinimum";
    ValueErrorType[ValueErrorType["BigIntMultipleOf"] = 12] = "BigIntMultipleOf";
    ValueErrorType[ValueErrorType["BigInt"] = 13] = "BigInt";
    ValueErrorType[ValueErrorType["Boolean"] = 14] = "Boolean";
    ValueErrorType[ValueErrorType["DateExclusiveMaximumTimestamp"] = 15] = "DateExclusiveMaximumTimestamp";
    ValueErrorType[ValueErrorType["DateExclusiveMinimumTimestamp"] = 16] = "DateExclusiveMinimumTimestamp";
    ValueErrorType[ValueErrorType["DateMaximumTimestamp"] = 17] = "DateMaximumTimestamp";
    ValueErrorType[ValueErrorType["DateMinimumTimestamp"] = 18] = "DateMinimumTimestamp";
    ValueErrorType[ValueErrorType["DateMultipleOfTimestamp"] = 19] = "DateMultipleOfTimestamp";
    ValueErrorType[ValueErrorType["Date"] = 20] = "Date";
    ValueErrorType[ValueErrorType["Function"] = 21] = "Function";
    ValueErrorType[ValueErrorType["IntegerExclusiveMaximum"] = 22] = "IntegerExclusiveMaximum";
    ValueErrorType[ValueErrorType["IntegerExclusiveMinimum"] = 23] = "IntegerExclusiveMinimum";
    ValueErrorType[ValueErrorType["IntegerMaximum"] = 24] = "IntegerMaximum";
    ValueErrorType[ValueErrorType["IntegerMinimum"] = 25] = "IntegerMinimum";
    ValueErrorType[ValueErrorType["IntegerMultipleOf"] = 26] = "IntegerMultipleOf";
    ValueErrorType[ValueErrorType["Integer"] = 27] = "Integer";
    ValueErrorType[ValueErrorType["IntersectUnevaluatedProperties"] = 28] = "IntersectUnevaluatedProperties";
    ValueErrorType[ValueErrorType["Intersect"] = 29] = "Intersect";
    ValueErrorType[ValueErrorType["Iterator"] = 30] = "Iterator";
    ValueErrorType[ValueErrorType["Kind"] = 31] = "Kind";
    ValueErrorType[ValueErrorType["Literal"] = 32] = "Literal";
    ValueErrorType[ValueErrorType["Never"] = 33] = "Never";
    ValueErrorType[ValueErrorType["Not"] = 34] = "Not";
    ValueErrorType[ValueErrorType["Null"] = 35] = "Null";
    ValueErrorType[ValueErrorType["NumberExclusiveMaximum"] = 36] = "NumberExclusiveMaximum";
    ValueErrorType[ValueErrorType["NumberExclusiveMinimum"] = 37] = "NumberExclusiveMinimum";
    ValueErrorType[ValueErrorType["NumberMaximum"] = 38] = "NumberMaximum";
    ValueErrorType[ValueErrorType["NumberMinimum"] = 39] = "NumberMinimum";
    ValueErrorType[ValueErrorType["NumberMultipleOf"] = 40] = "NumberMultipleOf";
    ValueErrorType[ValueErrorType["Number"] = 41] = "Number";
    ValueErrorType[ValueErrorType["ObjectAdditionalProperties"] = 42] = "ObjectAdditionalProperties";
    ValueErrorType[ValueErrorType["ObjectMaxProperties"] = 43] = "ObjectMaxProperties";
    ValueErrorType[ValueErrorType["ObjectMinProperties"] = 44] = "ObjectMinProperties";
    ValueErrorType[ValueErrorType["ObjectRequiredProperty"] = 45] = "ObjectRequiredProperty";
    ValueErrorType[ValueErrorType["Object"] = 46] = "Object";
    ValueErrorType[ValueErrorType["Promise"] = 47] = "Promise";
    ValueErrorType[ValueErrorType["StringFormatUnknown"] = 48] = "StringFormatUnknown";
    ValueErrorType[ValueErrorType["StringFormat"] = 49] = "StringFormat";
    ValueErrorType[ValueErrorType["StringMaxLength"] = 50] = "StringMaxLength";
    ValueErrorType[ValueErrorType["StringMinLength"] = 51] = "StringMinLength";
    ValueErrorType[ValueErrorType["StringPattern"] = 52] = "StringPattern";
    ValueErrorType[ValueErrorType["String"] = 53] = "String";
    ValueErrorType[ValueErrorType["Symbol"] = 54] = "Symbol";
    ValueErrorType[ValueErrorType["TupleLength"] = 55] = "TupleLength";
    ValueErrorType[ValueErrorType["Tuple"] = 56] = "Tuple";
    ValueErrorType[ValueErrorType["Uint8ArrayMaxByteLength"] = 57] = "Uint8ArrayMaxByteLength";
    ValueErrorType[ValueErrorType["Uint8ArrayMinByteLength"] = 58] = "Uint8ArrayMinByteLength";
    ValueErrorType[ValueErrorType["Uint8Array"] = 59] = "Uint8Array";
    ValueErrorType[ValueErrorType["Undefined"] = 60] = "Undefined";
    ValueErrorType[ValueErrorType["Union"] = 61] = "Union";
    ValueErrorType[ValueErrorType["Void"] = 62] = "Void";
})(ValueErrorType || (exports.ValueErrorType = ValueErrorType = {}));
// --------------------------------------------------------------------------
// ValueErrors
// --------------------------------------------------------------------------
class ValueErrorsUnknownTypeError extends Types.TypeBoxError {
    constructor(schema) {
        super('Unknown type');
        this.schema = schema;
    }
}
exports.ValueErrorsUnknownTypeError = ValueErrorsUnknownTypeError;
// --------------------------------------------------------------------------
// EscapeKey
// --------------------------------------------------------------------------
function EscapeKey(key) {
    return key.replace(/~/g, '~0').replace(/\//g, '~1'); // RFC6901 Path
}
exports.EscapeKey = EscapeKey;
// --------------------------------------------------------------------------
// Guards
// --------------------------------------------------------------------------
function IsDefined(value) {
    return value !== undefined;
}
// --------------------------------------------------------------------------
// ValueErrorIterator
// --------------------------------------------------------------------------
class ValueErrorIterator {
    constructor(iterator) {
        this.iterator = iterator;
    }
    [Symbol.iterator]() {
        return this.iterator;
    }
    /** Returns the first value error or undefined if no errors */
    First() {
        const next = this.iterator.next();
        return next.done ? undefined : next.value;
    }
}
exports.ValueErrorIterator = ValueErrorIterator;
// --------------------------------------------------------------------------
// Create
// --------------------------------------------------------------------------
function Create(type, schema, path, value) {
    return { type, schema, path, value, message: system_1.TypeSystemErrorFunction.Get()(schema, type) };
}
// --------------------------------------------------------------------------
// Types
// --------------------------------------------------------------------------
function* TAny(schema, references, path, value) { }
function* TArray(schema, references, path, value) {
    if (!(0, guard_1.IsArray)(value)) {
        return yield Create(ValueErrorType.Array, schema, path, value);
    }
    if (IsDefined(schema.minItems) && !(value.length >= schema.minItems)) {
        yield Create(ValueErrorType.ArrayMinItems, schema, path, value);
    }
    if (IsDefined(schema.maxItems) && !(value.length <= schema.maxItems)) {
        yield Create(ValueErrorType.ArrayMaxItems, schema, path, value);
    }
    for (let i = 0; i < value.length; i++) {
        yield* Visit(schema.items, references, `${path}/${i}`, value[i]);
    }
    // prettier-ignore
    if (schema.uniqueItems === true && !((function () { const set = new Set(); for (const element of value) {
        const hashed = (0, hash_1.Hash)(element);
        if (set.has(hashed)) {
            return false;
        }
        else {
            set.add(hashed);
        }
    } return true; })())) {
        yield Create(ValueErrorType.ArrayUniqueItems, schema, path, value);
    }
    // contains
    if (!(IsDefined(schema.contains) || IsDefined(schema.minContains) || IsDefined(schema.maxContains))) {
        return;
    }
    const containsSchema = IsDefined(schema.contains) ? schema.contains : Types.Type.Never();
    const containsCount = value.reduce((acc, value, index) => (Visit(containsSchema, references, `${path}${index}`, value).next().done === true ? acc + 1 : acc), 0);
    if (containsCount === 0) {
        yield Create(ValueErrorType.ArrayContains, schema, path, value);
    }
    if ((0, guard_1.IsNumber)(schema.minContains) && containsCount < schema.minContains) {
        yield Create(ValueErrorType.ArrayMinContains, schema, path, value);
    }
    if ((0, guard_1.IsNumber)(schema.maxContains) && containsCount > schema.maxContains) {
        yield Create(ValueErrorType.ArrayMaxContains, schema, path, value);
    }
}
function* TAsyncIterator(schema, references, path, value) {
    if (!(0, guard_1.IsAsyncIterator)(value))
        yield Create(ValueErrorType.AsyncIterator, schema, path, value);
}
function* TBigInt(schema, references, path, value) {
    if (!(0, guard_1.IsBigInt)(value))
        return yield Create(ValueErrorType.BigInt, schema, path, value);
    if (IsDefined(schema.exclusiveMaximum) && !(value < schema.exclusiveMaximum)) {
        yield Create(ValueErrorType.BigIntExclusiveMaximum, schema, path, value);
    }
    if (IsDefined(schema.exclusiveMinimum) && !(value > schema.exclusiveMinimum)) {
        yield Create(ValueErrorType.BigIntExclusiveMinimum, schema, path, value);
    }
    if (IsDefined(schema.maximum) && !(value <= schema.maximum)) {
        yield Create(ValueErrorType.BigIntMaximum, schema, path, value);
    }
    if (IsDefined(schema.minimum) && !(value >= schema.minimum)) {
        yield Create(ValueErrorType.BigIntMinimum, schema, path, value);
    }
    if (IsDefined(schema.multipleOf) && !(value % schema.multipleOf === BigInt(0))) {
        yield Create(ValueErrorType.BigIntMultipleOf, schema, path, value);
    }
}
function* TBoolean(schema, references, path, value) {
    if (!(0, guard_1.IsBoolean)(value))
        yield Create(ValueErrorType.Boolean, schema, path, value);
}
function* TConstructor(schema, references, path, value) {
    yield* Visit(schema.returns, references, path, value.prototype);
}
function* TDate(schema, references, path, value) {
    if (!(0, guard_1.IsDate)(value))
        return yield Create(ValueErrorType.Date, schema, path, value);
    if (IsDefined(schema.exclusiveMaximumTimestamp) && !(value.getTime() < schema.exclusiveMaximumTimestamp)) {
        yield Create(ValueErrorType.DateExclusiveMaximumTimestamp, schema, path, value);
    }
    if (IsDefined(schema.exclusiveMinimumTimestamp) && !(value.getTime() > schema.exclusiveMinimumTimestamp)) {
        yield Create(ValueErrorType.DateExclusiveMinimumTimestamp, schema, path, value);
    }
    if (IsDefined(schema.maximumTimestamp) && !(value.getTime() <= schema.maximumTimestamp)) {
        yield Create(ValueErrorType.DateMaximumTimestamp, schema, path, value);
    }
    if (IsDefined(schema.minimumTimestamp) && !(value.getTime() >= schema.minimumTimestamp)) {
        yield Create(ValueErrorType.DateMinimumTimestamp, schema, path, value);
    }
    if (IsDefined(schema.multipleOfTimestamp) && !(value.getTime() % schema.multipleOfTimestamp === 0)) {
        yield Create(ValueErrorType.DateMultipleOfTimestamp, schema, path, value);
    }
}
function* TFunction(schema, references, path, value) {
    if (!(0, guard_1.IsFunction)(value))
        yield Create(ValueErrorType.Function, schema, path, value);
}
function* TInteger(schema, references, path, value) {
    if (!(0, guard_1.IsInteger)(value))
        return yield Create(ValueErrorType.Integer, schema, path, value);
    if (IsDefined(schema.exclusiveMaximum) && !(value < schema.exclusiveMaximum)) {
        yield Create(ValueErrorType.IntegerExclusiveMaximum, schema, path, value);
    }
    if (IsDefined(schema.exclusiveMinimum) && !(value > schema.exclusiveMinimum)) {
        yield Create(ValueErrorType.IntegerExclusiveMinimum, schema, path, value);
    }
    if (IsDefined(schema.maximum) && !(value <= schema.maximum)) {
        yield Create(ValueErrorType.IntegerMaximum, schema, path, value);
    }
    if (IsDefined(schema.minimum) && !(value >= schema.minimum)) {
        yield Create(ValueErrorType.IntegerMinimum, schema, path, value);
    }
    if (IsDefined(schema.multipleOf) && !(value % schema.multipleOf === 0)) {
        yield Create(ValueErrorType.IntegerMultipleOf, schema, path, value);
    }
}
function* TIntersect(schema, references, path, value) {
    for (const inner of schema.allOf) {
        const next = Visit(inner, references, path, value).next();
        if (!next.done) {
            yield Create(ValueErrorType.Intersect, schema, path, value);
            yield next.value;
        }
    }
    if (schema.unevaluatedProperties === false) {
        const keyCheck = new RegExp(Types.KeyResolver.ResolvePattern(schema));
        for (const valueKey of Object.getOwnPropertyNames(value)) {
            if (!keyCheck.test(valueKey)) {
                yield Create(ValueErrorType.IntersectUnevaluatedProperties, schema, `${path}/${valueKey}`, value);
            }
        }
    }
    if (typeof schema.unevaluatedProperties === 'object') {
        const keyCheck = new RegExp(Types.KeyResolver.ResolvePattern(schema));
        for (const valueKey of Object.getOwnPropertyNames(value)) {
            if (!keyCheck.test(valueKey)) {
                const next = Visit(schema.unevaluatedProperties, references, `${path}/${valueKey}`, value[valueKey]).next();
                if (!next.done)
                    yield next.value; // yield interior
            }
        }
    }
}
function* TIterator(schema, references, path, value) {
    if (!(0, guard_1.IsIterator)(value))
        yield Create(ValueErrorType.Iterator, schema, path, value);
}
function* TLiteral(schema, references, path, value) {
    if (!(value === schema.const))
        yield Create(ValueErrorType.Literal, schema, path, value);
}
function* TNever(schema, references, path, value) {
    yield Create(ValueErrorType.Never, schema, path, value);
}
function* TNot(schema, references, path, value) {
    if (Visit(schema.not, references, path, value).next().done === true)
        yield Create(ValueErrorType.Not, schema, path, value);
}
function* TNull(schema, references, path, value) {
    if (!(0, guard_1.IsNull)(value))
        yield Create(ValueErrorType.Null, schema, path, value);
}
function* TNumber(schema, references, path, value) {
    if (!system_1.TypeSystemPolicy.IsNumberLike(value))
        return yield Create(ValueErrorType.Number, schema, path, value);
    if (IsDefined(schema.exclusiveMaximum) && !(value < schema.exclusiveMaximum)) {
        yield Create(ValueErrorType.NumberExclusiveMaximum, schema, path, value);
    }
    if (IsDefined(schema.exclusiveMinimum) && !(value > schema.exclusiveMinimum)) {
        yield Create(ValueErrorType.NumberExclusiveMinimum, schema, path, value);
    }
    if (IsDefined(schema.maximum) && !(value <= schema.maximum)) {
        yield Create(ValueErrorType.NumberMaximum, schema, path, value);
    }
    if (IsDefined(schema.minimum) && !(value >= schema.minimum)) {
        yield Create(ValueErrorType.NumberMinimum, schema, path, value);
    }
    if (IsDefined(schema.multipleOf) && !(value % schema.multipleOf === 0)) {
        yield Create(ValueErrorType.NumberMultipleOf, schema, path, value);
    }
}
function* TObject(schema, references, path, value) {
    if (!system_1.TypeSystemPolicy.IsObjectLike(value))
        return yield Create(ValueErrorType.Object, schema, path, value);
    if (IsDefined(schema.minProperties) && !(Object.getOwnPropertyNames(value).length >= schema.minProperties)) {
        yield Create(ValueErrorType.ObjectMinProperties, schema, path, value);
    }
    if (IsDefined(schema.maxProperties) && !(Object.getOwnPropertyNames(value).length <= schema.maxProperties)) {
        yield Create(ValueErrorType.ObjectMaxProperties, schema, path, value);
    }
    const requiredKeys = Array.isArray(schema.required) ? schema.required : [];
    const knownKeys = Object.getOwnPropertyNames(schema.properties);
    const unknownKeys = Object.getOwnPropertyNames(value);
    for (const requiredKey of requiredKeys) {
        if (unknownKeys.includes(requiredKey))
            continue;
        yield Create(ValueErrorType.ObjectRequiredProperty, schema.properties[requiredKey], `${path}/${EscapeKey(requiredKey)}`, undefined);
    }
    if (schema.additionalProperties === false) {
        for (const valueKey of unknownKeys) {
            if (!knownKeys.includes(valueKey)) {
                yield Create(ValueErrorType.ObjectAdditionalProperties, schema, `${path}/${EscapeKey(valueKey)}`, value[valueKey]);
            }
        }
    }
    if (typeof schema.additionalProperties === 'object') {
        for (const valueKey of unknownKeys) {
            if (knownKeys.includes(valueKey))
                continue;
            yield* Visit(schema.additionalProperties, references, `${path}/${EscapeKey(valueKey)}`, value[valueKey]);
        }
    }
    for (const knownKey of knownKeys) {
        const property = schema.properties[knownKey];
        if (schema.required && schema.required.includes(knownKey)) {
            yield* Visit(property, references, `${path}/${EscapeKey(knownKey)}`, value[knownKey]);
            if (Types.ExtendsUndefined.Check(schema) && !(knownKey in value)) {
                yield Create(ValueErrorType.ObjectRequiredProperty, property, `${path}/${EscapeKey(knownKey)}`, undefined);
            }
        }
        else {
            if (system_1.TypeSystemPolicy.IsExactOptionalProperty(value, knownKey)) {
                yield* Visit(property, references, `${path}/${EscapeKey(knownKey)}`, value[knownKey]);
            }
        }
    }
}
function* TPromise(schema, references, path, value) {
    if (!(0, guard_1.IsPromise)(value))
        yield Create(ValueErrorType.Promise, schema, path, value);
}
function* TRecord(schema, references, path, value) {
    if (!system_1.TypeSystemPolicy.IsRecordLike(value))
        return yield Create(ValueErrorType.Object, schema, path, value);
    if (IsDefined(schema.minProperties) && !(Object.getOwnPropertyNames(value).length >= schema.minProperties)) {
        yield Create(ValueErrorType.ObjectMinProperties, schema, path, value);
    }
    if (IsDefined(schema.maxProperties) && !(Object.getOwnPropertyNames(value).length <= schema.maxProperties)) {
        yield Create(ValueErrorType.ObjectMaxProperties, schema, path, value);
    }
    const [patternKey, patternSchema] = Object.entries(schema.patternProperties)[0];
    const regex = new RegExp(patternKey);
    for (const [propertyKey, propertyValue] of Object.entries(value)) {
        if (regex.test(propertyKey))
            yield* Visit(patternSchema, references, `${path}/${EscapeKey(propertyKey)}`, propertyValue);
    }
    if (typeof schema.additionalProperties === 'object') {
        for (const [propertyKey, propertyValue] of Object.entries(value)) {
            if (!regex.test(propertyKey))
                yield* Visit(schema.additionalProperties, references, `${path}/${EscapeKey(propertyKey)}`, propertyValue);
        }
    }
    if (schema.additionalProperties === false) {
        for (const [propertyKey, propertyValue] of Object.entries(value)) {
            if (regex.test(propertyKey))
                continue;
            return yield Create(ValueErrorType.ObjectAdditionalProperties, schema, `${path}/${EscapeKey(propertyKey)}`, propertyValue);
        }
    }
}
function* TRef(schema, references, path, value) {
    yield* Visit((0, deref_1.Deref)(schema, references), references, path, value);
}
function* TString(schema, references, path, value) {
    if (!(0, guard_1.IsString)(value))
        return yield Create(ValueErrorType.String, schema, path, value);
    if (IsDefined(schema.minLength) && !(value.length >= schema.minLength)) {
        yield Create(ValueErrorType.StringMinLength, schema, path, value);
    }
    if (IsDefined(schema.maxLength) && !(value.length <= schema.maxLength)) {
        yield Create(ValueErrorType.StringMaxLength, schema, path, value);
    }
    if ((0, guard_1.IsString)(schema.pattern)) {
        const regex = new RegExp(schema.pattern);
        if (!regex.test(value)) {
            yield Create(ValueErrorType.StringPattern, schema, path, value);
        }
    }
    if ((0, guard_1.IsString)(schema.format)) {
        if (!Types.FormatRegistry.Has(schema.format)) {
            yield Create(ValueErrorType.StringFormatUnknown, schema, path, value);
        }
        else {
            const format = Types.FormatRegistry.Get(schema.format);
            if (!format(value)) {
                yield Create(ValueErrorType.StringFormat, schema, path, value);
            }
        }
    }
}
function* TSymbol(schema, references, path, value) {
    if (!(0, guard_1.IsSymbol)(value))
        yield Create(ValueErrorType.Symbol, schema, path, value);
}
function* TTemplateLiteral(schema, references, path, value) {
    if (!(0, guard_1.IsString)(value))
        return yield Create(ValueErrorType.String, schema, path, value);
    const regex = new RegExp(schema.pattern);
    if (!regex.test(value)) {
        yield Create(ValueErrorType.StringPattern, schema, path, value);
    }
}
function* TThis(schema, references, path, value) {
    yield* Visit((0, deref_1.Deref)(schema, references), references, path, value);
}
function* TTuple(schema, references, path, value) {
    if (!(0, guard_1.IsArray)(value))
        return yield Create(ValueErrorType.Tuple, schema, path, value);
    if (schema.items === undefined && !(value.length === 0)) {
        return yield Create(ValueErrorType.TupleLength, schema, path, value);
    }
    if (!(value.length === schema.maxItems)) {
        return yield Create(ValueErrorType.TupleLength, schema, path, value);
    }
    if (!schema.items) {
        return;
    }
    for (let i = 0; i < schema.items.length; i++) {
        yield* Visit(schema.items[i], references, `${path}/${i}`, value[i]);
    }
}
function* TUndefined(schema, references, path, value) {
    if (!(0, guard_1.IsUndefined)(value))
        yield Create(ValueErrorType.Undefined, schema, path, value);
}
function* TUnion(schema, references, path, value) {
    let count = 0;
    for (const subschema of schema.anyOf) {
        const errors = [...Visit(subschema, references, path, value)];
        if (errors.length === 0)
            return; // matched
        count += errors.length;
    }
    if (count > 0) {
        yield Create(ValueErrorType.Union, schema, path, value);
    }
}
function* TUint8Array(schema, references, path, value) {
    if (!(0, guard_1.IsUint8Array)(value))
        return yield Create(ValueErrorType.Uint8Array, schema, path, value);
    if (IsDefined(schema.maxByteLength) && !(value.length <= schema.maxByteLength)) {
        yield Create(ValueErrorType.Uint8ArrayMaxByteLength, schema, path, value);
    }
    if (IsDefined(schema.minByteLength) && !(value.length >= schema.minByteLength)) {
        yield Create(ValueErrorType.Uint8ArrayMinByteLength, schema, path, value);
    }
}
function* TUnknown(schema, references, path, value) { }
function* TVoid(schema, references, path, value) {
    if (!system_1.TypeSystemPolicy.IsVoidLike(value))
        yield Create(ValueErrorType.Void, schema, path, value);
}
function* TKind(schema, references, path, value) {
    const check = Types.TypeRegistry.Get(schema[Types.Kind]);
    if (!check(schema, value))
        yield Create(ValueErrorType.Kind, schema, path, value);
}
function* Visit(schema, references, path, value) {
    const references_ = IsDefined(schema.$id) ? [...references, schema] : references;
    const schema_ = schema;
    switch (schema_[Types.Kind]) {
        case 'Any':
            return yield* TAny(schema_, references_, path, value);
        case 'Array':
            return yield* TArray(schema_, references_, path, value);
        case 'AsyncIterator':
            return yield* TAsyncIterator(schema_, references_, path, value);
        case 'BigInt':
            return yield* TBigInt(schema_, references_, path, value);
        case 'Boolean':
            return yield* TBoolean(schema_, references_, path, value);
        case 'Constructor':
            return yield* TConstructor(schema_, references_, path, value);
        case 'Date':
            return yield* TDate(schema_, references_, path, value);
        case 'Function':
            return yield* TFunction(schema_, references_, path, value);
        case 'Integer':
            return yield* TInteger(schema_, references_, path, value);
        case 'Intersect':
            return yield* TIntersect(schema_, references_, path, value);
        case 'Iterator':
            return yield* TIterator(schema_, references_, path, value);
        case 'Literal':
            return yield* TLiteral(schema_, references_, path, value);
        case 'Never':
            return yield* TNever(schema_, references_, path, value);
        case 'Not':
            return yield* TNot(schema_, references_, path, value);
        case 'Null':
            return yield* TNull(schema_, references_, path, value);
        case 'Number':
            return yield* TNumber(schema_, references_, path, value);
        case 'Object':
            return yield* TObject(schema_, references_, path, value);
        case 'Promise':
            return yield* TPromise(schema_, references_, path, value);
        case 'Record':
            return yield* TRecord(schema_, references_, path, value);
        case 'Ref':
            return yield* TRef(schema_, references_, path, value);
        case 'String':
            return yield* TString(schema_, references_, path, value);
        case 'Symbol':
            return yield* TSymbol(schema_, references_, path, value);
        case 'TemplateLiteral':
            return yield* TTemplateLiteral(schema_, references_, path, value);
        case 'This':
            return yield* TThis(schema_, references_, path, value);
        case 'Tuple':
            return yield* TTuple(schema_, references_, path, value);
        case 'Undefined':
            return yield* TUndefined(schema_, references_, path, value);
        case 'Union':
            return yield* TUnion(schema_, references_, path, value);
        case 'Uint8Array':
            return yield* TUint8Array(schema_, references_, path, value);
        case 'Unknown':
            return yield* TUnknown(schema_, references_, path, value);
        case 'Void':
            return yield* TVoid(schema_, references_, path, value);
        default:
            if (!Types.TypeRegistry.Has(schema_[Types.Kind]))
                throw new ValueErrorsUnknownTypeError(schema);
            return yield* TKind(schema_, references_, path, value);
    }
}
/** Returns an iterator for each error in this value. */
function Errors(...args) {
    const iterator = args.length === 3 ? Visit(args[0], args[1], '', args[2]) : Visit(args[0], [], '', args[1]);
    return new ValueErrorIterator(iterator);
}
exports.Errors = Errors;
