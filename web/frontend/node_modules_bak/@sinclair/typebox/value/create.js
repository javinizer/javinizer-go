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
exports.Create = exports.ValueCreateRecursiveInstantiationError = exports.ValueCreateTempateLiteralTypeError = exports.ValueCreateIntersectTypeError = exports.ValueCreateNotTypeError = exports.ValueCreateNeverTypeError = exports.ValueCreateUnknownTypeError = void 0;
const guard_1 = require("./guard");
const check_1 = require("./check");
const deref_1 = require("./deref");
const Types = require("../typebox");
// --------------------------------------------------------------------------
// Errors
// --------------------------------------------------------------------------
class ValueCreateUnknownTypeError extends Types.TypeBoxError {
    constructor(schema) {
        super('Unknown type');
        this.schema = schema;
    }
}
exports.ValueCreateUnknownTypeError = ValueCreateUnknownTypeError;
class ValueCreateNeverTypeError extends Types.TypeBoxError {
    constructor(schema) {
        super('Never types cannot be created');
        this.schema = schema;
    }
}
exports.ValueCreateNeverTypeError = ValueCreateNeverTypeError;
class ValueCreateNotTypeError extends Types.TypeBoxError {
    constructor(schema) {
        super('Not types must have a default value');
        this.schema = schema;
    }
}
exports.ValueCreateNotTypeError = ValueCreateNotTypeError;
class ValueCreateIntersectTypeError extends Types.TypeBoxError {
    constructor(schema) {
        super('Intersect produced invalid value. Consider using a default value.');
        this.schema = schema;
    }
}
exports.ValueCreateIntersectTypeError = ValueCreateIntersectTypeError;
class ValueCreateTempateLiteralTypeError extends Types.TypeBoxError {
    constructor(schema) {
        super('Can only create template literal values from patterns that produce finite sequences. Consider using a default value.');
        this.schema = schema;
    }
}
exports.ValueCreateTempateLiteralTypeError = ValueCreateTempateLiteralTypeError;
class ValueCreateRecursiveInstantiationError extends Types.TypeBoxError {
    constructor(schema, recursiveMaxDepth) {
        super('Value cannot be created as recursive type may produce value of infinite size. Consider using a default.');
        this.schema = schema;
        this.recursiveMaxDepth = recursiveMaxDepth;
    }
}
exports.ValueCreateRecursiveInstantiationError = ValueCreateRecursiveInstantiationError;
// --------------------------------------------------------------------------
// Types
// --------------------------------------------------------------------------
function TAny(schema, references) {
    if ((0, guard_1.HasPropertyKey)(schema, 'default')) {
        return schema.default;
    }
    else {
        return {};
    }
}
function TArray(schema, references) {
    if (schema.uniqueItems === true && !(0, guard_1.HasPropertyKey)(schema, 'default')) {
        throw new Error('ValueCreate.Array: Array with the uniqueItems constraint requires a default value');
    }
    else if ('contains' in schema && !(0, guard_1.HasPropertyKey)(schema, 'default')) {
        throw new Error('ValueCreate.Array: Array with the contains constraint requires a default value');
    }
    else if ('default' in schema) {
        return schema.default;
    }
    else if (schema.minItems !== undefined) {
        return Array.from({ length: schema.minItems }).map((item) => {
            return Visit(schema.items, references);
        });
    }
    else {
        return [];
    }
}
function TAsyncIterator(schema, references) {
    if ((0, guard_1.HasPropertyKey)(schema, 'default')) {
        return schema.default;
    }
    else {
        return (async function* () { })();
    }
}
function TBigInt(schema, references) {
    if ((0, guard_1.HasPropertyKey)(schema, 'default')) {
        return schema.default;
    }
    else {
        return BigInt(0);
    }
}
function TBoolean(schema, references) {
    if ((0, guard_1.HasPropertyKey)(schema, 'default')) {
        return schema.default;
    }
    else {
        return false;
    }
}
function TConstructor(schema, references) {
    if ((0, guard_1.HasPropertyKey)(schema, 'default')) {
        return schema.default;
    }
    else {
        const value = Visit(schema.returns, references);
        if (typeof value === 'object' && !Array.isArray(value)) {
            return class {
                constructor() {
                    for (const [key, val] of Object.entries(value)) {
                        const self = this;
                        self[key] = val;
                    }
                }
            };
        }
        else {
            return class {
            };
        }
    }
}
function TDate(schema, references) {
    if ((0, guard_1.HasPropertyKey)(schema, 'default')) {
        return schema.default;
    }
    else if (schema.minimumTimestamp !== undefined) {
        return new Date(schema.minimumTimestamp);
    }
    else {
        return new Date();
    }
}
function TFunction(schema, references) {
    if ((0, guard_1.HasPropertyKey)(schema, 'default')) {
        return schema.default;
    }
    else {
        return () => Visit(schema.returns, references);
    }
}
function TInteger(schema, references) {
    if ((0, guard_1.HasPropertyKey)(schema, 'default')) {
        return schema.default;
    }
    else if (schema.minimum !== undefined) {
        return schema.minimum;
    }
    else {
        return 0;
    }
}
function TIntersect(schema, references) {
    if ((0, guard_1.HasPropertyKey)(schema, 'default')) {
        return schema.default;
    }
    else {
        // Note: The best we can do here is attempt to instance each sub type and apply through object assign. For non-object
        // sub types, we just escape the assignment and just return the value. In the latter case, this is typically going to
        // be a consequence of an illogical intersection.
        const value = schema.allOf.reduce((acc, schema) => {
            const next = Visit(schema, references);
            return typeof next === 'object' ? { ...acc, ...next } : next;
        }, {});
        if (!(0, check_1.Check)(schema, references, value))
            throw new ValueCreateIntersectTypeError(schema);
        return value;
    }
}
function TIterator(schema, references) {
    if ((0, guard_1.HasPropertyKey)(schema, 'default')) {
        return schema.default;
    }
    else {
        return (function* () { })();
    }
}
function TLiteral(schema, references) {
    if ((0, guard_1.HasPropertyKey)(schema, 'default')) {
        return schema.default;
    }
    else {
        return schema.const;
    }
}
function TNever(schema, references) {
    throw new ValueCreateNeverTypeError(schema);
}
function TNot(schema, references) {
    if ((0, guard_1.HasPropertyKey)(schema, 'default')) {
        return schema.default;
    }
    else {
        throw new ValueCreateNotTypeError(schema);
    }
}
function TNull(schema, references) {
    if ((0, guard_1.HasPropertyKey)(schema, 'default')) {
        return schema.default;
    }
    else {
        return null;
    }
}
function TNumber(schema, references) {
    if ((0, guard_1.HasPropertyKey)(schema, 'default')) {
        return schema.default;
    }
    else if (schema.minimum !== undefined) {
        return schema.minimum;
    }
    else {
        return 0;
    }
}
function TObject(schema, references) {
    if ((0, guard_1.HasPropertyKey)(schema, 'default')) {
        return schema.default;
    }
    else {
        const required = new Set(schema.required);
        return (schema.default ||
            Object.entries(schema.properties).reduce((acc, [key, schema]) => {
                return required.has(key) ? { ...acc, [key]: Visit(schema, references) } : { ...acc };
            }, {}));
    }
}
function TPromise(schema, references) {
    if ((0, guard_1.HasPropertyKey)(schema, 'default')) {
        return schema.default;
    }
    else {
        return Promise.resolve(Visit(schema.item, references));
    }
}
function TRecord(schema, references) {
    const [keyPattern, valueSchema] = Object.entries(schema.patternProperties)[0];
    if ((0, guard_1.HasPropertyKey)(schema, 'default')) {
        return schema.default;
    }
    else if (!(keyPattern === Types.PatternStringExact || keyPattern === Types.PatternNumberExact)) {
        const propertyKeys = keyPattern.slice(1, keyPattern.length - 1).split('|');
        return propertyKeys.reduce((acc, key) => {
            return { ...acc, [key]: Visit(valueSchema, references) };
        }, {});
    }
    else {
        return {};
    }
}
function TRef(schema, references) {
    if ((0, guard_1.HasPropertyKey)(schema, 'default')) {
        return schema.default;
    }
    else {
        return Visit((0, deref_1.Deref)(schema, references), references);
    }
}
function TString(schema, references) {
    if (schema.pattern !== undefined) {
        if (!(0, guard_1.HasPropertyKey)(schema, 'default')) {
            throw new Error('ValueCreate.String: String types with patterns must specify a default value');
        }
        else {
            return schema.default;
        }
    }
    else if (schema.format !== undefined) {
        if (!(0, guard_1.HasPropertyKey)(schema, 'default')) {
            throw new Error('ValueCreate.String: String types with formats must specify a default value');
        }
        else {
            return schema.default;
        }
    }
    else {
        if ((0, guard_1.HasPropertyKey)(schema, 'default')) {
            return schema.default;
        }
        else if (schema.minLength !== undefined) {
            return Array.from({ length: schema.minLength })
                .map(() => '.')
                .join('');
        }
        else {
            return '';
        }
    }
}
function TSymbol(schema, references) {
    if ((0, guard_1.HasPropertyKey)(schema, 'default')) {
        return schema.default;
    }
    else if ('value' in schema) {
        return Symbol.for(schema.value);
    }
    else {
        return Symbol();
    }
}
function TTemplateLiteral(schema, references) {
    if ((0, guard_1.HasPropertyKey)(schema, 'default')) {
        return schema.default;
    }
    const expression = Types.TemplateLiteralParser.ParseExact(schema.pattern);
    if (!Types.TemplateLiteralFinite.Check(expression))
        throw new ValueCreateTempateLiteralTypeError(schema);
    const sequence = Types.TemplateLiteralGenerator.Generate(expression);
    return sequence.next().value;
}
function TThis(schema, references) {
    if (recursiveDepth++ > recursiveMaxDepth)
        throw new ValueCreateRecursiveInstantiationError(schema, recursiveMaxDepth);
    if ((0, guard_1.HasPropertyKey)(schema, 'default')) {
        return schema.default;
    }
    else {
        return Visit((0, deref_1.Deref)(schema, references), references);
    }
}
function TTuple(schema, references) {
    if ((0, guard_1.HasPropertyKey)(schema, 'default')) {
        return schema.default;
    }
    if (schema.items === undefined) {
        return [];
    }
    else {
        return Array.from({ length: schema.minItems }).map((_, index) => Visit(schema.items[index], references));
    }
}
function TUndefined(schema, references) {
    if ((0, guard_1.HasPropertyKey)(schema, 'default')) {
        return schema.default;
    }
    else {
        return undefined;
    }
}
function TUnion(schema, references) {
    if ((0, guard_1.HasPropertyKey)(schema, 'default')) {
        return schema.default;
    }
    else if (schema.anyOf.length === 0) {
        throw new Error('ValueCreate.Union: Cannot create Union with zero variants');
    }
    else {
        return Visit(schema.anyOf[0], references);
    }
}
function TUint8Array(schema, references) {
    if ((0, guard_1.HasPropertyKey)(schema, 'default')) {
        return schema.default;
    }
    else if (schema.minByteLength !== undefined) {
        return new Uint8Array(schema.minByteLength);
    }
    else {
        return new Uint8Array(0);
    }
}
function TUnknown(schema, references) {
    if ((0, guard_1.HasPropertyKey)(schema, 'default')) {
        return schema.default;
    }
    else {
        return {};
    }
}
function TVoid(schema, references) {
    if ((0, guard_1.HasPropertyKey)(schema, 'default')) {
        return schema.default;
    }
    else {
        return void 0;
    }
}
function TKind(schema, references) {
    if ((0, guard_1.HasPropertyKey)(schema, 'default')) {
        return schema.default;
    }
    else {
        throw new Error('User defined types must specify a default value');
    }
}
function Visit(schema, references) {
    const references_ = (0, guard_1.IsString)(schema.$id) ? [...references, schema] : references;
    const schema_ = schema;
    switch (schema_[Types.Kind]) {
        case 'Any':
            return TAny(schema_, references_);
        case 'Array':
            return TArray(schema_, references_);
        case 'AsyncIterator':
            return TAsyncIterator(schema_, references_);
        case 'BigInt':
            return TBigInt(schema_, references_);
        case 'Boolean':
            return TBoolean(schema_, references_);
        case 'Constructor':
            return TConstructor(schema_, references_);
        case 'Date':
            return TDate(schema_, references_);
        case 'Function':
            return TFunction(schema_, references_);
        case 'Integer':
            return TInteger(schema_, references_);
        case 'Intersect':
            return TIntersect(schema_, references_);
        case 'Iterator':
            return TIterator(schema_, references_);
        case 'Literal':
            return TLiteral(schema_, references_);
        case 'Never':
            return TNever(schema_, references_);
        case 'Not':
            return TNot(schema_, references_);
        case 'Null':
            return TNull(schema_, references_);
        case 'Number':
            return TNumber(schema_, references_);
        case 'Object':
            return TObject(schema_, references_);
        case 'Promise':
            return TPromise(schema_, references_);
        case 'Record':
            return TRecord(schema_, references_);
        case 'Ref':
            return TRef(schema_, references_);
        case 'String':
            return TString(schema_, references_);
        case 'Symbol':
            return TSymbol(schema_, references_);
        case 'TemplateLiteral':
            return TTemplateLiteral(schema_, references_);
        case 'This':
            return TThis(schema_, references_);
        case 'Tuple':
            return TTuple(schema_, references_);
        case 'Undefined':
            return TUndefined(schema_, references_);
        case 'Union':
            return TUnion(schema_, references_);
        case 'Uint8Array':
            return TUint8Array(schema_, references_);
        case 'Unknown':
            return TUnknown(schema_, references_);
        case 'Void':
            return TVoid(schema_, references_);
        default:
            if (!Types.TypeRegistry.Has(schema_[Types.Kind]))
                throw new ValueCreateUnknownTypeError(schema_);
            return TKind(schema_, references_);
    }
}
// --------------------------------------------------------------------------
// State
// --------------------------------------------------------------------------
const recursiveMaxDepth = 512;
let recursiveDepth = 0;
/** Creates a value from the given schema */
function Create(...args) {
    recursiveDepth = 0;
    return args.length === 2 ? Visit(args[0], args[1]) : Visit(args[0], []);
}
exports.Create = Create;
