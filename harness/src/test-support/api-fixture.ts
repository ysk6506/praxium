/** A small healthy contract, its configuration and a throwaway repository around them, for the API check tests. */
import type { ApiConfig } from "../checks/api-config";
import type { JsonObject } from "../checks/api-json";
import { miniRepo } from "./mini-repo";

export const API_CONFIG: ApiConfig = {
  openapi: "backend/internal/contract/openapi/openapi.json",
  parts: "backend/internal/contract/openapi/parts",
  planned: "backend/internal/contract/planned/routes.json",
  feature_routes: "docs/design-docs/rest-api-feature-routes.md",
  adr_dir: "docs/design-docs/adr",
  go_root: "backend",
  program: "./cmd/praxium",
  assemble_args: ["contract", "assemble"],
  routes_args: ["api", "routes", "--json"],
  schema_keywords_source: "backend/internal/contract/validate.go",
  schema_keywords: [
    "$ref",
    "type",
    "properties",
    "required",
    "items",
    "enum",
    "description",
    "example",
  ],
  roles: ["anonymous", "authenticated", "read", "viewer", "editor", "admin"],
  phases: ["alpha-1", "alpha-2"],
  singular_segments: ["health", "live", "catalog"],
  kg_resources: ["objects", "questions"],
  action_verbs: ["approve", "reject"],
};

const ref = (kind: string, name: string): JsonObject => ({ $ref: `#/components/${kind}/${name}` });

const problem = (name: string, code: string, status: number): [string, JsonObject] => [
  name,
  {
    description: `The ${name} answer.`,
    content: {
      "application/problem+json": {
        schema: ref("schemas", "Problem"),
        example: { type: `/v1/problems/${code}`, title: "t", status, code, trace_id: "tr-1" },
      },
    },
  },
];

function operation(over: JsonObject): JsonObject {
  return {
    operationId: "listThings",
    summary: "List things",
    description: "Lists things.",
    "x-required-role": ["read"],
    "x-safe": true,
    "x-long-running": false,
    parameters: [ref("parameters", "Limit"), ref("parameters", "Cursor")],
    responses: {
      "200": {
        description: "A page.",
        content: {
          "application/json": {
            schema: ref("schemas", "ThingList"),
            example: { data: [], meta: {} },
          },
        },
      },
      "401": ref("responses", "Unauthorized"),
      "406": ref("responses", "NotAcceptable"),
    },
    ...over,
  };
}

