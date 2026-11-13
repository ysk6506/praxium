/** Small helpers over parsed JSON that the API contract checks share (ADR-0024). */

export type Json = unknown;
export type JsonObject = Record<string, Json>;

export const isObject = (value: Json): value is JsonObject =>
  typeof value === "object" && value !== null && !Array.isArray(value);

export const asObject = (value: Json): JsonObject => (isObject(value) ? value : {});

export const asArray = (value: Json): Json[] => (Array.isArray(value) ? value : []);

export const asString = (value: Json): string => (typeof value === "string" ? value : "");

/** The HTTP methods an OpenAPI path item may hold that this contract uses. */
export const METHODS = ["get", "put", "post", "delete", "patch", "head", "options"] as const;

export interface OperationRef {
  readonly method: string;
  readonly path: string;
  readonly operation: JsonObject;
}

/** Every operation of a document, by path and then method. */
export function operationsOf(doc: JsonObject): OperationRef[] {
  const out: OperationRef[] = [];
  const paths = asObject(doc.paths);
  for (const path of Object.keys(paths).sort()) {
    const item = asObject(paths[path]);
    for (const method of METHODS) {
      const operation = item[method];
      if (isObject(operation)) out.push({ method: method.toUpperCase(), path, operation });
    }
  }
  return out;
}

/** Follow a local reference such as #/components/schemas/Problem; undefined when it does not resolve. */
export function resolveRef(doc: JsonObject, ref: string): Json | undefined {
  if (!ref.startsWith("#/")) return undefined;
  let current: Json = doc;
  for (const raw of ref.slice(2).split("/")) {
    const part = raw.replaceAll("~1", "/").replaceAll("~0", "~");
    if (!isObject(current) || !(part in current)) return undefined;
    current = current[part];
  }
  return current;
}

/** The object a value points at when it is a $ref, otherwise the value itself. */
export function deref(doc: JsonObject, value: Json): Json {
  let current = value;
  for (let hops = 0; hops < 20 && isObject(current) && typeof current.$ref === "string"; hops++) {
    const next = resolveRef(doc, current.$ref);
    if (next === undefined) return undefined;
    current = next;
  }
  return current;
}

/** Parse JSON text into an object; null when it is not a JSON object. */
export function parseObject(text: string): JsonObject | null {
  try {
    const value: Json = JSON.parse(text);
    return isObject(value) ? value : null;
  } catch {
    return null;
  }
}
