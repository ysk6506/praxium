/** TypeScript types from the schemas of the contract. */
import { compareText, type OpenApiDocument, refName, type Schema } from "./spec";

const IDENTIFIER = /^[A-Za-z_$][A-Za-z0-9_$]*$/;

const PRIMITIVES: Readonly<Record<string, string>> = {
  string: "string",
  integer: "number",
  number: "number",
  boolean: "boolean",
  null: "null",
};

/** A key of an object type: bare when it is an identifier, quoted when not. */
export function propertyKey(name: string): string {
  return IDENTIFIER.test(name) ? name : JSON.stringify(name);
}

/** A comment from a description; descriptions never end a comment early. */
export function docComment(text: string | undefined, indent = ""): string {
  if (text === undefined || text.trim() === "") return "";
  const flat = text.replace(/\s+/g, " ").replace(/\*\//g, "* /").trim();
  return `${indent}/** ${flat} */\n`;
}

function literal(value: unknown): string {
  return JSON.stringify(value) ?? "unknown";
}

function union(parts: readonly string[]): string {
  const unique = [...new Set(parts)];
  return unique.length === 0 ? "never" : unique.join(" | ");
}

function objectType(schema: Schema, refs?: Set<string>): string {
  const props = Object.entries(schema.properties ?? {});
  const extra = schema.additionalProperties;
  if (props.length === 0) {
    if (typeof extra === "object") return `Record<string, ${typeOf(extra, refs)}>`;
    return "Record<string, unknown>";
  }
  const required = new Set(schema.required ?? []);
  const lines = props.map(([name, child]) => {
    const optional = required.has(name) ? "" : "?";
    return `${propertyKey(name)}${optional}: ${typeOf(child, refs)};`;
  });
  return `{ ${lines.join(" ")} }`;
}

function typeByName(schema: Schema, name: string, refs?: Set<string>): string {
  if (name === "object") return objectType(schema, refs);
  if (name === "array") return `Array<${schema.items ? typeOf(schema.items, refs) : "unknown"}>`;
  return PRIMITIVES[name] ?? "unknown";
}

/** The TypeScript type that describes a value of this schema. `refs` collects the names of the schemas it points at. */
export function typeOf(schema: Schema, refs?: Set<string>): string {
  if (schema.$ref !== undefined) {
    refs?.add(refName(schema.$ref));
    return refName(schema.$ref);
  }
  if (schema.const !== undefined) return literal(schema.const);
  if (schema.enum !== undefined) return union(schema.enum.map(literal));
  if (schema.allOf !== undefined)
    return schema.allOf.map((s) => `(${typeOf(s, refs)})`).join(" & ");
  const alternatives = schema.oneOf ?? schema.anyOf;
  if (alternatives !== undefined) return union(alternatives.map((s) => typeOf(s, refs)));
  if (schema.type === undefined) return schema.properties ? objectType(schema, refs) : "unknown";
  const names = typeof schema.type === "string" ? [schema.type] : schema.type;
  return union(names.map((name) => typeByName(schema, name, refs)));
}

function declaration(name: string, schema: Schema, refs: Set<string>): string {
  const doc = docComment(schema.description);
  if (schema.type === "object" && Object.keys(schema.properties ?? {}).length > 0) {
    return `${doc}export interface ${name} ${objectType(schema, refs)}\n`;
  }
  return `${doc}export type ${name} = ${typeOf(schema, refs)};\n`;
}

/** How many schemas one generated file holds: the files stay short enough to read and under the size limit. */
export const SCHEMAS_PER_FILE = 24;

const fileName = (index: number): string => `types-${String(index + 1).padStart(2, "0")}.ts`;
const moduleName = (file: string): string => `./${file.replace(/\.ts$/, "")}`;

/**
 * The type files: `types-01.ts`, `types-02.ts`... hold one declaration per schema of `components.schemas`, sorted by
 * name, and `types.ts` re-exports every name, so callers import from one place.
 */
export function generateTypeFiles(doc: OpenApiDocument): Record<string, string> {
  const schemas = Object.entries(doc.components?.schemas ?? {}).sort(([a], [b]) =>
    compareText(a, b),
  );
  const chunks: [string, Schema][][] = [];
  for (let i = 0; i < schemas.length; i += SCHEMAS_PER_FILE) {
    chunks.push(schemas.slice(i, i + SCHEMAS_PER_FILE));
  }
  const fileOf = new Map<string, string>();
  chunks.forEach((chunk, index) => {
    for (const [name] of chunk) fileOf.set(name, fileName(index));
  });
  const files: Record<string, string> = {};
  const barrel: string[] = [];
  chunks.forEach((chunk, index) => {
    const own = fileName(index);
    const refs = new Set<string>();
    const code = chunk.map(([name, schema]) => declaration(name, schema, refs));
    const imports = new Map<string, string[]>();
    for (const ref of [...refs].sort(compareText)) {
      const file = fileOf.get(ref);
      if (file !== undefined && file !== own)
        imports.set(file, [...(imports.get(file) ?? []), ref]);
    }
    const lines = [...imports].map(
      ([file, names]) => `import type { ${names.join(", ")} } from "${moduleName(file)}";`,
    );
    files[own] = `${lines.join("\n")}${lines.length > 0 ? "\n\n" : ""}${code.join("\n")}`;
    barrel.push(
      `export type { ${chunk.map(([name]) => name).join(", ")} } from "${moduleName(own)}";`,
    );
  });
  files["types.ts"] = `${barrel.join("\n")}\n`;
  return files;
}
