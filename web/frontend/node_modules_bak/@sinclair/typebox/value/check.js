"use strict";
/*--------------------------------------------------------------------------

@sinclair/typebox/value

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
exports.Check = exports.ValueCheckUnknownTypeError = void 0;
const guard_1 = require("./guard");
const index_1 = require("../system/index");
const deref_1 = require("./deref");
const hash_1 = require("./hash");
const Types = require("../typebox");
// --------------------------------------------------------------------------
// Errors
// --------------------------------------------------------------------------
class ValueCheckUnknownTypeError extends Types.TypeBoxError {
    constructor(schema) {
        super(`Unknown type`);
        this.schema = schema;
    }
}
exports.ValueCheckUnknownTypeError = ValueCheckUnknownTypeError;
// --------------------------------------------------------------------------
// TypeGuards
// --------------------------------------------------------------------------
function IsAnyOrUnknown(schema) {
    return schema[Types.Kind] === 'Any' || schema[Types.Kind] === 'Unknown';
}
// --------------------------------------------------------------------------
// Guards
// --------------------------------------------------------------------------
function IsDefined(value) {
    return value !== undefined;
}
// --------------------------------------------------------------------------
// Types
// --------------------------------------------------------------------------
function TAny(schema, references, value) {
    return true;
}
function TArray(schema, references, value) {
    if (!(0, guard_1.IsArray)(value))
        return false;
    if (IsDefined(schema.minItems) && !(value.length >= schema.minItems)) {
        return false;
    }
    if (IsDefined(schema.maxItems) && !(value.length <= schema.maxItems)) {
        return false;
    }
    if (!value.every((value) => Visit(schema.items, references, value))) {
        return false;
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
        return false;
    }
    // contains
    if (!(IsDefined(schema.contains) || (0, guard_1.IsNumber)(schema.minContains) || (0, guard_1.IsNumber)(schema.maxContains))) {
        return true; // exit
    }
    const containsSchema = IsDefined(schema.contains) ? schema.contains : Types.Type.Never();
    const containsCount = value.reduce((acc, value) => (Visit(containsSchema, references, value) ? acc + 1 : acc), 0);
    if (containsCount === 0) {
        return false;
    }
    if ((0, guard_1.IsNumber)(schema.minContains) && containsCount < schema.minContains) {
        return false;
    }
    if ((0, guard_1.IsNumber)(schema.maxContains) && containsCount > schema.maxContains) {
        return false;
    }
    return true;
}
function TAsyncIterator(schema, references, value) {
    return (0, guard_1.IsAsyncIterator)(value);
}
function TBigInt(schema, references, value) {
    if (!(0, guard_1.IsBigInt)(value))
        return false;
    if (IsDefined(schema.exclusiveMaximum) && !(value < schema.exclusiveMaximum)) {
        return false;
    }
    if (IsDefined(schema.exclusiveMinimum) && !(value > schema.exclusiveMinimum)) {
        return false;
    }
    if (IsDefined(schema.maximum) && !(value <= schema.maximum)) {
        return false;
    }
    if (IsDefined(schema.minimum) && !(value >= schema.minimum)) {
        return false;
    }
    if (IsDefined(schema.multipleOf) && !(value % schema.multipleOf === BigInt(0))) {
        return false;
    }
    return true;
}
function TBoolean(schema, references, value) {
    return (0, guard_1.IsBoolean)(value);
}
function TConstructor(schema, references, value) {
    return Visit(schema.returns, references, value.prototype);
}
function TDate(schema, references, value) {
    if (!(0, guard_1.IsDate)(value))
        return false;
    if (IsDefined(schema.exclusiveMaximumTimestamp) && !(value.getTime() < schema.exclusiveMaximumTimestamp)) {
        return false;
    }
    if (IsDefined(schema.exclusiveMinimumTimestamp) && !(value.getTime() > schema.exclusiveMinimumTimestamp)) {
        return false;
    }
    if (IsDefined(schema.maximumTimestamp) && !(value.getTime() <= schema.maximumTimestamp)) {
        return false;
    }
    if (IsDefined(schema.minimumTimestamp) && !(value.getTime() >= schema.minimumTimestamp)) {
        return false;
    }
    if (IsDefined(schema.multipleOfTimestamp) && !(value.getTime() % schema.multipleOfTimestamp === 0)) {
        return false;
    }
    return true;
}
function TFunction(schema, references, value) {
    return (0, guard_1.IsFunction)(value);
}
function TInteger(schema, references, value) {
    if (!(0, guard_1.IsInteger)(value)) {
        return false;
    }
    if (IsDefined(schema.exclusiveMaximum) && !(value < schema.exclusiveMaximum)) {
        return false;
    }
    if (IsDefined(schema.exclusiveMinimum) && !(value > schema.exclusiveMinimum)) {
        return false;
    }
    if (IsDefined(schema.maximum) && !(value <= schema.maximum)) {
        return false;
    }
    if (IsDefined(schema.minimum) && !(value >= schema.minimum)) {
        return false;
    }
    if (IsDefined(schema.multipleOf) && !(value % schema.multipleOf === 0)) {
        return false;
    }
    return true;
}
function TIntersect(schema, references, value) {
    const check1 = schema.allOf.every((schema) => Visit(schema, references, value));
    if (schema.unevaluatedProperties === false) {
        const keyPattern = new RegExp(Types.KeyResolver.ResolvePattern(schema));
        const check2 = Object.getOwnPropertyNames(value).every((key) => keyPattern.test(key));
        return check1 && check2;
    }
    else if (Types.TypeGuard.TSchema(schema.unevaluatedProperties)) {
        const keyCheck = new RegExp(Types.KeyResolver.ResolvePattern(schema));
        const check2 = Object.getOwnPropertyNames(value).every((key) => keyCheck.test(key) || Visit(schema.unevaluatedProperties, references, value[key]));
        return check1 && check2;
    }
    else {
        return check1;
    }
}
function TIterator(schema, references, value) {
    return (0, guard_1.IsIterator)(value);
}
function TLiteral(schema, references, value) {
    return value === schema.const;
}
function TNever(schema, references, value) {
    return false;
}
function TNot(schema, references, value) {
    return !Visit(schema.not, references, value);
}
function TNull(schema, references, value) {
    return (0, guard_1.IsNull)(value);
}
function TNumber(schema, references, value) {
    if (!index_1.TypeSystemPolicy.IsNumberLike(value))
        return false;
    if (IsDefined(schema.exclusiveMaximum) && !(value < schema.exclusiveMaximum)) {
        return false;
    }
    if (IsDefined(schema.exclusiveMinimum) && !(value > schema.exclusiveMinimum)) {
        return false;
    }
    if (IsDefined(schema.minimum) && !(value >= schema.minimum)) {
        return false;
    }
    if (IsDefined(schema.maximum) && !(value <= schema.maximum)) {
        return false;
    }
    if (IsDefined(schema.multipleOf) && !(value % schema.multipleOf === 0)) {
        return false;
    }
    return true;
}
function TObject(schema, references, value) {
    if (!index_1.TypeSystemPolicy.IsObjectLike(value))
        return false;
    if (IsDefined(schema.minProperties) && !(Object.getOwnPropertyNames(value).length >= schema.minProperties)) {
        return false;
    }
    if (IsDefined(schema.maxProperties) && !(Object.getOwnPropertyNames(value).length <= schema.maxProperties)) {
        return false;
    }
    const knownKeys = Object.getOwnPropertyNames(schema.properties);
    for (const knownKey of knownKeys) {
        const property = schema.properties[knownKey];
        if (schema.required && schema.required.includes(knownKey)) {
            if (!Visit(property, references, value[knownKey])) {
                return false;
            }
            if ((Types.ExtendsUndefined.Check(property) || IsAnyOrUnknown(property)) && !(knownKey in value)) {
                return false;
            }
        }
        else {
            if (index_1.TypeSystemPolicy.IsExactOptionalProperty(value, knownKey) && !Visit(property, references, value[knownKey])) {
                return false;
            }
        }
    }
    if (schema.additionalProperties === false) {
        const valueKeys = Object.getOwnPropertyNames(value);
        // optimization: value is valid if schemaKey length matches the valueKey length
        if (schema.required && schema.required.length === knownKeys.length && valueKeys.length === knownKeys.length) {
            return true;
        }
        else {
            return valueKeys.every((valueKey) => knownKeys.includes(valueKey));
        }
    }
    else if (typeof schema.additionalProperties === 'object') {
        const valueKeys = Object.getOwnPropertyNames(value);
        return valueKeys.every((key) => knownKeys.includes(key) || Visit(schema.additionalProperties, references, value[key]));
    }
    else {
        return true;
    }
}
function TPromise(schema, references, value) {
    return (0, guard_1.IsPromise)(value);
}
function TRecord(schema, references, value) {
    if (!index_1.TypeSystemPolicy.IsRecordLike(value)) {
        return false;
    }
    if (IsDefined(schema.minProperties) && !(Object.getOwnPropertyNames(value).length >= schema.minProperties)) {
        return false;
    }
    if (IsDefined(schema.maxProperties) && !(Object.getOwnPropertyNames(value).length <= schema.maxProperties)) {
        return false;
    }
    const [patternKey, patternSchema] = Object.entries(schema.patternProperties)[0];
    const regex = new RegExp(patternKey);
    // prettier-ignore
    const check1 = Object.entries(value).every(([key, value]) => {
        return (regex.test(key)) ? Visit(patternSchema, references, value) : true;
    });
    // prettier-ignore
    const check2 = typeof schema.additionalProperties === 'object' ? Object.entries(value).every(([key, value]) => {
        return (!regex.test(key)) ? Visit(schema.additionalProperties, references, value) : true;
    }) : true;
    const check3 = schema.additionalProperties === false
        ? Object.getOwnPropertyNames(value).every((key) => {
            return regex.test(key);
        })
        : true;
    return check1 && check2 && check3;
}
function TRef(schema, references, value) {
    return Visit((0, deref_1.Deref)(schema, references), references, value);
}
function TString(schema, references, value) {
    if (!(0, guard_1.IsString)(value)) {
        return false;
    }
    if (IsDefined(schema.minLength)) {
        if (!(value.length >= schema.minLength))
            return false;
    }
    if (IsDefined(schema.maxLength)) {
        if (!(value.length <= schema.maxLength))
            return false;
    }
    if (IsDefined(schema.pattern)) {
        const regex = new RegExp(schema.pattern);
        if (!regex.test(value))
            return false;
    }
    if (IsDefined(schema.format)) {
        if (!Types.FormatRegistry.Has(schema.format))
            return false;
        const func = Types.FormatRegistry.Get(schema.format);
        return func(value);
    }
    return true;
}
function TSymbol(schema, references, value) {
    return (0, guard_1.IsSymbol)(value);
}
function TTemplateLiteral(schema, references, value) {
    return (0, guard_1.IsString)(value) && new RegExp(schema.pattern).test(value);
}
function TThis(schema, references, value) {
    return Visit((0, deref_1.Deref)(schema, references), references, value);
}
function TTuple(schema, references, value) {
    if (!(0, guard_1.IsArray)(value)) {
        return false;
    }
    if (schema.items === undefined && !(value.length === 0)) {
        return false;
    }
    if (!(value.length === schema.maxItems)) {
        return false;
    }
    if (!schema.items) {
        return true;
    }
    for (let i = 0; i < schema.items.length; i++) {
        if (!Visit(schema.items[i], references, value[i]))
            return false;
    }
    return true;
}
function TUndefined(schema, references, value) {
    return (0, guard_1.IsUndefined)(value);
}
function TUnion(schema, references, value) {
    return schema.anyOf.some((inner) => Visit(inner, references, value));
}
function TUint8Array(schema, references, value) {
    if (!(0, guard_1.IsUint8Array)(value)) {
        return false;
    }
    if (IsDefined(schema.maxByteLength) && !(value.length <= schema.maxByteLength)) {
        return false;
    }
    if (IsDefined(schema.minByteLength) && !(value.length >= schema.minByteLength)) {
        return false;
    }
    return true;
}
function TUnknown(schema, references, value) {
    return true;
}
function TVoid(schema, references, value) {
    return index_1.TypeSystemPolicy.IsVoidLike(value);
}
function TKind(schema, references, value) {
    if (!Types.TypeRegistry.Has(schema[Types.Kind]))
        return false;
    const func = Types.TypeRegistry.Get(schema[Types.Kind]);
    return func(schema, value);
}
function Visit(schema, references, value) {
    const references_ = IsDefined(schema.$id) ? [...references, schema] : references;
    const schema_ = schema;
    switch (schema_[Types.Kind]) {
        case 'Any':
            return TAny(schema_, references_, value);
        case 'Array':
            return TArray(schema_, references_, value);
        case 'AsyncIterator':
            return TAsyncIterator(schema_, references_, value);
        case 'BigInt':
            return TBigInt(schema_, references_, value);
        case 'Boolean':
            return TBoolean(schema_, references_, value);
        case 'Constructor':
            return TConstructor(schema_, references_, value);
        case 'Date':
            return TDate(schema_, references_, value);
        case 'Function':
            return TFunction(schema_, references_, value);
        case 'Integer':
            return TInteger(schema_, references_, value);
        case 'Intersect':
            return TIntersect(schema_, references_, value);
        case 'Iterator':
            return TIterator(schema_, references_, value);
        case 'Literal':
            return TLiteral(schema_, references_, value);
        case 'Never':
            return TNever(schema_, references_, value);
        case 'Not':
            return TNot(schema_, references_, value);
        case 'Null':
            return TNull(schema_, references_, value);
        case 'Number':
            return TNumber(schema_, references_, value);
        case 'Object':
            return TObject(schema_, references_, value);
        case 'Promise':
            return TPromise(schema_, references_, value);
        case 'Record':
            return TRecord(schema_, references_, value);
        case 'Ref':
            return TRef(schema_, references_, value);
        case 'String':
            return TString(schema_, references_, value);
        case 'Symbol':
            return TSymbol(schema_, references_, value);
        case 'TemplateLiteral':
            return TTemplateLiteral(schema_, references_, value);
        case 'This':
            return TThis(schema_, references_, value);
        case 'Tuple':
            return TTuple(schema_, references_, value);
        case 'Undefined':
            return TUndefined(schema_, references_, value);
        case 'Union':
            return TUnion(schema_, references_, value);
        case 'Uint8Array':
            return TUint8Array(schema_, references_, value);
        case 'Unknown':
            return TUnknown(schema_, references_, value);
        case 'Void':
            return TVoid(schema_, references_, value);
        default:
            if (!Types.TypeRegistry.Has(schema_[Types.Kind]))
                throw new ValueCheckUnknownTypeError(schema_);
            return TKind(schema_, references_, value);
    }
}
/** Returns true if the value matches the given type. */
function Check(...args) {
    return args.length === 3 ? Visit(args[0], args[1], args[2]) : Visit(args[0], [], args[1]);
}
exports.Check = Check;
