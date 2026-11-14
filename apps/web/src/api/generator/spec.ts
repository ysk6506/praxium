/** The part of an OpenAPI 3.1 document the generator reads, and the small helpers that walk it. */

export interface Schema {
  readonly $ref?: string;
  readonly type?: string | readonly string[];
  readonly description?: string;
  readonly enum?: readonly unknown[];
  readonly const?: unknown;
  readonly properties?: Readonly<Record<string, Schema>>;
  readonly required?: readonly string[];
  readonly additionalProperties?: boolean | Schema;
  readonly items?: Schema;
  readonly allOf?: readonly Schema[];
  readonly oneOf?: readonly Schema[];
  readonly anyOf?: readonly Schema[];
}

export interface Parameter {
  readonly $ref?: string;
  readonly name?: string;
  readonly in?: string;
  readonly required?: boolean;
  readonly description?: string;
  readonly schema?: Schema;
}

export interface MediaContent {
  readonly [mediaType: string]: { readonly schema?: Schema } | undefined;
}

export interface Operation {
  readonly operationId?: string;
  readonly summary?: string;
  readonly description?: string;
  readonly parameters?: readonly Parameter[];
  readonly requestBody?: { readonly required?: boolean; readonly content?: MediaContent };
  readonly responses?: Readonly<
    Record<string, { readonly $ref?: string; readonly content?: MediaContent } | undefined>
  >;
  readonly "x-safe"?: boolean;
  readonly "x-required-role"?: readonly string[];
}

export type PathItem = Readonly<Record<string, Operation | readonly Parameter[] | undefined>>;

export interface OpenApiDocument {
  readonly paths?: Readonly<Record<string, PathItem>>;
  readonly components?: {
    readonly schemas?: Readonly<Record<string, Schema>>;
    readonly parameters?: Readonly<Record<string, Parameter>>;
    readonly responses?: Readonly<Record<string, { readonly content?: MediaContent } | undefined>>;
  };
}

export const HTTP_METHODS = ["get", "post", "put", "patch", "delete"] as const;

/** The name a `$ref` points at: the last segment of `#/components/schemas/Name`. */
export function refName(ref: string): string {
  return ref.slice(ref.lastIndexOf("/") + 1);
}

/** A parameter, with a `$ref` to `components.parameters` followed. */
export function resolveParameter(doc: OpenApiDocument, parameter: Parameter): Parameter {
  if (parameter.$ref === undefined) return parameter;
  return doc.components?.parameters?.[refName(parameter.$ref)] ?? parameter;
}

export interface OperationEntry {
  readonly method: string;
  readonly path: string;
  readonly operation: Operation;
}

/** Every operation of the document that has an `operationId`, sorted by that id. */
export function operationsOf(doc: OpenApiDocument): OperationEntry[] {
  const out: OperationEntry[] = [];
  for (const [path, item] of Object.entries(doc.paths ?? {})) {
    for (const method of HTTP_METHODS) {
      const operation = item[method];
      if (operation !== undefined && !Array.isArray(operation) && "operationId" in operation) {
        out.push({ method: method.toUpperCase(), path, operation: operation as Operation });
      }
    }
  }
  return out.sort((a, b) =>
    compareText(a.operation.operationId ?? "", b.operation.operationId ?? ""),
  );
}

/** Code-point order, so the output does not depend on the locale of the machine that generates it. */
export function compareText(a: string, b: string): number {
  return a < b ? -1 : a > b ? 1 : 0;
}
