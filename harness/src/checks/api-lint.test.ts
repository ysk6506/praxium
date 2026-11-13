import { describe, expect, test } from "vitest";
import { API_CONFIG, healthyContract } from "../test-support/api-fixture";
import { asObject, type JsonObject } from "./api-json";
import { lintDocument } from "./api-lint";

const lint = (doc: JsonObject) => lintDocument(doc, API_CONFIG, "openapi.json");

const path = (doc: JsonObject, p: string): JsonObject => asObject(asObject(doc.paths)[p]);
const op = (doc: JsonObject, p: string, method: string): JsonObject =>
  asObject(path(doc, p)[method]);
const responses = (doc: JsonObject, p: string, method: string): JsonObject =>
  asObject(op(doc, p, method).responses);
const schemas = (doc: JsonObject): JsonObject => asObject(asObject(doc.components).schemas);
const props = (doc: JsonObject, name: string): JsonObject =>
  asObject(asObject(schemas(doc)[name]).properties);
const shared = (doc: JsonObject, name: string): JsonObject =>
  asObject(asObject(asObject(doc.components).responses)[name]);
const problemMedia = (doc: JsonObject, name: string): JsonObject =>
  asObject(asObject(shared(doc, name).content)["application/problem+json"]);
const catalogue = (doc: JsonObject): JsonObject =>
  asObject(asObject(schemas(doc).ProblemCode)["x-catalogue"]);

const put = (target: JsonObject, key: string, value: unknown): void => {
  target[key] = value;
};
const unset = (target: JsonObject, key: string): void => {
  delete target[key];
};
const clone = (doc: JsonObject, from: string, to: string): void =>
  put(asObject(doc.paths), to, structuredClone(path(doc, from)));
const queryParam = (name: string, describe = true): JsonObject => ({
  name,
  in: "query",
  ...(describe ? { description: "x" } : {}),
  schema: { type: "string" },
});

type Mutation = (doc: JsonObject) => void;
const GETS = "/v1/things";
const PATCHES = "/v1/things/{thing_id}";
const ACTS = "/v1/things/{thing_id}:approve";
const ASK = "/v1/kgs/{kg}/questions/{question_name}";

