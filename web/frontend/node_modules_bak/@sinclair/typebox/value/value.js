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
exports.Value = void 0;
const ValueErrors = require("../errors/index");
const ValueMutate = require("./mutate");
const ValueHash = require("./hash");
const ValueEqual = require("./equal");
const ValueCast = require("./cast");
const ValueClone = require("./clone");
const ValueConvert = require("./convert");
const ValueCreate = require("./create");
const ValueCheck = require("./check");
const ValueDelta = require("./delta");
const ValueTransform = require("./transform");
/** Functions to perform structural operations on JavaScript values */
var Value;
(function (Value) {
    /** Casts a value into a given type. The return value will retain as much information of the original value as possible. */
    function Cast(...args) {
        return ValueCast.Cast.apply(ValueCast, args);
    }
    Value.Cast = Cast;
    /** Creates a value from the given type */
    function Create(...args) {
        return ValueCreate.Create.apply(ValueCreate, args);
    }
    Value.Create = Create;
    /** Returns true if the value matches the given type */
    function Check(...args) {
        return ValueCheck.Check.apply(ValueCheck, args);
    }
    Value.Check = Check;
    /** Converts any type mismatched values to their target type if a reasonable conversion is possible */
    function Convert(...args) {
        return ValueConvert.Convert.apply(ValueConvert, args);
    }
    Value.Convert = Convert;
    /** Returns a structural clone of the given value */
    function Clone(value) {
        return ValueClone.Clone(value);
    }
    Value.Clone = Clone;
    /** Decodes a value or throws if error */
    function Decode(...args) {
        const [schema, references, value] = args.length === 3 ? [args[0], args[1], args[2]] : [args[0], [], args[1]];
        if (!Check(schema, references, value))
            throw new ValueTransform.TransformDecodeCheckError(schema, value, Errors(schema, references, value).First());
        return ValueTransform.DecodeTransform.Decode(schema, references, value);
    }
    Value.Decode = Decode;
    /** Encodes a value or throws if error */
    function Encode(...args) {
        const [schema, references, value] = args.length === 3 ? [args[0], args[1], args[2]] : [args[0], [], args[1]];
        const encoded = ValueTransform.EncodeTransform.Encode(schema, references, value);
        if (!Check(schema, references, encoded))
            throw new ValueTransform.TransformEncodeCheckError(schema, value, Errors(schema, references, value).First());
        return encoded;
    }
    Value.Encode = Encode;
    /** Returns an iterator for each error in this value. */
    function Errors(...args) {
        return ValueErrors.Errors.apply(ValueErrors, args);
    }
    Value.Errors = Errors;
    /** Returns true if left and right values are structurally equal */
    function Equal(left, right) {
        return ValueEqual.Equal(left, right);
    }
    Value.Equal = Equal;
    /** Returns edits to transform the current value into the next value */
    function Diff(current, next) {
        return ValueDelta.Diff(current, next);
    }
    Value.Diff = Diff;
    /** Returns a FNV1A-64 non cryptographic hash of the given value */
    function Hash(value) {
        return ValueHash.Hash(value);
    }
    Value.Hash = Hash;
    /** Returns a new value with edits applied to the given value */
    function Patch(current, edits) {
        return ValueDelta.Patch(current, edits);
    }
    Value.Patch = Patch;
    /** Performs a deep mutable value assignment while retaining internal references. */
    function Mutate(current, next) {
        ValueMutate.Mutate(current, next);
    }
    Value.Mutate = Mutate;
})(Value || (exports.Value = Value = {}));
