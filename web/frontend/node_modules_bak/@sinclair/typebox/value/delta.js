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
exports.Patch = exports.Diff = exports.ValueDeltaUnableToDiffUnknownValue = exports.ValueDeltaObjectWithSymbolKeyError = exports.Edit = exports.Delete = exports.Update = exports.Insert = void 0;
const guard_1 = require("./guard");
const typebox_1 = require("../typebox");
const pointer_1 = require("./pointer");
const clone_1 = require("./clone");
exports.Insert = typebox_1.Type.Object({
    type: typebox_1.Type.Literal('insert'),
    path: typebox_1.Type.String(),
    value: typebox_1.Type.Unknown(),
});
exports.Update = typebox_1.Type.Object({
    type: typebox_1.Type.Literal('update'),
    path: typebox_1.Type.String(),
    value: typebox_1.Type.Unknown(),
});
exports.Delete = typebox_1.Type.Object({
    type: typebox_1.Type.Literal('delete'),
    path: typebox_1.Type.String(),
});
exports.Edit = typebox_1.Type.Union([exports.Insert, exports.Update, exports.Delete]);
// --------------------------------------------------------------------------
// Errors
// --------------------------------------------------------------------------
class ValueDeltaObjectWithSymbolKeyError extends Error {
    constructor(key) {
        super('Cannot diff objects with symbol keys');
        this.key = key;
    }
}
exports.ValueDeltaObjectWithSymbolKeyError = ValueDeltaObjectWithSymbolKeyError;
class ValueDeltaUnableToDiffUnknownValue extends Error {
    constructor(value) {
        super('Unable to create diff edits for unknown value');
        this.value = value;
    }
}
exports.ValueDeltaUnableToDiffUnknownValue = ValueDeltaUnableToDiffUnknownValue;
// --------------------------------------------------------------------------
// Command Factory
// --------------------------------------------------------------------------
function CreateUpdate(path, value) {
    return { type: 'update', path, value };
}
function CreateInsert(path, value) {
    return { type: 'insert', path, value };
}
function CreateDelete(path) {
    return { type: 'delete', path };
}
// --------------------------------------------------------------------------
// Diffing Generators
// --------------------------------------------------------------------------
function* ObjectType(path, current, next) {
    if (!(0, guard_1.IsPlainObject)(next))
        return yield CreateUpdate(path, next);
    const currentKeys = [...Object.keys(current), ...Object.getOwnPropertySymbols(current)];
    const nextKeys = [...Object.keys(next), ...Object.getOwnPropertySymbols(next)];
    for (const key of currentKeys) {
        if ((0, guard_1.IsSymbol)(key))
            throw new ValueDeltaObjectWithSymbolKeyError(key);
        if ((0, guard_1.IsUndefined)(next[key]) && nextKeys.includes(key))
            yield CreateUpdate(`${path}/${String(key)}`, undefined);
    }
    for (const key of nextKeys) {
        if ((0, guard_1.IsUndefined)(current[key]) || (0, guard_1.IsUndefined)(next[key]))
            continue;
        if ((0, guard_1.IsSymbol)(key))
            throw new ValueDeltaObjectWithSymbolKeyError(key);
        yield* Visit(`${path}/${String(key)}`, current[key], next[key]);
    }
    for (const key of nextKeys) {
        if ((0, guard_1.IsSymbol)(key))
            throw new ValueDeltaObjectWithSymbolKeyError(key);
        if ((0, guard_1.IsUndefined)(current[key]))
            yield CreateInsert(`${path}/${String(key)}`, next[key]);
    }
    for (const key of currentKeys.reverse()) {
        if ((0, guard_1.IsSymbol)(key))
            throw new ValueDeltaObjectWithSymbolKeyError(key);
        if ((0, guard_1.IsUndefined)(next[key]) && !nextKeys.includes(key))
            yield CreateDelete(`${path}/${String(key)}`);
    }
}
function* ArrayType(path, current, next) {
    if (!(0, guard_1.IsArray)(next))
        return yield CreateUpdate(path, next);
    for (let i = 0; i < Math.min(current.length, next.length); i++) {
        yield* Visit(`${path}/${i}`, current[i], next[i]);
    }
    for (let i = 0; i < next.length; i++) {
        if (i < current.length)
            continue;
        yield CreateInsert(`${path}/${i}`, next[i]);
    }
    for (let i = current.length - 1; i >= 0; i--) {
        if (i < next.length)
            continue;
        yield CreateDelete(`${path}/${i}`);
    }
}
function* TypedArrayType(path, current, next) {
    if (!(0, guard_1.IsTypedArray)(next) || current.length !== next.length || Object.getPrototypeOf(current).constructor.name !== Object.getPrototypeOf(next).constructor.name)
        return yield CreateUpdate(path, next);
    for (let i = 0; i < Math.min(current.length, next.length); i++) {
        yield* Visit(`${path}/${i}`, current[i], next[i]);
    }
}
function* ValueType(path, current, next) {
    if (current === next)
        return;
    yield CreateUpdate(path, next);
}
function* Visit(path, current, next) {
    if ((0, guard_1.IsPlainObject)(current))
        return yield* ObjectType(path, current, next);
    if ((0, guard_1.IsArray)(current))
        return yield* ArrayType(path, current, next);
    if ((0, guard_1.IsTypedArray)(current))
        return yield* TypedArrayType(path, current, next);
    if ((0, guard_1.IsValueType)(current))
        return yield* ValueType(path, current, next);
    throw new ValueDeltaUnableToDiffUnknownValue(current);
}
// ---------------------------------------------------------------------
// Diff
// ---------------------------------------------------------------------
function Diff(current, next) {
    return [...Visit('', current, next)];
}
exports.Diff = Diff;
// ---------------------------------------------------------------------
// Patch
// ---------------------------------------------------------------------
function IsRootUpdate(edits) {
    return edits.length > 0 && edits[0].path === '' && edits[0].type === 'update';
}
function IsIdentity(edits) {
    return edits.length === 0;
}
function Patch(current, edits) {
    if (IsRootUpdate(edits)) {
        return (0, clone_1.Clone)(edits[0].value);
    }
    if (IsIdentity(edits)) {
        return (0, clone_1.Clone)(current);
    }
    const clone = (0, clone_1.Clone)(current);
    for (const edit of edits) {
        switch (edit.type) {
            case 'insert': {
                pointer_1.ValuePointer.Set(clone, edit.path, edit.value);
                break;
            }
            case 'update': {
                pointer_1.ValuePointer.Set(clone, edit.path, edit.value);
                break;
            }
            case 'delete': {
                pointer_1.ValuePointer.Delete(clone, edit.path);
                break;
            }
        }
    }
    return clone;
}
exports.Patch = Patch;
