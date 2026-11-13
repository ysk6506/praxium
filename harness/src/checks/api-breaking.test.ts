import { describe, expect, test } from "vitest";
import { API_CONFIG, healthyContract } from "../test-support/api-fixture";
import { breaksBetween, checkBreaking, type Declarations, readDeclarations } from "./api-breaking";
import { asObject, type JsonObject } from "./api-json";
import { surfaceOf } from "./api-surface";
import { bodyFingerprint } from "./doc-print";

const base = (): JsonObject => structuredClone(healthyContract());
const op = (doc: JsonObject, p: string, m: string): JsonObject =>
  asObject(asObject(asObject(doc.paths)[p])[m]);
const schemas = (doc: JsonObject): JsonObject => asObject(asObject(doc.components).schemas);
const props = (doc: JsonObject, name: string): JsonObject =>
  asObject(asObject(schemas(doc)[name]).properties);
const keys = (before: JsonObject, after: JsonObject): string[] =>
  breaksBetween(surfaceOf(before), surfaceOf(after)).map((b) => b.key);
const GET = "GET /v1/things";
const OK = `${GET}#200`;

const put = (target: JsonObject, key: string, value: unknown): void => {
  target[key] = value;
};
const unset = (target: JsonObject, key: string): void => {
  delete target[key];
};

const NONE: Declarations = { keys: new Set(), violations: [] };

describe("breaking changes against the base [REQ-143]", () => {
  test("an unchanged contract and additions are not breaks", () => {
    expect(keys(base(), base())).toEqual([]);
    const after = base();
    asObject(after.paths)["/v1/new-things"] = structuredClone(
      asObject(asObject(after.paths)["/v1/things"]),
    );
    asObject(props(after, "Thing")).extra = { type: "string", description: "new" };
    asObject(op(after, "/v1/things", "get").responses)["404"] = {
      $ref: "#/components/responses/NotFound",
    };
    op(after, "/v1/things", "get")["x-required-role"] = ["read", "viewer"];
    (op(after, "/v1/things", "get").parameters as unknown[]).push({
      name: "q",
      in: "query",
      required: false,
      description: "x",
      schema: { type: "string" },
    });
    expect(keys(base(), after)).toEqual([]);
  });

  test.each<[string, string, (doc: JsonObject) => void]>([
    [
      GET,
      "an operation is removed",
      (d) => unset(asObject(asObject(d.paths)["/v1/things"]), "get"),
    ],
    [
      `${GET} roles`,
      "a role is no longer accepted",
      (d) => put(op(d, "/v1/things", "get"), "x-required-role", ["admin"]),
    ],
    [
      `${GET} param query:limit`,
      "a parameter is removed",
      (d) => put(op(d, "/v1/things", "get"), "parameters", []),
    ],
    [
      OK,
      "a response status is removed",
      (d) => unset(asObject(op(d, "/v1/things", "get").responses), "200"),
    ],
    [`${OK} /data[]/kind`, "a response field is removed", (d) => unset(props(d, "Thing"), "kind")],
    [
      `${OK} /data[]/id`,
      "a response field changes type",
      (d) => put(props(d, "Thing"), "id", { type: "integer", description: "x" }),
    ],
    [
      `${OK} /data[]/kind`,
      "an enum value is removed",
      (d) => put(asObject(props(d, "Thing").kind), "enum", ["a"]),
    ],
    [
      `${OK} /data[]/kind`,
      "a response field stops being required",
      (d) => put(asObject(schemas(d).Thing), "required", ["id"]),
    ],
    [
      "code not_found.resource",
      "a code changes status",
      (d) =>
        put(asObject(asObject(schemas(d).ProblemCode)["x-catalogue"]), "not_found.resource", {
          group: "not_found",
          status: 410,
        }),
    ],
    [
      "code not_found.resource",
      "a code is removed",
      (d) => {
        asObject(schemas(d).ProblemCode).enum = [
          "auth.token_missing",
          "invalid_input.not_acceptable",
        ];
      },
    ],
  ])("%s: %s", (key, _what, change) => {
    const after = base();
    change(after);
    expect(keys(base(), after)).toContain(key);
  });

  test("a removed operation explains the facts below it, a removed response its fields", () => {
    const after = base();
    delete asObject(asObject(after.paths)["/v1/things"]).get;
    expect(keys(base(), after)).toEqual([GET]);
    const noOk = base();
    delete asObject(op(noOk, "/v1/things", "get").responses)["200"];
    expect(keys(base(), noOk)).toEqual([OK]);
  });

  test("a new required parameter or request field is a break, but not on a new operation", () => {
    const required = base();
    (op(required, "/v1/things", "get").parameters as unknown[]).push({
      name: "q",
      in: "query",
      required: true,
      description: "x",
      schema: { type: "string" },
    });
    expect(keys(base(), required)).toEqual([`${GET} param query:q`]);
    const optionalNow = base();
    (op(optionalNow, "/v1/things", "get").parameters as JsonObject[])[0] = {
      name: "limit",
      in: "query",
      required: true,
      description: "x",
      schema: { type: "string" },
    };
    expect(keys(base(), optionalNow)).toEqual([`${GET} param query:limit`]);
    const body = (required: boolean): JsonObject => {
      const doc = base();
      op(doc, "/v1/things", "get").requestBody = {
        required,
        content: {
          "application/json": {
            schema: {
              type: "object",
              description: "x",
              properties: { n: { type: "string", description: "x" } },
            },
          },
        },
      };
      return doc;
    };
    expect(keys(base(), body(true))).toEqual(["GET /v1/things body application/json /"]);
    expect(keys(body(false), body(true))).toContain("GET /v1/things body application/json /");
    const fresh = base();
    asObject(fresh.paths)["/v1/fresh"] = {
      get: {
        ...structuredClone(op(fresh, "/v1/things", "get")),
        parameters: [
          { name: "q", in: "query", required: true, description: "x", schema: { type: "string" } },
        ],
      },
    };
    expect(keys(base(), fresh)).toEqual([]);
  });

  test("a recursive schema does not run forever", () => {
    const loop = base();
    asObject(props(loop, "Thing")).parent = { $ref: "#/components/schemas/Thing" };
    expect(keys(loop, loop)).toEqual([]);
    expect(surfaceOf(loop).size).toBeGreaterThan(10);
  });
});

