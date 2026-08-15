import { it, expect } from "vitest";
import { split } from "./error-handling.js";
it("splits an array", () => {
    const isEven = (n) => n % 2 === 0;
    const arr = [1, 2, 3, 4, 5, 6];
    const [even, odd] = split(arr, isEven);
    expect(even).toEqual([2, 4, 6]);
    expect(odd).toEqual([1, 3, 5]);
});
//# sourceMappingURL=error-handling.test.js.map