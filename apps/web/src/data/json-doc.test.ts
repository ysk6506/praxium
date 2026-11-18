import fc from "fast-check";
import { describe, expect, it } from "vitest";
import {
  attachIssues,
  childPointer,
  formatPointer,
  getAt,
  hasIssueUnder,
  insertItem,
  type Json,
  moveItem,
  PointerError,
  parsePointer,
  removeAt,
  removeItem,
  setAt,
  writeValue,
} from "./json-doc";

// Keys that tend to break pointer code: separators, escapes, names that exist on every object, and index look-alikes.
const key = fc.oneof(
  fc.string({ maxLength: 5 }),
  fc.constantFrom("a", "b", "/", "~", "~1", "~0", "__proto__", "constructor", "toString", "0", "-"),
);
const scalar: fc.Arbitrary<Json> = fc.oneof(
  fc.constant(null),
  fc.boolean(),
  fc.integer(),
  fc.string({ maxLength: 6 }),
);
const doc: fc.Arbitrary<Json> = fc.letrec<{ tree: Json }>((tie) => ({
  tree: fc.oneof(
    { depthSize: "small" },
    scalar,
    fc.array(tie("tree"), { maxLength: 4 }),
    fc.dictionary(key, tie("tree"), { maxKeys: 4 }),
  ),
})).tree;

/** Every pointer the document answers, the whole document ("") first. */
function pointersOf(node: Json, at = ""): string[] {
  const out = [at];
  if (Array.isArray(node)) {
    node.forEach((child, i) => {
      out.push(...pointersOf(child, childPointer(at, i)));
    });
  } else if (typeof node === "object" && node !== null) {
    for (const [name, child] of Object.entries(node)) {
      out.push(...pointersOf(child, childPointer(at, name)));
    }
  }
  return out;
}

const same = (a: unknown, b: unknown): void => {
  expect(JSON.stringify(a)).toBe(JSON.stringify(b));
};
const below = (p: string, ancestor: string): boolean =>
  p === ancestor || p.startsWith(`${ancestor}/`);
const inner = (d: Json): string[] => pointersOf(d).filter((p) => p !== "");
const RUNS = { numRuns: 200 };

describe("[REQ-408] a pointer is read and written by the rules of RFC 6901", () => {
  it("reads the steps of a pointer, with the two escapes", () => {
    expect(parsePointer("")).toEqual([]);
    expect(parsePointer("/a/b~1c/~0d")).toEqual(["a", "b/c", "~d"]);
    expect(parsePointer("/")).toEqual([""]);
    expect(() => parsePointer("a/b")).toThrow(PointerError);
    expect(() => parsePointer("/a~2")).toThrow(PointerError);
  });

  it("writes the steps back to the same pointer", () => {
    fc.assert(
      fc.property(fc.array(key, { maxLength: 5 }), (tokens) => {
        expect(parsePointer(formatPointer(tokens))).toEqual(tokens);
      }),
      RUNS,
    );
  });

  it("reads what is there and says undefined for what is not (null is a value)", () => {
    const d = { a: { list: [10, null] }, "x/y": "z" };
    expect(getAt(d, "")).toBe(d);
    expect(getAt(d, "/a/list/0")).toBe(10);
    expect(getAt(d, "/a/list/1")).toBeNull();
    expect(getAt(d, "/a/list/2")).toBeUndefined();
    expect(getAt(d, "/x~1y")).toBe("z");
    expect(getAt(d, "/a/missing/deeper")).toBeUndefined();
    expect(getAt(d, "/a/list/01")).toBeUndefined();
    expect(getAt(d, "/toString")).toBeUndefined();
    expect(getAt(d, "/constructor")).toBeUndefined();
  });

  it("does not let a key named like a prototype member reach the prototype", () => {
    const written = setAt({}, "/__proto__", { polluted: true });
    expect(Object.hasOwn(written as object, "__proto__")).toBe(true);
    expect(({} as { polluted?: boolean }).polluted).toBeUndefined();
    expect(getAt(written, "/__proto__/polluted")).toBe(true);
  });
});

