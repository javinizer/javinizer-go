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
exports.EncodeTransform = exports.DecodeTransform = exports.HasTransform = exports.TransformEncodeError = exports.TransformDecodeError = exports.TransformEncodeCheckError = exports.TransformDecodeCheckError = void 0;
const guard_1 = require("./guard");
const deref_1 = require("./deref");
const check_1 = require("./check");
const Types = require("../typebox");
// -------------------------------------------------------------------------
// Errors
// -------------------------------------------------------------------------
class TransformDecodeCheckError extends Types.TypeBoxError {
    constructor(schema, value, error) {
        super(`Unable to decode due to invalid value`);
        this.schema = schema;
        this.value = value;
        this.error = error;
    }
}
exports.TransformDecodeCheckError = TransformDecodeCheckError;
class TransformEncodeCheckError extends Types.TypeBoxError {
    constructor(schema, value, error) {
        super(`Unable to encode due to invalid value`);
        this.schema = schema;
        this.value = value;
        this.error = error;
    }
}
exports.TransformEncodeCheckError = TransformEncodeCheckError;
class TransformDecodeError extends Types.TypeBoxError {
    constructor(schema, value, error) {
        super(`${error instanceof Error ? error.message : 'Unknown error'}`);
        this.schema = schema;
        this.value = value;
    }
}
exports.TransformDecodeError = TransformDecodeError;
class TransformEncodeError extends Types.TypeBoxError {
    constructor(schema, value, error) {
        super(`${error instanceof Error ? error.message : 'Unknown error'}`);
        this.schema = schema;
        this.value = value;
    }
}
exports.TransformEncodeError = TransformEncodeError;
// ------------------------------------------------------------------
// HasTransform
// ------------------------------------------------------------------
/** Recursively checks a schema for transform codecs */
var HasTransform;
(function (HasTransform) {
    function TArray(schema, references) {
        return Types.TypeGuard.TTransform(schema) || Visit(schema.items, references);
    }
    function TAsyncIterator(schema, references) {
        return Types.TypeGuard.TTransform(schema) || Visit(schema.items, references);
    }
    function TConstructor(schema, references) {
        return Types.TypeGuard.TTransform(schema) || Visit(schema.returns, references) || schema.parameters.some((schema) => Visit(schema, references));
    }
    function TFunction(schema, references) {
        return Types.TypeGuard.TTransform(schema) || Visit(schema.returns, references) || schema.parameters.some((schema) => Visit(schema, references));
    }
    function TIntersect(schema, references) {
        return Types.TypeGuard.TTransform(schema) || Types.TypeGuard.TTransform(schema.unevaluatedProperties) || schema.allOf.some((schema) => Visit(schema, references));
    }
    function TIterator(schema, references) {
        return Types.TypeGuard.TTransform(schema) || Visit(schema.items, references);
    }
    function TNot(schema, references) {
        return Types.TypeGuard.TTransform(schema) || Visit(schema.not, references);
    }
    function TObject(schema, references) {
        // prettier-ignore
        return (Types.TypeGuard.TTransform(schema) || Object.values(schema.properties).some((schema) => Visit(schema, references)) || Types.TypeGuard.TSchema(schema.additionalProperties) && Visit(schema.additionalProperties, references));
    }
    function TPromise(schema, references) {
        return Types.TypeGuard.TTransform(schema) || Visit(schema.item, references);
    }
    function TRecord(schema, references) {
        const pattern = Object.getOwnPropertyNames(schema.patternProperties)[0];
        const property = schema.patternProperties[pattern];
        return Types.TypeGuard.TTransform(schema) || Visit(property, references) || (Types.TypeGuard.TSchema(schema.additionalProperties) && Types.TypeGuard.TTransform(schema.additionalProperties));
    }
    function TRef(schema, references) {
        if (Types.TypeGuard.TTransform(schema))
            return true;
        return Visit((0, deref_1.Deref)(schema, references), references);
    }
    function TThis(schema, references) {
        if (Types.TypeGuard.TTransform(schema))
            return true;
        return Visit((0, deref_1.Deref)(schema, references), references);
    }
    function TTuple(schema, references) {
        return Types.TypeGuard.TTransform(schema) || (!(0, guard_1.IsUndefined)(schema.items) && schema.items.some((schema) => Visit(schema, references)));
    }
    function TUnion(schema, references) {
        return Types.TypeGuard.TTransform(schema) || schema.anyOf.some((schema) => Visit(schema, references));
    }
    function Visit(schema, references) {
        const references_ = (0, guard_1.IsString)(schema.$id) ? [...references, schema] : references;
        const schema_ = schema;
        if (schema.$id && visited.has(schema.$id))
            return false;
        if (schema.$id)
            visited.add(schema.$id);
        switch (schema[Types.Kind]) {
            case 'Array':
                return TArray(schema_, references_);
            case 'AsyncIterator':
                return TAsyncIterator(schema_, references_);
            case 'Constructor':
                return TConstructor(schema_, references_);
            case 'Function':
                return TFunction(schema_, references_);
            case 'Intersect':
                return TIntersect(schema_, references_);
            case 'Iterator':
                return TIterator(schema_, references_);
            case 'Not':
                return TNot(schema_, references_);
            case 'Object':
                return TObject(schema_, references_);
            case 'Promise':
                return TPromise(schema_, references_);
            case 'Record':
                return TRecord(schema_, references_);
            case 'Ref':
                return TRef(schema_, references_);
            case 'This':
                return TThis(schema_, references_);
            case 'Tuple':
                return TTuple(schema_, references_);
            case 'Union':
                return TUnion(schema_, references_);
            default:
                return Types.TypeGuard.TTransform(schema);
        }
    }
    const visited = new Set();
    /** Returns true if this schema contains a transform codec */
    function Has(schema, references) {
        visited.clear();
        return Visit(schema, references);
    }
    HasTransform.Has = Has;
})(HasTransform || (exports.HasTransform = HasTransform = {}));
// ------------------------------------------------------------------
// DecodeTransform
// ------------------------------------------------------------------
/** Decodes a value using transform decoders if available. Does not ensure correct results. */
var DecodeTransform;
(function (DecodeTransform) {
    function Default(schema, value) {
        try {
            return Types.TypeGuard.TTransform(schema) ? schema[Types.Transform].Decode(value) : value;
        }
        catch (error) {
            throw new TransformDecodeError(schema, value, error);
        }
    }
    // prettier-ignore
    function TArray(schema, references, value) {
        return ((0, guard_1.IsArray)(value))
            ? Default(schema, value.map((value) => Visit(schema.items, references, value)))
            : Default(schema, value);
    }
    // prettier-ignore
    function TIntersect(schema, references, value) {
        if (!(0, guard_1.IsPlainObject)(value) || (0, guard_1.IsValueType)(value))
            return Default(schema, value);
        const knownKeys = Types.KeyResolver.ResolveKeys(schema, { includePatterns: false });
        const knownProperties = knownKeys.reduce((value, key) => {
            return (key in value)
                ? { ...value, [key]: Visit(Types.IndexedAccessor.Resolve(schema, [key]), references, value[key]) }
                : value;
        }, value);
        if (!Types.TypeGuard.TTransform(schema.unevaluatedProperties)) {
            return Default(schema, knownProperties);
        }
        const unknownKeys = Object.getOwnPropertyNames(knownProperties);
        const unevaluatedProperties = schema.unevaluatedProperties;
        const unknownProperties = unknownKeys.reduce((value, key) => {
            return !knownKeys.includes(key)
                ? { ...value, [key]: Default(unevaluatedProperties, value[key]) }
                : value;
        }, knownProperties);
        return Default(schema, unknownProperties);
    }
    function TNot(schema, references, value) {
        return Default(schema, Visit(schema.not, references, value));
    }
    // prettier-ignore
    function TObject(schema, references, value) {
        if (!(0, guard_1.IsPlainObject)(value))
            return Default(schema, value);
        const knownKeys = Types.KeyResolver.ResolveKeys(schema, { includePatterns: false });
        const knownProperties = knownKeys.reduce((value, key) => {
            return (key in value)
                ? { ...value, [key]: Visit(schema.properties[key], references, value[key]) }
                : value;
        }, value);
        if (!Types.TypeGuard.TSchema(schema.additionalProperties)) {
            return Default(schema, knownProperties);
        }
        const unknownKeys = Object.getOwnPropertyNames(knownProperties);
        const additionalProperties = schema.additionalProperties;
        const unknownProperties = unknownKeys.reduce((value, key) => {
            return !knownKeys.includes(key)
                ? { ...value, [key]: Default(additionalProperties, value[key]) }
                : value;
        }, knownProperties);
        return Default(schema, unknownProperties);
    }
    // prettier-ignore
    function TRecord(schema, references, value) {
        if (!(0, guard_1.IsPlainObject)(value))
            return Default(schema, value);
        const pattern = Object.getOwnPropertyNames(schema.patternProperties)[0];
        const knownKeys = new RegExp(pattern);
        const knownProperties = Object.getOwnPropertyNames(value).reduce((value, key) => {
            return knownKeys.test(key)
                ? { ...value, [key]: Visit(schema.patternProperties[pattern], references, value[key]) }
                : value;
        }, value);
        if (!Types.TypeGuard.TSchema(schema.additionalProperties)) {
            return Default(schema, knownProperties);
        }
        const unknownKeys = Object.getOwnPropertyNames(knownProperties);
        const additionalProperties = schema.additionalProperties;
        const unknownProperties = unknownKeys.reduce((value, key) => {
            return !knownKeys.test(key)
                ? { ...value, [key]: Default(additionalProperties, value[key]) }
                : value;
        }, knownProperties);
        return Default(schema, unknownProperties);
    }
    function TRef(schema, references, value) {
        const target = (0, deref_1.Deref)(schema, references);
        return Default(schema, Visit(target, references, value));
    }
    function TThis(schema, references, value) {
        const target = (0, deref_1.Deref)(schema, references);
        return Default(schema, Visit(target, references, value));
    }
    // prettier-ignore
    function TTuple(schema, references, value) {
        return ((0, guard_1.IsArray)(value) && (0, guard_1.IsArray)(schema.items))
            ? Default(schema, schema.items.map((schema, index) => Visit(schema, references, value[index])))
            : Default(schema, value);
    }
    function TUnion(schema, references, value) {
        const defaulted = Default(schema, value);
        for (const subschema of schema.anyOf) {
            if (!(0, check_1.Check)(subschema, references, defaulted))
                continue;
            return Visit(subschema, references, defaulted);
        }
        return defaulted;
    }
    function Visit(schema, references, value) {
        const references_ = typeof schema.$id === 'string' ? [...references, schema] : references;
        const schema_ = schema;
        switch (schema[Types.Kind]) {
            case 'Array':
                return TArray(schema_, references_, value);
            case 'Intersect':
                return TIntersect(schema_, references_, value);
            case 'Not':
                return TNot(schema_, references_, value);
            case 'Object':
                return TObject(schema_, references_, value);
            case 'Record':
                return TRecord(schema_, references_, value);
            case 'Ref':
                return TRef(schema_, references_, value);
            case 'Symbol':
                return Default(schema_, value);
            case 'This':
                return TThis(schema_, references_, value);
            case 'Tuple':
                return TTuple(schema_, references_, value);
            case 'Union':
                return TUnion(schema_, references_, value);
            default:
                return Default(schema_, value);
        }
    }
    function Decode(schema, references, value) {
        return Visit(schema, references, value);
    }
    DecodeTransform.Decode = Decode;
})(DecodeTransform || (exports.DecodeTransform = DecodeTransform = {}));
// ------------------------------------------------------------------
// DecodeTransform
// ------------------------------------------------------------------
/** Encodes a value using transform encoders if available. Does not ensure correct results. */
var EncodeTransform;
(function (EncodeTransform) {
    function Default(schema, value) {
        try {
            return Types.TypeGuard.TTransform(schema) ? schema[Types.Transform].Encode(value) : value;
        }
        catch (error) {
            throw new TransformEncodeError(schema, value, error);
        }
    }
    // prettier-ignore
    function TArray(schema, references, value) {
        const defaulted = Default(schema, value);
        return (0, guard_1.IsArray)(defaulted)
            ? defaulted.map((value) => Visit(schema.items, references, value))
            : defaulted;
    }
    // prettier-ignore
    function TIntersect(schema, references, value) {
        const defaulted = Default(schema, value);
        if (!(0, guard_1.IsPlainObject)(value) || (0, guard_1.IsValueType)(value))
            return defaulted;
        const knownKeys = Types.KeyResolver.ResolveKeys(schema, { includePatterns: false });
        const knownProperties = knownKeys.reduce((value, key) => {
            return key in defaulted
                ? { ...value, [key]: Visit(Types.IndexedAccessor.Resolve(schema, [key]), references, value[key]) }
                : value;
        }, defaulted);
        if (!Types.TypeGuard.TTransform(schema.unevaluatedProperties)) {
            return Default(schema, knownProperties);
        }
        const unknownKeys = Object.getOwnPropertyNames(knownProperties);
        const unevaluatedProperties = schema.unevaluatedProperties;
        return unknownKeys.reduce((value, key) => {
            return !knownKeys.includes(key)
                ? { ...value, [key]: Default(unevaluatedProperties, value[key]) }
                : value;
        }, knownProperties);
    }
    function TNot(schema, references, value) {
        return Default(schema.not, Default(schema, value));
    }
    // prettier-ignore
    function TObject(schema, references, value) {
        const defaulted = Default(schema, value);
        if (!(0, guard_1.IsPlainObject)(value))
            return defaulted;
        const knownKeys = Types.KeyResolver.ResolveKeys(schema, { includePatterns: false });
        const knownProperties = knownKeys.reduce((value, key) => {
            return key in value
                ? { ...value, [key]: Visit(schema.properties[key], references, value[key]) }
                : value;
        }, defaulted);
        if (!Types.TypeGuard.TSchema(schema.additionalProperties)) {
            return knownProperties;
        }
        const unknownKeys = Object.getOwnPropertyNames(knownProperties);
        const additionalProperties = schema.additionalProperties;
        return unknownKeys.reduce((value, key) => {
            return !knownKeys.includes(key)
                ? { ...value, [key]: Default(additionalProperties, value[key]) }
                : value;
        }, knownProperties);
    }
    // prettier-ignore
    function TRecord(schema, references, value) {
        const defaulted = Default(schema, value);
        if (!(0, guard_1.IsPlainObject)(value))
            return defaulted;
        const pattern = Object.getOwnPropertyNames(schema.patternProperties)[0];
        const knownKeys = new RegExp(pattern);
        const knownProperties = Object.getOwnPropertyNames(value).reduce((value, key) => {
            return knownKeys.test(key)
                ? { ...value, [key]: Visit(schema.patternProperties[pattern], references, value[key]) }
                : value;
        }, defaulted);
        if (!Types.TypeGuard.TSchema(schema.additionalProperties)) {
            return Default(schema, knownProperties);
        }
        const unknownKeys = Object.getOwnPropertyNames(knownProperties);
        const additionalProperties = schema.additionalProperties;
        return unknownKeys.reduce((value, key) => {
            return !knownKeys.test(key)
                ? { ...value, [key]: Default(additionalProperties, value[key]) }
                : value;
        }, knownProperties);
    }
    function TRef(schema, references, value) {
        const target = (0, deref_1.Deref)(schema, references);
        const resolved = Visit(target, references, value);
        return Default(schema, resolved);
    }
    function TThis(schema, references, value) {
        const target = (0, deref_1.Deref)(schema, references);
        const resolved = Visit(target, references, value);
        return Default(schema, resolved);
    }
    function TTuple(schema, references, value) {
        const value1 = Default(schema, value);
        return (0, guard_1.IsArray)(schema.items) ? schema.items.map((schema, index) => Visit(schema, references, value1[index])) : [];
    }
    function TUnion(schema, references, value) {
        // test value against union variants
        for (const subschema of schema.anyOf) {
            if (!(0, check_1.Check)(subschema, references, value))
                continue;
            const value1 = Visit(subschema, references, value);
            return Default(schema, value1);
        }
        // test transformed value against union variants
        for (const subschema of schema.anyOf) {
            const value1 = Visit(subschema, references, value);
            if (!(0, check_1.Check)(schema, references, value1))
                continue;
            return Default(schema, value1);
        }
        return Default(schema, value);
    }
    function Visit(schema, references, value) {
        const references_ = typeof schema.$id === 'string' ? [...references, schema] : references;
        const schema_ = schema;
        switch (schema[Types.Kind]) {
            case 'Array':
                return TArray(schema_, references_, value);
            case 'Intersect':
                return TIntersect(schema_, references_, value);
            case 'Not':
                return TNot(schema_, references_, value);
            case 'Object':
                return TObject(schema_, references_, value);
            case 'Record':
                return TRecord(schema_, references_, value);
            case 'Ref':
                return TRef(schema_, references_, value);
            case 'This':
                return TThis(schema_, references_, value);
            case 'Tuple':
                return TTuple(schema_, references_, value);
            case 'Union':
                return TUnion(schema_, references_, value);
            default:
                return Default(schema_, value);
        }
    }
    function Encode(schema, references, value) {
        return Visit(schema, references, value);
    }
    EncodeTransform.Encode = Encode;
})(EncodeTransform || (exports.EncodeTransform = EncodeTransform = {}));
