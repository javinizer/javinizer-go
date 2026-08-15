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
exports.Value = exports.ValuePointer = exports.Delete = exports.Update = exports.Insert = exports.Edit = exports.ValueErrorIterator = exports.ValueErrorType = void 0;
var index_1 = require("../errors/index");
Object.defineProperty(exports, "ValueErrorType", { enumerable: true, get: function () { return index_1.ValueErrorType; } });
Object.defineProperty(exports, "ValueErrorIterator", { enumerable: true, get: function () { return index_1.ValueErrorIterator; } });
var delta_1 = require("./delta");
Object.defineProperty(exports, "Edit", { enumerable: true, get: function () { return delta_1.Edit; } });
Object.defineProperty(exports, "Insert", { enumerable: true, get: function () { return delta_1.Insert; } });
Object.defineProperty(exports, "Update", { enumerable: true, get: function () { return delta_1.Update; } });
Object.defineProperty(exports, "Delete", { enumerable: true, get: function () { return delta_1.Delete; } });
var pointer_1 = require("./pointer");
Object.defineProperty(exports, "ValuePointer", { enumerable: true, get: function () { return pointer_1.ValuePointer; } });
var value_1 = require("./value");
Object.defineProperty(exports, "Value", { enumerable: true, get: function () { return value_1.Value; } });
