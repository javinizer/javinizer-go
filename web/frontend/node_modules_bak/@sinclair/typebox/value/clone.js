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
exports.Clone = void 0;
const guard_1 = require("./guard");
// --------------------------------------------------------------------------
// Clonable
// --------------------------------------------------------------------------
function ObjectType(value) {
    const keys = [...Object.getOwnPropertyNames(value), ...Object.getOwnPropertySymbols(value)];
    return keys.reduce((acc, key) => ({ ...acc, [key]: Clone(value[key]) }), {});
}
function ArrayType(value) {
    return value.map((element) => Clone(element));
}
function TypedArrayType(value) {
    return value.slice();
}
function DateType(value) {
    return new Date(value.toISOString());
}
function ValueType(value) {
    return value;
}
// --------------------------------------------------------------------------
// Clone
// --------------------------------------------------------------------------
/** Returns a clone of the given value */
function Clone(value) {
    if ((0, guard_1.IsArray)(value))
        return ArrayType(value);
    if ((0, guard_1.IsDate)(value))
        return DateType(value);
    if ((0, guard_1.IsPlainObject)(value))
        return ObjectType(value);
    if ((0, guard_1.IsTypedArray)(value))
        return TypedArrayType(value);
    if ((0, guard_1.IsValueType)(value))
        return ValueType(value);
    throw new Error('ValueClone: Unable to clone value');
}
exports.Clone = Clone;
