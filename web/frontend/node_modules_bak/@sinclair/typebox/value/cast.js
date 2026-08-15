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
exports.Cast = exports.Default = exports.DefaultClone = exports.ValueCastUnknownTypeError = exports.ValueCastRecursiveTypeError = exports.ValueCastNeverTypeError = exports.ValueCastArrayUniqueItemsTypeError = void 0;
const guard_1 = require("./guard");
const create_1 = require("./create");
const check_1 = require("./check");
const clone_1 = require("./clone");
const deref_1 = require("./deref");
const Types = require("../typebox");
// --------------------------------------------------------------------------
// Errors
// --------------------------------------------------------------------------
class ValueCastArrayUniqueItemsTypeError extends Types.TypeBoxError {
    constructor(schema, value) {
        super('Array cast produced invalid data due to uniqueItems constraint');
        this.schema = schema;
        this.value = value;
    }
}
exports.ValueCastArrayUniqueItemsTypeError = ValueCastArrayUniqueItemsTypeError;
class ValueCastNeverTypeError extends Types.TypeBoxError {
    constructor(schema) {
        super('Never types cannot be cast');
        this.schema = schema;
    }
}
exports.ValueCastNeverTypeError = ValueCastNeverTypeError;
class ValueCastRecursiveTypeError extends Types.TypeBoxError {
    constructor(schema) {
        super('Cannot cast recursive schemas');
        this.schema = schema;
    }
}
exports.ValueCastRecursiveTypeError = ValueCastRecursiveTypeError;
class ValueCastUnknownTypeError extends Types.TypeBoxError {
    constructor(schema) {
        super('Unknown type');
        this.schema = schema;
    }
}
exports.ValueCastUnknownTypeError = ValueCastUnknownTypeError;
// --------------------------------------------------------------------------
// The following will score a schema against a value. For objects, the score
// is the tally of points awarded for each property of the value. Property
// points are (1.0 / propertyCount) to prevent large property counts biasing
// results. Properties that match literal values are maximally awarded as
// literals are typically used as union discriminator fields.
// --------------------------------------------------------------------------
var UnionCastCreate;
(function (UnionCastCreate) {
    function Score(schema, references, value) {
        if (schema[Types.Kind] === 'Object' && typeof value === 'object' && !(0, guard_1.IsNull)(value)) {
            const object = schema;
            const keys = Object.getOwnPropertyNames(value);
            const entries = Object.entries(object.properties);
            const [point, max] = [1 / entries.length, entries.length];
            return entries.reduce((acc, [key, schema]) => {
                const literal = schema[Types.Kind] === 'Literal' && schema.const === value[key] ? max : 0;
                const checks = (0, check_1.Check)(schema, references, value[key]) ? point : 0;
                const exists = keys.includes(key) ? point : 0;
                return acc + (literal + checks + exists);
            }, 0);
        }
        else {
            return (0, check_1.Check)(schema, references, value) ? 1 : 0;
        }
    }
    function Select(union, references, value) {
        let [select, best] = [union.anyOf[0], 0];
        for (const schema of union.anyOf) {
            const score = Score(schema, references, value);
            if (score > best) {
                select = schema;
                best = score;
            }
        }
        return select;
    }
    function Create(union, references, value) {
        if ('default' in union) {
            return union.default;
        }
        else {
            const schema = Select(union, references, value);
            return Cast(schema, references, value);
        }
    }
    UnionCastCreate.Create = Create;
})(UnionCastCreate || (UnionCastCreate = {}));
// --------------------------------------------------------------------------
// Default
// --------------------------------------------------------------------------
function DefaultClone(schema, references, value) {
    return (0, check_1.Check)(schema, references, value) ? (0, clone_1.Clone)(value) : (0, create_1.Create)(schema, references);
}
exports.DefaultClone = DefaultClone;
function Default(schema, references, value) {
    return (0, check_1.Check)(schema, references, value) ? value : (0, create_1.Create)(schema, references);
}
exports.Default = Default;
// --------------------------------------------------------------------------
// Cast
// --------------------------------------------------------------------------
function TArray(schema, references, value) {
    if ((0, check_1.Check)(schema, references, value))
        return (0, clone_1.Clone)(value);
    const created = (0, guard_1.IsArray)(value) ? (0, clone_1.Clone)(value) : (0, create_1.Create)(schema, references);
    const minimum = (0, guard_1.IsNumber)(schema.minItems) && created.length < schema.minItems ? [...created, ...Array.from({ length: schema.minItems - created.length }, () => null)] : created;
    const maximum = (0, guard_1.IsNumber)(schema.maxItems) && minimum.length > schema.maxItems ? minimum.slice(0, schema.maxItems) : minimum;
    const casted = maximum.map((value) => Visit(schema.items, references, value));
    if (schema.uniqueItems !== true)
        return casted;
    const unique = [...new Set(casted)];
    if (!(0, check_1.Check)(schema, references, unique))
        throw new ValueCastArrayUniqueItemsTypeError(schema, unique);
    return unique;
}
function TConstructor(schema, references, value) {
    if ((0, check_1.Check)(schema, references, value))
        return (0, create_1.Create)(schema, references);
    const required = new Set(schema.returns.required || []);
    const result = function () { };
    for (const [key, property] of Object.entries(schema.returns.properties)) {
        if (!required.has(key) && value.prototype[key] === undefined)
            continue;
        result.prototype[key] = Visit(property, references, value.prototype[key]);
    }
    return result;
}
function TIntersect(schema, references, value) {
    const created = (0, create_1.Create)(schema, references);
    const mapped = (0, guard_1.IsPlainObject)(created) && (0, guard_1.IsPlainObject)(value) ? { ...created, ...value } : value;
    return (0, check_1.Check)(schema, references, mapped) ? mapped : (0, create_1.Create)(schema, references);
}
function TNever(schema, references, value) {
    throw new ValueCastNeverTypeError(schema);
}
function TObject(schema, references, value) {
    if ((0, check_1.Check)(schema, references, value))
        return value;
    if (value === null || typeof value !== 'object')
        return (0, create_1.Create)(schema, references);
    const required = new Set(schema.required || []);
    const result = {};
    for (const [key, property] of Object.entries(schema.properties)) {
        if (!required.has(key) && value[key] === undefined)
            continue;
        result[key] = Visit(property, references, value[key]);
    }
    // additional schema properties
    if (typeof schema.additionalProperties === 'object') {
        const propertyNames = Object.getOwnPropertyNames(schema.properties);
        for (const propertyName of Object.getOwnPropertyNames(value)) {
            if (propertyNames.includes(propertyName))
                continue;
            result[propertyName] = Visit(schema.additionalProperties, references, value[propertyName]);
        }
    }
    return result;
}
function TRecord(schema, references, value) {
    if ((0, check_1.Check)(schema, references, value))
        return (0, clone_1.Clone)(value);
    if (value === null || typeof value !== 'object' || Array.isArray(value) || value instanceof Date)
        return (0, create_1.Create)(schema, references);
    const subschemaPropertyName = Object.getOwnPropertyNames(schema.patternProperties)[0];
    const subschema = schema.patternProperties[subschemaPropertyName];
    const result = {};
    for (const [propKey, propValue] of Object.entries(value)) {
        result[propKey] = Visit(subschema, references, propValue);
    }
    return result;
}
function TRef(schema, references, value) {
    return Visit((0, deref_1.Deref)(schema, references), references, value);
}
function TThis(schema, references, value) {
    return Visit((0, deref_1.Deref)(schema, references), references, value);
}
function TTuple(schema, references, value) {
    if ((0, check_1.Check)(schema, references, value))
        return (0, clone_1.Clone)(value);
    if (!(0, guard_1.IsArray)(value))
        return (0, create_1.Create)(schema, references);
    if (schema.items === undefined)
        return [];
    return schema.items.map((schema, index) => Visit(schema, references, value[index]));
}
function TUnion(schema, references, value) {
    return (0, check_1.Check)(schema, references, value) ? (0, clone_1.Clone)(value) : UnionCastCreate.Create(schema, references, value);
}
function Visit(schema, references, value) {
    const references_ = (0, guard_1.IsString)(schema.$id) ? [...references, schema] : references;
    const schema_ = schema;
    switch (schema[Types.Kind]) {
        // ------------------------------------------------------
        // Structural
        // ------------------------------------------------------
        case 'Array':
            return TArray(schema_, references_, value);
        case 'Constructor':
            return TConstructor(schema_, references_, value);
        case 'Intersect':
            return TIntersect(schema_, references_, value);
        case 'Never':
            return TNever(schema_, references_, value);
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
        // ------------------------------------------------------
        // DefaultClone
        // ------------------------------------------------------
        case 'Date':
        case 'Symbol':
        case 'Uint8Array':
            return DefaultClone(schema, references, value);
        // ------------------------------------------------------
        // Default
        // ------------------------------------------------------
        case 'Any':
        case 'AsyncIterator':
        case 'BigInt':
        case 'Boolean':
        case 'Function':
        case 'Integer':
        case 'Iterator':
        case 'Literal':
        case 'Not':
        case 'Null':
        case 'Number':
        case 'Promise':
        case 'String':
        case 'TemplateLiteral':
        case 'Undefined':
        case 'Unknown':
        case 'Void':
            return Default(schema_, references_, value);
        default:
            if (!Types.TypeRegistry.Has(schema_[Types.Kind]))
                throw new ValueCastUnknownTypeError(schema_);
            return Default(schema_, references_, value);
    }
}
/** Casts a value into a given type. The return value will retain as much information of the original value as possible. */
function Cast(...args) {
    return args.length === 3 ? Visit(args[0], args[1], args[2]) : Visit(args[0], [], args[1]);
}
exports.Cast = Cast;