describe("[REQ-408] writing a value and reading it back gives the value", () => {
  it("returns what was written at any place the document has", () => {
    fc.assert(
      fc.property(doc, scalar, fc.nat(), (d, value, pick) => {
        const places = inner(d);
        fc.pre(places.length > 0);
        const p = places[pick % places.length] as string;
        expect(getAt(setAt(d, p, value), p)).toEqual(value);
      }),
      RUNS,
    );
  });

  it("returns what was written under a key the object did not have", () => {
    fc.assert(
      fc.property(doc, key, scalar, fc.nat(), (d, name, value, pick) => {
        const objects = pointersOf(d).filter((p) => {
          const node = getAt(d, p);
          return typeof node === "object" && node !== null && !Array.isArray(node);
        });
        fc.pre(objects.length > 0);
        const at = objects[pick % objects.length] as string;
        fc.pre(getAt(d, childPointer(at, name)) === undefined);
        const p = childPointer(at, name);
        expect(getAt(setAt(d, p, value), p)).toEqual(value);
      }),
      RUNS,
    );
  });

  it("makes the steps that are missing: objects, and a list for an index", () => {
    expect(setAt({}, "/a/b", 1)).toEqual({ a: { b: 1 } });
    expect(setAt({}, "/items/0/name", "x")).toEqual({ items: [{ name: "x" }] });
    expect(setAt({ items: [1] }, "/items/-", 2)).toEqual({ items: [1, 2] });
    expect(setAt({ items: [1] }, "/items/1", 2)).toEqual({ items: [1, 2] });
    expect(setAt(5, "", 6)).toBe(6);
  });

  it("refuses a place the document cannot have", () => {
    expect(() => setAt({ a: "text" }, "/a/b", 1)).toThrow(PointerError);
    expect(() => setAt({ items: [1] }, "/items/5", 1)).toThrow(PointerError);
    expect(() => setAt({ items: [1] }, "/items/x", 1)).toThrow(PointerError);
    expect(() => setAt({ a: null }, "/a/b", 1)).toThrow(PointerError);
  });
});

describe("[REQ-408] a write leaves the rest of the document as it was", () => {
  it("keeps every place that is neither above nor below the one written", () => {
    fc.assert(
      fc.property(doc, scalar, fc.nat(), (d, value, pick) => {
        const places = inner(d);
        fc.pre(places.length > 0);
        const p = places[pick % places.length] as string;
        const written = setAt(d, p, value);
        for (const q of places) {
          if (below(q, p) || below(p, q)) continue;
          expect(getAt(written, q)).toEqual(getAt(d, q));
        }
      }),
      RUNS,
    );
  });

  it("keeps the keys it does not know, in their order", () => {
    const d = { known: "a", unknown: { deep: [1, 2] }, other: null };
    same(setAt(d, "/known", "b"), { known: "b", unknown: { deep: [1, 2] }, other: null });
    same(writeValue(d, "/known", ""), { unknown: { deep: [1, 2] }, other: null });
  });

  it("does not change the document it was given", () => {
    fc.assert(
      fc.property(doc, scalar, fc.nat(), (d, value, pick) => {
        const before = JSON.stringify(d);
        const places = inner(d);
        fc.pre(places.length > 0);
        const p = places[pick % places.length] as string;
        setAt(d, p, value);
        removeAt(d, p);
        writeValue(d, p, "");
        expect(JSON.stringify(d)).toBe(before);
      }),
      RUNS,
    );
  });

  it("shares what did not change", () => {
    const d = { a: { x: 1 }, b: { y: 2 } };
    const written = setAt(d, "/a/x", 5) as typeof d;
    expect(written.b).toBe(d.b);
    expect(removeAt(d, "/c")).toBe(d);
    expect(removeAt(d, "/a/z/deeper")).toBe(d);
  });
});

describe("[REQ-408] writing and then removing gives the document back", () => {
  it("removes a key the object did not have before", () => {
    fc.assert(
      fc.property(doc, key, scalar, fc.nat(), (d, name, value, pick) => {
        const objects = pointersOf(d).filter((p) => {
          const node = getAt(d, p);
          return typeof node === "object" && node !== null && !Array.isArray(node);
        });
        fc.pre(objects.length > 0);
        const at = objects[pick % objects.length] as string;
        const p = childPointer(at, name);
        fc.pre(getAt(d, p) === undefined);
        same(removeAt(setAt(d, p, value), p), d);
      }),
      RUNS,
    );
  });

  it("removes an item added at the end of a list", () => {
    fc.assert(
      fc.property(doc, scalar, fc.nat(), (d, value, pick) => {
        const lists = pointersOf(d).filter((p) => Array.isArray(getAt(d, p)));
        fc.pre(lists.length > 0);
        const at = lists[pick % lists.length] as string;
        const length = (getAt(d, at) as Json[]).length;
        const added = setAt(d, `${at}/-`, value);
        same(removeAt(added, childPointer(at, length)), d);
      }),
      RUNS,
    );
  });

  it("takes the key out for an empty text or no value, and writes 0, false and null", () => {
    const d = { a: "x", b: { c: "y" } };
    same(writeValue(d, "/a", ""), { b: { c: "y" } });
    same(writeValue(d, "/a", undefined), { b: { c: "y" } });
    same(writeValue(d, "/b/c", ""), { a: "x", b: {} });
    same(writeValue(d, "/a", 0), { a: 0, b: { c: "y" } });
    same(writeValue(d, "/a", false), { a: false, b: { c: "y" } });
    same(writeValue(d, "/a", null), { a: null, b: { c: "y" } });
    same(writeValue(d, "/missing", ""), d);
    expect(() => removeAt(d, "")).toThrow(PointerError);
  });

  it("removes an item of a list and lets the later items move up", () => {
    same(removeAt({ l: ["a", "b", "c"] }, "/l/1"), { l: ["a", "c"] });
    same(removeAt({ l: ["a"] }, "/l/3"), { l: ["a"] });
  });
});

