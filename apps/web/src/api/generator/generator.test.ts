import { describe, expect, it } from "vitest";
import { generateBodies, type OpenApiDocument, parseFile, renderFile } from "./index";
import { generateOperations, generateOperationTable } from "./operations";
import { operationsOf } from "./spec";
import { generateTypeFiles, propertyKey, typeOf } from "./types";

const fakeSha = (text: string): string => text.length.toString(16).padStart(64, "0");

const doc: OpenApiDocument = {
  paths: {
    "/v1/things/{id}": {
      get: {
        operationId: "getThing",
        summary: "Get a thing",
        parameters: [
          { name: "id", in: "path", required: true, schema: { type: "string" } },
          { $ref: "#/components/parameters/Limit" },
          { name: "filter", in: "query", schema: { type: "object" } },
          { name: "If-None-Match", in: "header", schema: { type: "string" } },
        ],
        responses: {
          "200": {
            content: { "application/json": { schema: { $ref: "#/components/schemas/Thing" } } },
          },
        },
        "x-safe": true,
        "x-required-role": ["read"],
      },
    },
    "/v1/things": {
      post: {
        operationId: "makeThing",
        requestBody: {
          required: true,
          content: { "application/json": { schema: { $ref: "#/components/schemas/ThingInput" } } },
        },
        responses: { "204": {} },
      },
      get: {
        operationId: "listThings",
        responses: { "200": { $ref: "#/components/responses/ThingList" } },
      },
    },
  },
  components: {
    parameters: { Limit: { name: "limit", in: "query", schema: { type: "integer" } } },
    responses: {
      ThingList: {
        content: {
          "application/json": {
            schema: { type: "array", items: { $ref: "#/components/schemas/Thing" } },
          },
        },
      },
    },
    schemas: {
      Thing: {
        type: "object",
        description: "One thing. It ends */ here.",
        required: ["id"],
        properties: {
          id: { type: "string" },
          size: { type: ["integer", "null"] },
          kind: { enum: ["a", "b"] },
          tags: { type: "array", items: { type: "string" } },
          extra: { type: "object", additionalProperties: { type: "number" } },
          "odd-name": { type: "boolean" },
        },
      },
      ThingInput: {
        allOf: [
          { $ref: "#/components/schemas/Thing" },
          { type: "object", properties: { note: { type: "string" } } },
        ],
      },
      Either: { oneOf: [{ type: "string" }, { $ref: "#/components/schemas/Thing" }] },
      Free: { type: "object" },
    },
  },
};

describe("[REQ-276] the generator turns schemas into TypeScript types", () => {
  it("writes an interface for an object with properties, optional unless required", () => {
    const text = generateTypeFiles(doc)["types-01.ts"] ?? "";
    expect(text).toContain("export interface Thing {");
    expect(text).toContain("id: string;");
    expect(text).toContain("size?: number | null;");
    expect(text).toContain('kind?: "a" | "b";');
    expect(text).toContain("tags?: Array<string>;");
    expect(text).toContain("extra?: Record<string, number>;");
    expect(text).toContain('"odd-name"?: boolean;');
  });

  it("keeps a description from ending its own comment", () => {
    const text = generateTypeFiles(doc)["types-01.ts"] ?? "";
    expect(text).toContain("/** One thing. It ends * / here. */");
  });

  it("writes aliases for compositions and free-form objects", () => {
    const text = generateTypeFiles(doc)["types-01.ts"] ?? "";
    expect(text).toContain("export type ThingInput = (Thing) & ({ note?: string; });");
    expect(text).toContain("export type Either = string | Thing;");
    expect(text).toContain("export type Free = Record<string, unknown>;");
  });

  it("quotes names that are not identifiers and leaves the rest bare", () => {
    expect(propertyKey("name")).toBe("name");
    expect(propertyKey("odd-name")).toBe('"odd-name"');
  });

  it("types what it cannot read as unknown, and a const as its literal", () => {
    expect(typeOf({})).toBe("unknown");
    expect(typeOf({ type: "mystery" })).toBe("unknown");
    expect(typeOf({ const: "x" })).toBe('"x"');
    expect(typeOf({ type: "array" })).toBe("Array<unknown>");
    expect(typeOf({ properties: { a: { type: "string" } } })).toBe("{ a?: string; }");
    expect(typeOf({ enum: [] })).toBe("never");
  });

  it("splits many schemas over files, imports across them and re-exports every name from types.ts", () => {
    const schemas: Record<string, { type: string; properties?: Record<string, { $ref: string }> }> =
      {};
    for (let i = 0; i < 30; i++) {
      const name = `S${String(i).padStart(2, "0")}`;
      schemas[name] = {
        type: "object",
        properties: {
          next: { $ref: `#/components/schemas/S${String((i + 1) % 30).padStart(2, "0")}` },
        },
      };
    }
    const files = generateTypeFiles({ components: { schemas } });
    expect(Object.keys(files).sort()).toEqual(["types-01.ts", "types-02.ts", "types.ts"]);
    expect(files["types-01.ts"]).toContain('import type { S24 } from "./types-02";');
    expect(files["types-02.ts"]).toContain('import type { S00 } from "./types-01";');
    expect(files["types.ts"]).toContain('} from "./types-02";');
    expect(files["types.ts"]).toContain("S29");
  });
});