const MUTATIONS: Array<[code: string, what: string, change: Mutation]> = [
  ["API001", "the version is not 3.1", (d) => put(d, "openapi", "3.0.3")],
  ["API001", "no title", (d) => put(d, "info", { version: "1" })],
  ["API002", "an upper-case segment", (d) => clone(d, GETS, "/v1/Things")],
  ["API002", "a singular resource", (d) => clone(d, GETS, "/v1/thing")],
  ["API002", "a camel-case parameter", (d) => clone(d, GETS, "/v1/things/{thingId}")],
  ["API003", "no operationId", (d) => unset(op(d, GETS, "get"), "operationId")],
  [
    "API003",
    "the same operationId twice",
    (d) => put(op(d, PATCHES, "patch"), "operationId", "listThings"),
  ],
  ["API004", "no x-required-role", (d) => unset(op(d, GETS, "get"), "x-required-role")],
  ["API004", "an unknown role", (d) => put(op(d, GETS, "get"), "x-required-role", ["root"])],
  ["API004", "x-safe is not a boolean", (d) => put(op(d, GETS, "get"), "x-safe", "yes")],
  ["API004", "no x-long-running", (d) => unset(op(d, GETS, "get"), "x-long-running")],
  [
    "API005",
    "a response without an example",
    (d) => {
      const ok = asObject(responses(d, GETS, "get")["200"]);
      unset(asObject(asObject(ok.content)["application/json"]), "example");
    },
  ],
  [
    "API005",
    "a shared error response without an example",
    (d) => unset(problemMedia(d, "NotFound"), "example"),
  ],
  [
    "API006",
    "an example with a code outside the list",
    (d) => put(asObject(problemMedia(d, "NotFound").example), "code", "made.up"),
  ],
  ["API006", "no list of codes", (d) => unset(schemas(d), "ProblemCode")],
  [
    "API006",
    "a code without its status",
    (d) => put(catalogue(d), "not_found.resource", { group: "auth" }),
  ],
  [
    "API006",
    "an extra x-catalogue entry",
    (d) => put(catalogue(d), "extra.code", { group: "extra", status: 400 }),
  ],
  [
    "API007",
    "a list without a limit",
    (d) => put(op(d, GETS, "get"), "parameters", [{ $ref: "#/components/parameters/Cursor" }]),
  ],
  [
    "API007",
    "a list meta without result_complete",
    (d) => put(asObject(schemas(d).ListMeta), "properties", {}),
  ],
  [
    "API008",
    "an error response that is not a problem",
    (d) =>
      put(responses(d, GETS, "get"), "404", {
        description: "no",
        content: {
          "application/json": { schema: { type: "object", description: "x" }, example: {} },
        },
      }),
  ],
  ["API008", "no 401", (d) => unset(responses(d, GETS, "get"), "401")],
  ["API008", "no 406", (d) => unset(responses(d, GETS, "get"), "406")],
  ["API009", "a keyword outside the subset", (d) => put(asObject(schemas(d).Thing), "oneOf", [])],
  [
    "API009",
    "an inline schema with a bad keyword",
    (d) =>
      put(op(d, GETS, "get"), "parameters", [
        { name: "x", in: "query", description: "x", schema: { allOf: [] } },
      ]),
  ],
  [
    "API010",
    "a reference that does not resolve",
    (d) => put(props(d, "ThingList"), "meta", { $ref: "#/components/schemas/Nothing" }),
  ],
  [
    "API010",
    "a response reference that does not resolve",
    (d) => put(responses(d, GETS, "get"), "404", { $ref: "#/components/responses/Nothing" }),
  ],
  [
    "API010",
    "a parameter reference that does not resolve",
    (d) => put(op(d, GETS, "get"), "parameters", [{ $ref: "#/components/parameters/Nothing" }]),
  ],
  [
    "API010",
    "a remote reference",
    (d) => put(props(d, "ThingList"), "meta", { $ref: "https://example.test/x" }),
  ],
  [
    "API011",
    "a camel-case property",
    (d) => put(props(d, "Thing"), "thingId", { type: "string", description: "x" }),
  ],
  [
    "API011",
    "a camel-case query parameter",
    (d) => put(op(d, GETS, "get"), "parameters", [queryParam("pageSize")]),
  ],
  [
    "API012",
    "a property without a description",
    (d) => put(props(d, "Thing"), "id", { type: "string" }),
  ],
  ["API012", "an operation without a summary", (d) => unset(op(d, GETS, "get"), "summary")],
  [
    "API012",
    "a parameter without a description",
    (d) => put(op(d, GETS, "get"), "parameters", [queryParam("x", false)]),
  ],
  [
    "API012",
    "required names a missing property",
    (d) => put(asObject(schemas(d).Thing), "required", ["id", "ghost"]),
  ],
  [
    "API013",
    "a creating POST without Idempotency-Key",
    (d) => put(op(d, ACTS, "post"), "parameters", []),
  ],
  [
    "API013",
    "a safe POST with Idempotency-Key",
    (d) =>
      put(op(d, ASK, "post"), "parameters", [{ $ref: "#/components/parameters/IdempotencyKey" }]),
  ],
  ["API013", "a PATCH without If-Match", (d) => put(op(d, PATCHES, "patch"), "parameters", [])],
  ["API014", "a KG resource outside /v1/kgs/{kg}", (d) => clone(d, GETS, "/v1/objects")],
  ["API015", "a verb as a segment", (d) => clone(d, GETS, "/v1/things/approve")],
  ["API015", "an action in capitals", (d) => clone(d, GETS, "/v1/things/{thing_id}:Approve")],
  [
    "API016",
    "an anonymous operation that asks for a login",
    (d) => unset(op(d, "/v1/health/live", "get"), "security"),
  ],
  [
    "API016",
    "an open security on a protected operation",
    (d) => put(op(d, GETS, "get"), "security", []),
  ],
  ["API016", "no security scheme", (d) => put(asObject(d.components), "securitySchemes", {})],
  ["API017", "a GET that is not safe", (d) => put(op(d, GETS, "get"), "x-safe", false)],
  ["API017", "a PATCH that is safe", (d) => put(op(d, PATCHES, "patch"), "x-safe", true)],
  ["API018", "a path outside /v1", (d) => clone(d, GETS, "/things")],
];

describe("contract lint [REQ-143]", () => {
  test("a healthy contract has no violation", () => {
    expect(lint(healthyContract())).toEqual([]);
  });

  test.each(MUTATIONS)("%s: %s", (code, _what, change) => {
    const doc = structuredClone(healthyContract());
    change(doc);
    expect(lint(doc).map((v) => v.code)).toContain(code);
  });

  test("a message names the place and the way out", () => {
    const doc = structuredClone(healthyContract());
    unset(op(doc, GETS, "get"), "operationId");
    const found = lint(doc).find((v) => v.code === "API003");
    expect(found?.message).toContain("GET /v1/things");
    expect(found?.fix.length).toBeGreaterThan(10);
  });
});