describe("[REQ-408] the items of a list are added, removed and moved", () => {
  const listOf = fc.array(fc.string({ maxLength: 4 }), { minLength: 1, maxLength: 6 });

  it("takes an item out again after putting it in", () => {
    fc.assert(
      fc.property(listOf, fc.string({ maxLength: 4 }), fc.nat(), (items, item, pick) => {
        const d = { list: items };
        const at = pick % (items.length + 1);
        same(removeItem(insertItem(d, "/list", item, at), "/list", at), d);
      }),
      RUNS,
    );
  });

  it("puts an item back by moving it back", () => {
    fc.assert(
      fc.property(listOf, fc.nat(), fc.nat(), (items, a, b) => {
        const d = { list: items, other: 1 };
        const [from, to] = [a % items.length, b % items.length];
        const moved = moveItem(d, "/list", from, to);
        same(moveItem(moved, "/list", to, from), d);
        expect((getAt(moved, "/list") as string[])[to]).toBe(items[from]);
        expect([...(getAt(moved, "/list") as string[])].sort()).toEqual([...items].sort());
        expect(getAt(moved, "/other")).toBe(1);
      }),
      RUNS,
    );
  });

  it("moves one item one place, with the keyboard's step", () => {
    same(moveItem({ l: ["a", "b", "c"] }, "/l", 1, 0), { l: ["b", "a", "c"] });
    same(moveItem({ l: ["a", "b", "c"] }, "/l", 1, 2), { l: ["a", "c", "b"] });
    same(insertItem({ l: ["a"] }, "/l", "z"), { l: ["a", "z"] });
  });

  it("refuses a place the list does not have, and a value that is not a list", () => {
    const d = { l: ["a", "b"], t: "x" };
    expect(() => moveItem(d, "/l", 0, 2)).toThrow(PointerError);
    expect(() => moveItem(d, "/l", -1, 0)).toThrow(PointerError);
    expect(() => removeItem(d, "/l", 2)).toThrow(PointerError);
    expect(() => insertItem(d, "/l", "z", 3)).toThrow(PointerError);
    expect(() => insertItem(d, "/t", "z")).toThrow(PointerError);
    expect(() => insertItem(d, "/missing", "z")).toThrow(PointerError);
  });
});

describe("[REQ-408] an error of the server is tied to the field that shows it", () => {
  const fields = ["/name", "/items/0", "/items/0/label", "/items/1"];

  it("goes to the field with the same pointer, else to the closest field above", () => {
    const attached = attachIssues(
      [
        { pointer: "/name", code: "invalid_input.field_required" },
        { pointer: "/items/0/label", code: "invalid_input.field_format" },
        { pointer: "/items/1/value", code: "invalid_input.field" },
        { pointer: "/name", code: "invalid_input.field_format" },
      ],
      fields,
    );
    expect(Object.fromEntries(attached.byField)).toEqual({
      "/name": ["invalid_input.field_required", "invalid_input.field_format"],
      "/items/0/label": ["invalid_input.field_format"],
      "/items/1": ["invalid_input.field"],
    });
    expect(attached.unattached).toEqual([]);
  });

  it("keeps the issue apart when no field is at or above it", () => {
    const attached = attachIssues(
      [
        { pointer: "/other", code: "invalid_input.field" },
        { pointer: "", code: "invalid_input.field" },
        { pointer: "/names", code: "invalid_input.field" },
      ],
      ["/name"],
    );
    expect(attached.byField.size).toBe(0);
    expect(attached.unattached.map((i) => i.pointer)).toEqual(["/other", "", "/names"]);
  });

  it("does not take a field whose name only begins like the pointer", () => {
    const attached = attachIssues([{ pointer: "/items/10", code: "c" }], ["/items/1"]);
    expect(attached.byField.size).toBe(0);
  });

  it("tells a list that one of its items has a problem", () => {
    const issues = [{ pointer: "/items/2/name", code: "c" }];
    expect(hasIssueUnder(issues, "/items")).toBe(true);
    expect(hasIssueUnder(issues, "/items/2")).toBe(true);
    expect(hasIssueUnder(issues, "/items/20")).toBe(false);
    expect(hasIssueUnder(issues, "/other")).toBe(false);
  });
});
