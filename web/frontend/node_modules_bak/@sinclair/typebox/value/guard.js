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
exports.IsValueType = exports.IsSymbol = exports.IsFunction = exports.IsString = exports.IsBigInt = exports.IsInteger = exports.IsNumber = exports.IsBoolean = exports.IsNull = exports.IsUndefined = exports.IsArray = exports.IsObject = exports.IsPlainObject = exports.HasPropertyKey = exports.IsDate = exports.IsUint8Array = exports.IsPromise = exports.IsTypedArray = exports.IsIterator = exports.IsAsyncIterator = void 0;
// --------------------------------------------------------------------------
// Iterators
// --------------------------------------------------------------------------
/** Returns true if this value is an async iterator */
function IsAsyncIterator(value) {
    return IsObject(value) && Symbol.asyncIterator in value;
}
exports.IsAsyncIterator = IsAsyncIterator;
/** Returns true if this value is an iterator */
function IsIterator(value) {
    return IsObject(value) && Symbol.iterator in value;
}
exports.IsIterator = IsIterator;
// --------------------------------------------------------------------------
// Nominal
// --------------------------------------------------------------------------
/** Returns true if this value is a typed array */
function IsTypedArray(value) {
    return ArrayBuffer.isView(value);
}
exports.IsTypedArray = IsTypedArray;
/** Returns true if this value is a Promise */
function IsPromise(value) {
    return value instanceof Promise;
}
exports.IsPromise = IsPromise;
/** Returns true if the value is a Uint8Array */
function IsUint8Array(value) {
    return value instanceof Uint8Array;
}
exports.IsUint8Array = IsUint8Array;
/** Returns true if this value is a Date */
function IsDate(value) {
    return value instanceof Date && Number.isFinite(value.getTime());
}
exports.IsDate = IsDate;
// --------------------------------------------------------------------------
// Standard
// --------------------------------------------------------------------------
/** Returns true if this value has this property key */
function HasPropertyKey(value, key) {
    return key in value;
}
exports.HasPropertyKey = HasPropertyKey;
/** Returns true if this object is not an instance of any other type */
function IsPlainObject(value) {
    return IsObject(value) && IsFunction(value.constructor) && value.constructor.name === 'Object';
}
exports.IsPlainObject = IsPlainObject;
/** Returns true of this value is an object type */
function IsObject(value) {
    return value !== null && typeof value === 'object';
}
exports.IsObject = IsObject;
/** Returns true if this value is an array, but not a typed array */
function IsArray(value) {
    return Array.isArray(value) && !ArrayBuffer.isView(value);
}
exports.IsArray = IsArray;
/** Returns true if this value is an undefined */
function IsUndefined(value) {
    return value === undefined;
}
exports.IsUndefined = IsUndefined;
/** Returns true if this value is an null */
function IsNull(value) {
    return value === null;
}
exports.IsNull = IsNull;
/** Returns true if this value is an boolean */
function IsBoolean(value) {
    return typeof value === 'boolean';
}
exports.IsBoolean = IsBoolean;
/** Returns true if this value is an number */
function IsNumber(value) {
    return typeof value === 'number';
}
exports.IsNumber = IsNumber;
/** Returns true if this value is an integer */
function IsInteger(value) {
    return IsNumber(value) && Number.isInteger(value);
}
exports.IsInteger = IsInteger;
/** Returns true if this value is bigint */
function IsBigInt(value) {
    return typeof value === 'bigint';
}
exports.IsBigInt = IsBigInt;
/** Returns true if this value is string */
function IsString(value) {
    return typeof value === 'string';
}
exports.IsString = IsString;
/** Returns true if this value is a function */
function IsFunction(value) {
    return typeof value === 'function';
}
exports.IsFunction = IsFunction;
/** Returns true if this value is a symbol */
function IsSymbol(value) {
    return typeof value === 'symbol';
}
exports.IsSymbol = IsSymbol;
/** Returns true if this value is a value type such as number, string, boolean */
function IsValueType(value) {
    // prettier-ignore
    return (IsBigInt(value) ||
        IsBoolean(value) ||
        IsNull(value) ||
        IsNumber(value) ||
        IsString(value) ||
        IsSymbol(value) ||
        IsUndefined(value));
}
exports.IsValueType = IsValueType;