/** A contract that passes every rule of the lint. Tests copy it (with `structuredClone`) and break one thing. */
export function healthyContract(): JsonObject {
  const parameter = (name: string, where: string): JsonObject => ({
    name,
    in: where,
    required: false,
    description: `The ${name}.`,
    schema: { type: "string" },
  });
  return {
    openapi: "3.1.0",
    info: { title: "Test API", version: "1.0.0" },
    security: [{ bearerAuth: [] }],
    components: {
      securitySchemes: { bearerAuth: { type: "http", scheme: "bearer" } },
      parameters: {
        Limit: parameter("limit", "query"),
        Cursor: parameter("cursor", "query"),
        IdempotencyKey: parameter("Idempotency-Key", "header"),
        IfMatch: parameter("If-Match", "header"),
      },
      responses: Object.fromEntries([
        problem("Unauthorized", "auth.token_missing", 401),
        problem("NotAcceptable", "invalid_input.not_acceptable", 406),
        problem("NotFound", "not_found.resource", 404),
      ]),
      schemas: {
        ProblemCode: {
          type: "string",
          description: "The codes.",
          enum: ["auth.token_missing", "invalid_input.not_acceptable", "not_found.resource"],
          "x-catalogue": {
            "auth.token_missing": { group: "auth", status: 401 },
            "invalid_input.not_acceptable": { group: "invalid_input", status: 406 },
            "not_found.resource": { group: "not_found", status: 404 },
          },
        },
        Problem: {
          type: "object",
          description: "An error.",
          properties: {
            type: { type: "string", description: "Address." },
            title: { type: "string", description: "Title." },
            status: { type: "integer", description: "Status." },
            code: ref("schemas", "ProblemCode"),
            trace_id: { type: "string", description: "Trace." },
          },
          required: ["type", "title", "status", "code", "trace_id"],
        },
        Thing: {
          type: "object",
          description: "A thing.",
          properties: {
            id: { type: "string", description: "Id." },
            kind: { type: "string", description: "Kind.", enum: ["a", "b"] },
          },
          required: ["id", "kind"],
        },
        ListMeta: {
          type: "object",
          description: "List meta.",
          properties: { result_complete: { type: "object", description: "Done." } },
          required: ["result_complete"],
        },
        ThingList: {
          type: "object",
          description: "Things.",
          properties: {
            data: { type: "array", description: "Items.", items: ref("schemas", "Thing") },
            meta: ref("schemas", "ListMeta"),
          },
          required: ["data", "meta"],
        },
      },
    },
    paths: {
      "/v1/things": { get: operation({}) },
      "/v1/health/live": {
        get: operation({
          operationId: "getHealthLive",
          "x-required-role": ["anonymous"],
          security: [],
          parameters: [],
          responses: {
            "200": {
              description: "Live.",
              content: {
                "application/json": {
                  schema: { type: "object", description: "Live." },
                  example: {},
                },
              },
            },
            "406": ref("responses", "NotAcceptable"),
          },
        }),
      },
      "/v1/things/{thing_id}": {
        patch: operation({
          operationId: "patchThing",
          "x-safe": false,
          parameters: [ref("parameters", "IfMatch")],
          responses: {
            "200": {
              description: "Changed.",
              content: {
                "application/json": {
                  schema: ref("schemas", "Thing"),
                  example: { id: "x", kind: "a" },
                },
              },
            },
            "401": ref("responses", "Unauthorized"),
            "406": ref("responses", "NotAcceptable"),
          },
        }),
      },
      "/v1/things/{thing_id}:approve": {
        post: operation({
          operationId: "approveThing",
          "x-safe": false,
          parameters: [ref("parameters", "IdempotencyKey")],
        }),
      },
      "/v1/kgs/{kg}/questions/{question_name}": {
        post: operation({ operationId: "askImpact", "x-safe": true, parameters: [] }),
      },
    },
  };
}

export const FEATURE_TABLE = [
  "# Routes",
  "",
  "| 기능 | 길 |",
  "|---|---|",
  "| 보기 | `GET /v1/things`, `PATCH /v1/things/{thing_id}` |",
  "| 승인 | `POST /v1/things/{thing_id}:approve` |",
  "| 산 | `GET /v1/health/live`, `POST /v1/kgs/{kg}/questions/{question_name}` |",
  "| 나중 | `GET, POST /v1/later` |",
  "| 없음 | 없음. 정하지 않았어요 |",
  "",
].join("\n");

export const PLANNED = JSON.stringify({
  version: 1,
  routes: ["GET", "POST"].map((method) => ({
    method,
    path: "/v1/later",
    feature: "나중",
    adr: "ADR-0001",
    phase: "alpha-2",
    status: "planned",
  })),
});

/** A repository holding the healthy contract, its table, registry and one ADR. */
export function apiRepo(extra: Record<string, string> = {}): string {
  return miniRepo({
    "harness/config/api-contract.yaml": JSON.stringify(API_CONFIG),
    [API_CONFIG.openapi]: `${JSON.stringify(healthyContract(), null, 2)}\n`,
    [API_CONFIG.feature_routes]: FEATURE_TABLE,
    [API_CONFIG.planned]: PLANNED,
    [API_CONFIG.schema_keywords_source]: [
      "package contract",
      "func SchemaKeywords() []string {",
      '\treturn []string{"$ref", "type", "properties", "required", "items", "enum", "description", "example"}',
      "}",
      "",
    ].join("\n"),
    "docs/design-docs/adr/0001-first.md": "---\ntitle: first\n---\nBody\n",
    ...extra,
  });
}
