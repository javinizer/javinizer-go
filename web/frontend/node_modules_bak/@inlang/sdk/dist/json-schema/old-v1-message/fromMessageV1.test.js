import { test, expect } from "vitest";
import { fromMessageV1 } from "./fromMessageV1.js";
import { Value } from "@sinclair/typebox/value";
import { MessageV1 } from "./schemaV1.js";
const messageV1 = {
    id: "hello_world",
    alias: {},
    variants: [
        {
            languageTag: "en",
            match: [],
            pattern: [
                {
                    type: "Text",
                    value: "Hello World!",
                },
            ],
        },
        {
            languageTag: "de",
            match: [],
            pattern: [
                {
                    type: "Text",
                    value: "Hallo Welt!",
                },
            ],
        },
    ],
    selectors: [],
};
const bundle = {
    id: "hello_world",
    declarations: [],
    messages: [
        {
            bundleId: "hello_world",
            id: "hello_world" + "_en",
            locale: "en",
            selectors: [],
            variants: [
                {
                    id: "hello_world" + "_en_1",
                    matches: [],
                    messageId: "hello_world" + "_en",
                    pattern: [
                        {
                            type: "text",
                            value: "Hello World!",
                        },
                    ],
                },
            ],
        },
        {
            bundleId: "hello_world",
            id: "hello_world" + "_de",
            locale: "de",
            selectors: [],
            variants: [
                {
                    id: "hello_world" + "_de_1",
                    matches: [],
                    messageId: "hello_world" + "_de",
                    pattern: [
                        {
                            type: "text",
                            value: "Hallo Welt!",
                        },
                    ],
                },
            ],
        },
    ],
};
test("fromMessageV1", () => {
    expect(Value.Check(MessageV1, messageV1)).toBe(true);
    const nestedBundle = fromMessageV1(messageV1);
    expect(nestedBundle).toEqual(bundle);
});
test.todo("with variable references", () => { });
//# sourceMappingURL=fromMessageV1.test.js.map