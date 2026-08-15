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
exports.Mutate = exports.ValueMutateInvalidRootMutationError = exports.ValueMutateTypeMismatchError = void 0;
const guard_1 = require("./guard");
const pointer_1 = require("./pointer");
const clone_1 = require("./clone");
// --------------------------------------------------------------------------
// Errors
// --------------------------------------------------------------------------
class ValueMutateTypeMismatchError extends Error {
    constructor() {
        super('Cannot assign due type mismatch of assignable values');
    }
}
exports.ValueMutateTypeMismatchError = ValueMutateTypeMismatchError;
class ValueMutateInvalidRootMutationError extends Error {
    constructor() {
        super('Only object and array types can be mutated at the root level');
    }
}
exports.ValueMutateInvalidRootMutationError = ValueMutateInvalidRootMutationError;
function ObjectType(root, path, current, next) {
    if (!(0, guard_1.IsPlainObject)(current)) {
        pointer_1.ValuePointer.Set(root, path, (0, clone_1.Clone)(next));
    }
    else {
        const currentKeys = Object.keys(current);
        const nextKeys = Object.keys(next);
        for (const currentKey of currentKeys) {
            if (!nextKeys.includes(currentKey)) {
                delete current[currentKey];
            }
        }
        for (const nextKey of nextKeys) {
            if (!currentKeys.includes(nextKey)) {
                current[nextKey] = null;
            }
        }
        for (const nextKey of nextKeys) {
            Visit(root, `${path}/${nextKey}`, current[nextKey], next[nextKey]);
        }
    }
}
function ArrayType(root, path, current, next) {
    if (!(0, guard_1.IsArray)(current)) {
        pointer_1.ValuePointer.Set(root, path, (0, clone_1.Clone)(next));
    }
    else {
        for (let index = 0; index < next.length; index++) {
            Visit(root, `${path}/${index}`, current[index], next[index]);
        }
        current.splice(next.length);
    }
}
function TypedArrayType(root, path, current, next) {
    if ((0, guard_1.IsTypedArray)(current) && current.length === next.length) {
        for (let i = 0; i < current.length; i++) {
            current[i] = next[i];
        }
    }
    else {
        pointer_1.ValuePointer.Set(root, path, (0, clone_1.Clone)(next));
    }
}
function ValueType(root, path, current, next) {
    if (current === next)
        return;
    pointer_1.ValuePointer.Set(root, path, next);
}
function Visit(root, path, current, next) {
    if ((0, guard_1.IsArray)(next))
        return ArrayType(root, path, current, next);
    if ((0, guard_1.IsTypedArray)(next))
        return TypedArrayType(root, path, current, next);
    if ((0, guard_1.IsPlainObject)(next))
        return ObjectType(root, path, current, next);
    if ((0, guard_1.IsValueType)(next))
        return ValueType(root, path, current, next);
}
// --------------------------------------------------------------------------
// Mutate
// --------------------------------------------------------------------------
function IsNonMutableValue(value) {
    return (0, guard_1.IsTypedArray)(value) || (0, guard_1.IsValueType)(value);
}
function IsMismatchedValue(current, next) {
    // prettier-ignore
    return (((0, guard_1.IsPlainObject)(current) && (0, guard_1.IsArray)(next)) ||
        ((0, guard_1.IsArray)(current) && (0, guard_1.IsPlainObject)(next)));
}
// --------------------------------------------------------------------------
// Mutate
// --------------------------------------------------------------------------
/** Performs a deep mutable value assignment while retaining internal references */
function Mutate(current, next) {
    if (IsNonMutableValue(current) || IsNonMutableValue(next))
        throw new ValueMutateInvalidRootMutationError();
    if (IsMismatchedValue(current, next))
        throw new ValueMutateTypeMismatchError();
    Visit(current, '', current, next);
}
exports.Mutate = Mutate;