describe("[REQ-276] the generator turns operations into functions", () => {
  it("lists the operations sorted by id, with their method and path", () => {
    expect(
      operationsOf(doc).map((e) => `${e.method} ${e.path} ${e.operation.operationId}`),
    ).toEqual([
      "GET /v1/things/{id} getThing",
      "GET /v1/things listThings",
      "POST /v1/things makeThing",
    ]);
  });

  it("writes path, query and body into the arguments and keeps header parameters out", () => {
    const text = generateOperations(doc);
    expect(text).toContain("export interface GetThingArgs {");
    expect(text).toContain("path: { id: string; };");
    expect(text).toContain("query?: { limit?: number; filter?: QueryObject; };");
    expect(text).not.toContain("If-None-Match");
    expect(text).toContain("export interface MakeThingArgs { body: ThingInput; }");
    expect(text).toContain(
      "export const getThing = (client: Requester, args: GetThingArgs): Promise<Thing> =>",
    );
    expect(text).toContain(
      'client.request<Thing>({ method: "GET", path: "/v1/things/{id}", pathParams: args.path, query: args.query });',
    );
  });

  it("imports only the schemas it uses and follows a response reference", () => {
    const text = generateOperations(doc);
    expect(text).toContain('import type { Thing, ThingInput } from "./types";');
    expect(text).toContain(
      "export const listThings = (client: Requester): Promise<Array<Thing>> =>",
    );
    expect(text).toContain(
      "export const makeThing = (client: Requester, args: MakeThingArgs): Promise<void> =>",
    );
  });

  it("makes arguments optional when nothing in them is required", () => {
    const optional: OpenApiDocument = {
      paths: {
        "/v1/x": {
          get: {
            operationId: "listX",
            parameters: [{ name: "limit", in: "query", schema: { type: "integer" } }],
            responses: {},
          },
        },
      },
    };
    expect(generateOperations(optional)).toContain("args: ListXArgs = {}");
    expect(generateOperations(optional)).not.toContain("import type {  }");
  });

  it("answers void for an operation without a JSON answer", () => {
    expect(generateOperations(doc)).toContain("): Promise<void> =>");
  });

  it("writes a table with the method, path, safety and roles of every operation", () => {
    const text = generateOperationTable(doc);
    expect(text).toContain(
      'getThing: { method: "GET", path: "/v1/things/{id}", safe: true, roles: ["read"] },',
    );
    expect(text).toContain(
      'makeThing: { method: "POST", path: "/v1/things", safe: false, roles: [] },',
    );
  });

  it("gives the same text for the same document", () => {
    expect(generateBodies(doc)).toEqual(generateBodies(doc));
  });
});

describe("[REQ-276] a generated file says which contract it came from", () => {
  it("reads back the hashes and the body it was written with", () => {
    const text = renderFile("export type A = string;\n", "a".repeat(64), fakeSha);
    const parsed = parseFile(text);
    expect(parsed?.contractSha256).toBe("a".repeat(64));
    expect(parsed?.bodySha256).toBe(fakeSha("export type A = string;\n"));
    expect(parsed?.body).toBe("export type A = string;\n");
  });

  it("does not read a file that has no header", () => {
    expect(parseFile("export type A = string;\n")).toBeUndefined();
    expect(
      parseFile("// a\n// b\n// contract-sha256: xyz\n// body-sha256: xyz\n\nbody"),
    ).toBeUndefined();
  });
});
