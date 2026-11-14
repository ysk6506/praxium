/** One typed function per operation of the contract, and a table of what each operation is. */
import {
  compareText,
  type MediaContent,
  type OpenApiDocument,
  type OperationEntry,
  operationsOf,
  type Parameter,
  refName,
  resolveParameter,
  type Schema,
} from "./spec";
import { docComment, propertyKey, typeOf } from "./types";

interface Field {
  readonly name: string;
  readonly required: boolean;
  readonly type: string;
  readonly description: string | undefined;
}

/** A deepObject parameter (`filter[a][equals]=x`) is free-form to the client; the contract lists what a route accepts. */
const isFreeForm = (schema: Schema | undefined): boolean =>
  schema?.type === "object" && schema.$ref === undefined;

function freeFormOr(schema: Schema | undefined, refs: Set<string>): string {
  if (!schema) return "unknown";
  return isFreeForm(schema) ? "QueryObject" : typeOf(schema, refs);
}

function fieldOf(parameter: Parameter, refs: Set<string>): Field {
  return {
    name: parameter.name ?? "",
    required: parameter.required === true || parameter.in === "path",
    type: freeFormOr(parameter.schema, refs),
    description: parameter.description,
  };
}

function group(fields: readonly Field[]): string {
  const lines = fields.map((f) => `${propertyKey(f.name)}${f.required ? "" : "?"}: ${f.type};`);
  return `{ ${lines.join(" ")} }`;
}

function jsonSchema(content: MediaContent | undefined): Schema | undefined {
  return content?.["application/json"]?.schema;
}

/** The schema of the body of the first 2xx answer that carries JSON, if any. */
function responseSchema(entry: OperationEntry, doc: OpenApiDocument): Schema | undefined {
  const responses = entry.operation.responses ?? {};
  for (const status of Object.keys(responses).sort(compareText)) {
    if (!status.startsWith("2")) continue;
    const response = responses[status];
    const target =
      response?.$ref !== undefined ? doc.components?.responses?.[refName(response.$ref)] : response;
    const schema = jsonSchema(target?.content);
    if (schema) return schema;
  }
  return undefined;
}

interface Shape {
  readonly path: readonly Field[];
  readonly query: readonly Field[];
  readonly body: string | undefined;
  readonly bodyRequired: boolean;
}

function shapeOf(entry: OperationEntry, doc: OpenApiDocument, refs: Set<string>): Shape {
  const parameters = (entry.operation.parameters ?? []).map((p) => resolveParameter(doc, p));
  const request = entry.operation.requestBody;
  const body = jsonSchema(request?.content);
  return {
    path: parameters.filter((p) => p.in === "path").map((p) => fieldOf(p, refs)),
    query: parameters.filter((p) => p.in === "query").map((p) => fieldOf(p, refs)),
    body: body ? typeOf(body, refs) : undefined,
    bodyRequired: request?.required === true,
  };
}

function argsFields(shape: Shape): string[] {
  const fields: string[] = [];
  if (shape.path.length > 0) fields.push(`path: ${group(shape.path)};`);
  if (shape.query.length > 0) {
    fields.push(`query${shape.query.some((q) => q.required) ? "" : "?"}: ${group(shape.query)};`);
  }
  if (shape.body) fields.push(`body${shape.bodyRequired ? "" : "?"}: ${shape.body};`);
  return fields;
}

function pascal(name: string): string {
  return name.charAt(0).toUpperCase() + name.slice(1);
}

function requestSpec(entry: OperationEntry, shape: Shape): string {
  const spec = [`method: ${JSON.stringify(entry.method)}`, `path: ${JSON.stringify(entry.path)}`];
  if (shape.path.length > 0) spec.push("pathParams: args.path");
  if (shape.query.length > 0) spec.push("query: args.query");
  if (shape.body) spec.push("body: args.body");
  return spec.join(", ");
}

function operationCode(entry: OperationEntry, doc: OpenApiDocument, refs: Set<string>): string {
  const id = entry.operation.operationId ?? "";
  const shape = shapeOf(entry, doc, refs);
  const fields = argsFields(shape);
  const schema = responseSchema(entry, doc);
  const response = schema ? typeOf(schema, refs) : "void";
  const argsName = `${pascal(id)}Args`;
  const mustPass =
    shape.path.length > 0 || shape.query.some((q) => q.required) || shape.bodyRequired;
  const comment = docComment(entry.operation.summary ?? entry.operation.description);
  const interfaceCode =
    fields.length > 0 ? `export interface ${argsName} { ${fields.join(" ")} }\n\n` : "";
  let parameter = "";
  if (fields.length > 0) parameter = `, args: ${argsName}${mustPass ? "" : " = {}"}`;
  return (
    `${interfaceCode}${comment}export const ${id} = (client: Requester${parameter}): Promise<${response}> =>\n` +
    `  client.request<${response}>({ ${requestSpec(entry, shape)} });\n`
  );
}

function tableEntry(entry: OperationEntry): string {
  const op = entry.operation;
  const roles = JSON.stringify(op["x-required-role"] ?? []);
  return `${op.operationId}: { method: ${JSON.stringify(entry.method)}, path: ${JSON.stringify(entry.path)}, safe: ${op["x-safe"] === true}, roles: ${roles} },`;
}

/** The body of `operations.ts`: one function (and its argument type) per operation. */
export function generateOperations(doc: OpenApiDocument): string {
  const refs = new Set<string>();
  const code = operationsOf(doc).map((entry) => operationCode(entry, doc, refs));
  const imports = [...refs].sort(compareText).join(", ");
  return [
    'import type { QueryObject, Requester } from "../client";',
    imports === "" ? "" : `import type { ${imports} } from "./types";`,
    "",
    code.join("\n"),
  ].join("\n");
}

/** The body of `operation-table.ts`: the method, path, safety and roles of every operation, by operation id. */
export function generateOperationTable(doc: OpenApiDocument): string {
  const entries = operationsOf(doc).map(tableEntry);
  return `export const operationTable = {\n${entries.join("\n")}\n} as const;\n`;
}
