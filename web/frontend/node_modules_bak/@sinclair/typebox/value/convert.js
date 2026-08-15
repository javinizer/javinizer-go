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
exports.Convert = exports.Default = exports.ValueConvertUnknownTypeError = void 0;
const guard_1 = require("./guard");
const clone_1 = require("./clone");
const check_1 = require("./check");
const deref_1 = require("./deref");
const Types = require("../typebox");
// --------------------------------------------------------------------------
// Errors
// --------------------------------------------------------------------------
class ValueConvertUnknownTypeError extends Types.TypeBoxError {
    constructor(schema) {
        super('Unknown type');
        this.schema = schema;
    }
}
exports.ValueConvertUnknownTypeError = ValueConvertUnknownTypeError;
// --------------------------------------------------------------------------
// Conversions
// --------------------------------------------------------------------------
function IsStringNumeric(value) {
    return (0, guard_1.IsString)(value) && !isNaN(value) && !isNaN(parseFloat(value));
}
function IsValueToString(value) {
    return (0, guard_1.IsBigInt)(value) || (0, guard_1.IsBoolean)(value) || (0, guard_1.IsNumber)(value);
}
function IsValueTrue(value) {
    return value === true || ((0, guard_1.IsNumber)(value) && value === 1) || ((0, guard_1.IsBigInt)(value) && value === BigInt('1')) || ((0, guard_1.IsString)(value) && (value.toLowerCase() === 'true' || value === '1'));
}
function IsValueFalse(value) {
    return value === false || ((0, guard_1.IsNumber)(value) && (value === 0 || Object.is(value, -0))) || ((0, guard_1.IsBigInt)(value) && value === BigInt('0')) || ((0, guard_1.IsString)(value) && (value.toLowerCase() === 'false' || value === '0' || value === '-0'));
}
function IsTimeStringWithTimeZone(value) {
    return (0, guard_1.IsString)(value) && /^(?:[0-2]\d:[0-5]\d:[0-5]\d|23:59:60)(?:\.\d+)?(?:z|[+-]\d\d(?::?\d\d)?)$/i.test(value);
}
function IsTimeStringWithoutTimeZone(value) {
    return (0, guard_1.IsString)(value) && /^(?:[0-2]\d:[0-5]\d:[0-5]\d|23:59:60)?$/i.test(value);
}
function IsDateTimeStringWithTimeZone(value) {
    return (0, guard_1.IsString)(value) && /^\d\d\d\d-[0-1]\d-[0-3]\dt(?:[0-2]\d:[0-5]\d:[0-5]\d|23:59:60)(?:\.\d+)?(?:z|[+-]\d\d(?::?\d\d)?)$/i.test(value);
}
function IsDateTimeStringWithoutTimeZone(value) {
    return (0, guard_1.IsString)(value) && /^\d\d\d\d-[0-1]\d-[0-3]\dt(?:[0-2]\d:[0-5]\d:[0-5]\d|23:59:60)?$/i.test(value);
}
function IsDateString(value) {
    return (0, guard_1.IsString)(value) && /^\d\d\d\d-[0-1]\d-[0-3]\d$/i.test(value);
}
// --------------------------------------------------------------------------
// Convert
// --------------------------------------------------------------------------
function TryConvertLiteralString(value, target) {
    const conversion = TryConvertString(value);
    return conversion === target ? conversion : value;
}
function TryConvertLiteralNumber(value, target) {
    const conversion = TryConvertNumber(value);
    return conversion === target ? conversion : value;
}
function TryConvertLiteralBoolean(value, target) {
    const conversion = TryConvertBoolean(value);
    return conversion === target ? conversion : value;
}
function TryConvertLiteral(schema, value) {
    if (typeof schema.const === 'string') {
        return TryConvertLiteralString(value, schema.const);
    }
    else if (typeof schema.const === 'number') {
        return TryConvertLiteralNumber(value, schema.const);
    }
    else if (typeof schema.const === 'boolean') {
        return TryConvertLiteralBoolean(value, schema.const);
    }
    else {
        return (0, clone_1.Clone)(value);
    }
}
function TryConvertBoolean(value) {
    return IsValueTrue(value) ? true : IsValueFalse(value) ? false : value;
}
function TryConvertBigInt(value) {
    return IsStringNumeric(value) ? BigInt(parseInt(value)) : (0, guard_1.IsNumber)(value) ? BigInt(value | 0) : IsValueFalse(value) ? BigInt(0) : IsValueTrue(value) ? BigInt(1) : value;
}
function TryConvertString(value) {
    return IsValueToString(value) ? value.toString() : (0, guard_1.IsSymbol)(value) && value.description !== undefined ? value.description.toString() : value;
}
function TryConvertNumber(value) {
    return IsStringNumeric(value) ? parseFloat(value) : IsValueTrue(value) ? 1 : IsValueFalse(value) ? 0 : value;
}
function TryConvertInteger(value) {
    return IsStringNumeric(value) ? parseInt(value) : (0, guard_1.IsNumber)(value) ? value | 0 : IsValueTrue(value) ? 1 : IsValueFalse(value) ? 0 : value;
}
function TryConvertNull(value) {
    return (0, guard_1.IsString)(value) && value.toLowerCase() === 'null' ? null : value;
}
function TryConvertUndefined(value) {
    return (0, guard_1.IsString)(value) && value === 'undefined' ? undefined : value;
}
function TryConvertDate(value) {
    // --------------------------------------------------------------------------
    // note: this function may return an invalid dates for the regex tests
    // above. Invalid dates will however be checked during the casting function
    // and will return a epoch date if invalid. Consider better string parsing
    // for the iso dates in future revisions.
    // --------------------------------------------------------------------------
    return (0, guard_1.IsDate)(value)
        ? value
        : (0, guard_1.IsNumber)(value)
            ? new Date(value)
            : IsValueTrue(value)
                ? new Date(1)
                : IsValueFalse(value)
                    ? new Date(0)
                    : IsStringNumeric(value)
                        ? new Date(parseInt(value))
                        : IsTimeStringWithoutTimeZone(value)
                            ? new Date(`1970-01-01T${value}.000Z`)
                            : IsTimeStringWithTimeZone(value)
                                ? new Date(`1970-01-01T${value}`)
                                : IsDateTimeStringWithoutTimeZone(value)
                                    ? new Date(`${value}.000Z`)
                                    : IsDateTimeStringWithTimeZone(value)
                                        ? new Date(value)
                                        : IsDateString(value)
                                            ? new Date(`${value}T00:00:00.000Z`)
                                            : value;
}
// --------------------------------------------------------------------------
// Default
// --------------------------------------------------------------------------
function Default(value) {
    return value;
}
exports.Default = Default;
// --------------------------------------------------------------------------
// Convert
// --------------------------------------------------------------------------
function TArray(schema, references, value) {
    if ((0, guard_1.IsArray)(value)) {
        return value.map((value) => Visit(schema.items, references, value));
    }
    return value;
}
function TBigInt(schema, references, value) {
    return TryConvertBigInt(value);
}
function TBoolean(schema, references, value) {
    return TryConvertBoolean(value);
}
function TDate(schema, references, value) {
    return TryConvertDate(value);
}
function TInteger(schema, references, value) {
    return TryConvertInteger(value);
}
function TIntersect(schema, references, value) {
    // prettier-ignore
    return (schema.allOf.every(schema => Types.TypeGuard.TObject(schema)))
        ? Visit(Types.Type.Composite(schema.allOf), references, value)
        : Visit(schema.allOf[0], references, value);
}
function TLiteral(schema, references, value) {
    return TryConvertLiteral(schema, value);
}
function TNull(schema, references, value) {
    return TryConvertNull(value);
}
function TNumber(schema, references, value) {
    return TryConvertNumber(value);
}
function TObject(schema, references, value) {
    if ((0, guard_1.IsObject)(value))
        return Object.getOwnPropertyNames(schema.properties).reduce((acc, key) => {
            return value[key] !== undefined ? { ...acc, [key]: Visit(schema.properties[key], references, value[key]) } : { ...acc };
        }, value);
    return value;
}
function TRecord(schema, references, value) {
    const propertyKey = Object.getOwnPropertyNames(schema.patternProperties)[0];
    const property = schema.patternProperties[propertyKey];
    const result = {};
    for (const [propKey, propValue] of Object.entries(value)) {
        result[propKey] = Visit(property, references, propValue);
    }
    return result;
}
function TRef(schema, references, value) {
    return Visit((0, deref_1.Deref)(schema, references), references, value);
}
function TString(schema, references, value) {
    return TryConvertString(value);
}
function TSymbol(schema, references, value) {
    return (0, guard_1.IsString)(value) || (0, guard_1.IsNumber)(value) ? Symbol(value) : value;
}
function TThis(schema, references, value) {
    return Visit((0, deref_1.Deref)(schema, references), references, value);
}
function TTuple(schema, references, value) {
    if ((0, guard_1.IsArray)(value) && !(0, guard_1.IsUndefined)(schema.items)) {
        return value.map((value, index) => {
            return index < schema.items.length ? Visit(schema.items[index], references, value) : value;
        });
    }
    return value;
}
function TUndefined(schema, references, value) {
    return TryConvertUndefined(value);
}
function TUnion(schema, references, value) {
    for (const subschema of schema.anyOf) {
        const converted = Visit(subschema, references, value);
        if ((0, check_1.Check)(subschema, references, converted)) {
            return converted;
        }
    }
    return value;
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
        case 'BigInt':
            return TBigInt(schema_, references_, value);
        case 'Boolean':
            return TBoolean(schema_, references_, value);
        case 'Date':
            return TDate(schema_, references_, value);
        case 'Integer':
            return TInteger(schema_, references_, value);
        case 'Intersect':
            return TIntersect(schema_, references_, value);
        case 'Literal':
            return TLiteral(schema_, references_, value);
        case 'Null':
            return TNull(schema_, references_, value);
        case 'Number':
            return TNumber(schema_, references_, value);
        case 'Object':
            return TObject(schema_, references_, value);
        case 'Record':
            return TRecord(schema_, references_, value);
        case 'Ref':
            return TRef(schema_, references_, value);
        case 'String':
            return TString(schema_, references_, value);
        case 'Symbol':
            return TSymbol(schema_, references_, value);
        case 'This':
            return TThis(schema_, references_, value);
        case 'Tuple':
            return TTuple(schema_, references_, value);
        case 'Undefined':
            return TUndefined(schema_, references_, value);
        case 'Union':
            return TUnion(schema_, references_, value);
        // ------------------------------------------------------
        // Default
        // ------------------------------------------------------
        case 'Any':
        case 'AsyncIterator':
        case 'Constructor':
        case 'Function':
        case 'Iterator':
        case 'Never':
        case 'Promise':
        case 'TemplateLiteral':
        case 'Uint8Array':
        case 'Unknown':
        case 'Void':
            return Default(value);
        default:
            if (!Types.TypeRegistry.Has(schema_[Types.Kind]))
                throw new ValueConvertUnknownTypeError(schema_);
            return Default(value);
    }
}
/** Converts any type mismatched values to their target type if a reasonable conversion is possible. */
function Convert(...args) {
    return args.length === 3 ? Visit(args[0], args[1], args[2]) : Visit(args[0], [], args[1]);
}
exports.Convert = Convert;