const sealed = (data: Record<string, unknown>, body = "Body\n"): string => {
  const fingerprint = bodyFingerprint(body);
  const front = Object.entries({ title: "t", decision_hash: fingerprint, ...data })
    .map(([k, v]) => `${k}: ${JSON.stringify(v)}`)
    .join("\n");
  return `---\n${front}\n---\n${body}`;
};
const REASON = "The old field was never used by any client and removing it saves space.";

describe("a break is declared by a new sealed ADR [REQ-143]", () => {
  test("the keys of a sealed ADR with a reason are accepted", () => {
    const found = readDeclarations(API_CONFIG, [
      {
        file: "0002-a.md",
        text: sealed({ breaks_contract: [GET, OK], contract_break_reason: REASON }),
      },
      { file: "0003-b.md", text: "---\ntitle: plain\n---\nBody\n" },
    ]);
    expect([...found.keys]).toEqual([GET, OK]);
    expect(found.violations).toEqual([]);
  });

  test.each([
    ["not a list", { breaks_contract: GET, contract_break_reason: REASON }],
    ["an empty list", { breaks_contract: [], contract_break_reason: REASON }],
    ["no reason", { breaks_contract: [GET] }],
    ["a short reason", { breaks_contract: [GET], contract_break_reason: "too short" }],
  ])("API031: %s", (_name, data) => {
    const found = readDeclarations(API_CONFIG, [{ file: "0002-a.md", text: sealed(data) }]);
    expect(found.violations.map((v) => v.code)).toEqual(["API031"]);
    expect(found.keys.size).toBe(0);
  });

  test("API031: an ADR that is not sealed", () => {
    const text = sealed({ breaks_contract: [GET], contract_break_reason: REASON }).replace(
      "Body",
      "Edited body",
    );
    expect(
      readDeclarations(API_CONFIG, [{ file: "0002-a.md", text }]).violations.map((v) => v.code),
    ).toEqual(["API031"]);
  });
});

describe("the verdict on a change [REQ-143]", () => {
  const broken = (): JsonObject => {
    const doc = base();
    unset(asObject(op(doc, "/v1/health/live", "get").responses), "406");
    return doc;
  };
  const baseText = JSON.stringify(base());

  test("a compatible change passes, with or without a base", () => {
    expect(checkBreaking(API_CONFIG, base(), baseText, NONE)).toEqual([]);
    expect(checkBreaking(API_CONFIG, broken(), null, NONE)).toEqual([]);
    expect(checkBreaking(API_CONFIG, broken(), "not json", NONE)).toEqual([]);
  });

  test("API030: a break nobody declared", () => {
    const [found] = checkBreaking(API_CONFIG, broken(), baseText, NONE);
    expect(found?.code).toBe("API030");
    expect(found?.message).toContain("[GET /v1/health/live#406]");
  });

  test("a declared break passes, and a declaration of nothing is stale (API032)", () => {
    const declared: Declarations = { keys: new Set(["GET /v1/health/live#406"]), violations: [] };
    expect(checkBreaking(API_CONFIG, broken(), baseText, declared)).toEqual([]);
    const stale = checkBreaking(API_CONFIG, base(), baseText, declared);
    expect(stale.map((v) => v.code)).toEqual(["API032"]);
  });
});
