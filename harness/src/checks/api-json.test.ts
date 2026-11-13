import { describe, expect, test } from "vitest";
import {
  asArray,
  asObject,
  asString,
  deref,
  isObject,
  operationsOf,
  parseObject,
  resolveRef,
} from "./api-json";

const DOC = {
  paths: { "/b": { post: { id: 1 } }, "/a": { get: { id: 2 }, delete: { id: 3 }, "x-note": {} } },
  components: {
    schemas: { A: { $ref: "#/components/schemas/B" }, B: { type: "string" }, "a/b": 1 },
  },
};

describe("JSON helpers of the API checks [REQ-143]", () => {
  test("values are read safely", () => {
    expect(isObject({})).toBe(true);
    expect(isObject([])).toBe(false);
    expect(isObject(null)).toBe(false);
    expect(asObject(3)).toEqual({});
    expect(asArray("x")).toEqual([]);
    expect(asArray([1])).toEqual([1]);
    expect(asString(1)).toBe("");
    expect(asString("x")).toBe("x");
  });

  test("operations come sorted by path and then in method order", () => {
    expect(operationsOf(DOC).map((o) => `${o.method} ${o.path}`)).toEqual([
      "GET /a",
      "DELETE /a",
      "POST /b",
    ]);
    expect(operationsOf({})).toEqual([]);
  });

  test("references resolve inside the document only", () => {
    expect(resolveRef(DOC, "#/components/schemas/B")).toEqual({ type: "string" });
    expect(resolveRef(DOC, "#/components/schemas/a~1b")).toBe(1);
    expect(resolveRef(DOC, "#/components/nothing")).toBeUndefined();
    expect(resolveRef(DOC, "#/components/schemas/B/type/x")).toBeUndefined();
    expect(resolveRef(DOC, "https://example.test/x")).toBeUndefined();
    expect(deref(DOC, { $ref: "#/components/schemas/A" })).toEqual({ type: "string" });
    expect(deref(DOC, { $ref: "#/nothing" })).toBeUndefined();
    expect(deref(DOC, 5)).toBe(5);
    const loop = { components: { schemas: { L: { $ref: "#/components/schemas/L" } } } };
    expect(deref(loop, { $ref: "#/components/schemas/L" })).toEqual({
      $ref: "#/components/schemas/L",
    });
  });

  test("only a JSON object parses", () => {
    expect(parseObject('{"a":1}')).toEqual({ a: 1 });
    expect(parseObject("[1]")).toBeNull();
    expect(parseObject("{")).toBeNull();
  });
});
