/**
 * The surface of a contract that a client can depend on, as a flat map of facts (ADR-0024). The breaking-change
 * check compares the surface of the base with the surface of the change.
 */
import {
  asArray,
  asObject,
  asString,
  deref,
  type Json,
  type JsonObject,
  operationsOf,
} from "./api-json";

export type FactKind = "op" | "roles" | "param" | "response" | "field" | "bodyfield" | "code";

export interface Fact {
  readonly kind: FactKind;
  /** The type names of a schema, joined with `|`. */
  readonly type?: string;
  readonly enum?: readonly string[];
  readonly required?: boolean;
  /** roles of an operation, or `group/status` of a code. */
  readonly values?: readonly string[];
}

export type Surface = Map<string, Fact>;

const MAX_DEPTH = 8;

function typeOf(schema: JsonObject): string {
  const t = schema.type;
  if (Array.isArray(t)) return t.map(asString).sort().join("|");
  return asString(t);
}

function enumOf(schema: JsonObject): string[] | undefined {
  return Array.isArray(schema.enum) ? schema.enum.map((v) => JSON.stringify(v)).sort() : undefined;
}

/** Record the schema and everything below it at a pointer. */
interface Walk {
  doc: JsonObject;
  base: string;
  kind: "field" | "bodyfield";
  surface: Surface;
}

function flatten(walk: Walk, raw: Json, pointer: string, required: boolean, depth: number): void {
  const { doc, base, kind, surface } = walk;
  if (depth > MAX_DEPTH) return;
  const schema = asObject(deref(doc, raw));
  const key = `${base} ${pointer || "/"}`;
  surface.set(key, { kind, type: typeOf(schema), enum: enumOf(schema), required });
  const needed = new Set(asArray(schema.required).map(asString));
  for (const [name, child] of Object.entries(asObject(schema.properties)))
    flatten(walk, child, `${pointer}/${name}`, needed.has(name), depth + 1);
  if (schema.items !== undefined) flatten(walk, schema.items, `${pointer}[]`, true, depth + 1);
  const extra = asObject(schema.additionalProperties);
  if (Object.keys(extra).length > 0) flatten(walk, extra, `${pointer}/*`, false, depth + 1);
}

export function surfaceOf(doc: JsonObject): Surface {
  const surface: Surface = new Map();
  for (const op of operationsOf(doc)) {
    const id = `${op.method} ${op.path}`;
    surface.set(id, { kind: "op" });
    surface.set(`${id} roles`, {
      kind: "roles",
      values: asArray(op.operation["x-required-role"]).map(asString).sort(),
    });
    addParameters(doc, op.operation, id, surface);
    addBody(doc, op.operation, id, surface);
    addResponses(doc, op.operation, id, surface);
  }
  addCodes(doc, surface);
  return surface;
}

function addParameters(doc: JsonObject, operation: JsonObject, id: string, surface: Surface) {
  for (const raw of asArray(operation.parameters)) {
    const parameter = asObject(deref(doc, raw));
    const schema = asObject(deref(doc, parameter.schema));
    surface.set(`${id} param ${asString(parameter.in)}:${asString(parameter.name)}`, {
      kind: "param",
      type: typeOf(schema),
      enum: enumOf(schema),
      required: parameter.required === true,
    });
  }
}

function addBody(doc: JsonObject, operation: JsonObject, id: string, surface: Surface) {
  const body = asObject(deref(doc, operation.requestBody));
  for (const [media, entry] of Object.entries(asObject(body.content)))
    flatten(
      { doc, base: `${id} body ${media}`, kind: "bodyfield", surface },
      asObject(entry).schema,
      "",
      body.required === true,
      0,
    );
}

function addResponses(doc: JsonObject, operation: JsonObject, id: string, surface: Surface) {
  for (const [status, raw] of Object.entries(asObject(operation.responses))) {
    surface.set(`${id}#${status}`, { kind: "response" });
    const response = asObject(deref(doc, raw));
    for (const [media, entry] of Object.entries(asObject(response.content)))
      if (media === "application/json")
        flatten(
          { doc, base: `${id}#${status}`, kind: "field", surface },
          asObject(entry).schema,
          "",
          true,
          0,
        );
  }
}

function addCodes(doc: JsonObject, surface: Surface) {
  const schema = asObject(asObject(asObject(doc.components).schemas).ProblemCode);
  const catalogue = asObject(schema["x-catalogue"]);
  for (const code of asArray(schema.enum).map(asString)) {
    const entry = asObject(catalogue[code]);
    surface.set(`code ${code}`, {
      kind: "code",
      values: [asString(entry.group), String(entry.status)],
    });
  }
}
