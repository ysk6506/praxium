/**
 * Lint of the assembled OpenAPI document (rest-api.md 5-3, ADR-0024). The rules below are what the design asks of
 * the contract; each failing rule says where and how to fix it.
 *
 * API001 not OpenAPI 3.1 or no info                 API002 a path breaks the naming rules
 * API003 operationId missing or repeated            API004 x-required-role, x-safe or x-long-running wrong
 * API005 a response has no example                  API006 the error code list is missing or an example breaks it
 * API007 a list lacks limit, cursor or result_complete   API008 an error response is not a problem+json
 * API009 a schema keyword is outside the subset     API010 a reference does not resolve
 * API011 a name is not snake_case                   API012 a description is missing
 * API013 a write lacks Idempotency-Key or If-Match  API014 a KG resource is outside /v1/kgs/{kg}/
 * API015 an action is not written after a colon     API016 security does not match x-required-role
 * API017 x-safe does not match the method           API018 a path is outside /v1
 */
import type { Violation } from "../common";
import type { ApiConfig } from "./api-config";
import {
  asArray,
  asObject,
  asString,
  isObject,
  type Json,
  type JsonObject,
  type OperationRef,
  operationsOf,
  resolveRef,
} from "./api-json";
import { add, type LintContext } from "./api-lint-context";
import { lintOperations } from "./api-lint-operations";

const SNAKE = /^[a-z][a-z0-9]*(_[a-z0-9]+)*$/;

export function lintDocument(doc: JsonObject, config: ApiConfig, file: string): Violation[] {
  const ctx: LintContext = { doc, config, file, out: [] };
  lintHeader(ctx);
  lintSchemas(ctx);
  lintPaths(ctx);
  lintOperations(ctx, operationsOf(doc));
  return ctx.out;
}

function lintHeader(ctx: LintContext): void {
  const info = asObject(ctx.doc.info);
  if (!asString(ctx.doc.openapi).startsWith("3.1"))
    add(
      ctx,
      "API001",
      "openapi",
      "the document is not OpenAPI 3.1",
      "Declare openapi 3.1.0 in a part.",
    );
  if (!asString(info.title) || !asString(info.version))
    add(ctx, "API001", "info", "title and version are required", "Fill info in the root part.");
}

function lintPaths(ctx: LintContext): void {
  for (const path of Object.keys(asObject(ctx.doc.paths))) {
    if (!path.startsWith("/v1/") && path !== "/v1") {
      add(
        ctx,
        "API018",
        path,
        "the path is outside /v1",
        "Every path starts with the major version, /v1.",
      );
      continue;
    }
    for (const segment of path.split("/").slice(2)) lintSegment(ctx, path, segment);
    lintKgScope(ctx, path);
  }
}

function lintSegment(ctx: LintContext, path: string, segment: string): void {
  const [name = "", ...actions] = segment.split(":");
  if (name.startsWith("{")) {
    if (!/^\{[a-z][a-z0-9_]*\}$/.test(name))
      add(
        ctx,
        "API002",
        path,
        `the parameter ${name} is not snake_case`,
        "Write {kg_version_id} style names.",
      );
  } else lintName(ctx, path, name);
  for (const action of actions)
    if (!/^[a-z]+(-[a-z]+)*$/.test(action))
      add(
        ctx,
        "API015",
        path,
        `the action :${action} is not lower-case with hyphens`,
        "Write :request-review style actions.",
      );
}

function lintName(ctx: LintContext, path: string, name: string): void {
  if (!/^[a-z][a-z0-9]*(-[a-z0-9]+)*$/.test(name))
    add(
      ctx,
      "API002",
      path,
      `the segment ${name} is not lower-case with hyphens`,
      "Use object-types style names.",
    );
  else if (!name.endsWith("s") && !ctx.config.singular_segments.includes(name))
    add(
      ctx,
      "API002",
      path,
      `the segment ${name} is not plural`,
      "Name resources in the plural (rest-api.md 3-1), or list the word in singular_segments with an ADR.",
    );
  if (ctx.config.action_verbs.includes(name))
    add(
      ctx,
      "API015",
      path,
      `the verb ${name} is a path segment`,
      "Write an action after a colon: /v1/proposals/{proposal_id}:approve.",
    );
}

function lintKgScope(ctx: LintContext, path: string): void {
  const segments = path.split("/").slice(2);
  const inKg = segments[0] === "kgs" && segments[1] === "{kg}";
  segments.forEach((segment, index) => {
    const name = segment.split(":")[0] ?? "";
    if (!ctx.config.kg_resources.includes(name)) return;
    if (!(inKg && index >= 2))
      add(
        ctx,
        "API014",
        path,
        `${name} belongs to a KG`,
        "Put it under /v1/kgs/{kg}/... (rest-api.md 3-15).",
      );
  });
}

// ---- schemas

function lintSchemas(ctx: LintContext): void {
  walkSchemas(ctx, ctx.doc, "");
}

/** Visit every object of the document that is a schema: under components.schemas, parameters, media types. */
function walkSchemas(ctx: LintContext, node: Json, pointer: string): void {
  if (Array.isArray(node)) {
    for (const [i, item] of node.entries()) walkSchemas(ctx, item, `${pointer}/${i}`);
    return;
  }
  if (!isObject(node)) return;
  for (const [key, value] of Object.entries(node)) {
    const at = `${pointer}/${key.replaceAll("~", "~0").replaceAll("/", "~1")}`;
    if (key === "schema" || (key === "schemas" && pointer === "/components")) {
      const schemas =
        key === "schema"
          ? [[at, value] as const]
          : Object.entries(asObject(value)).map(([n, s]) => [`${at}/${n}`, s] as const);
      for (const [where, schema] of schemas) lintSchema(ctx, schema, where);
    }
    if (key !== "example" && key !== "x-catalogue") walkSchemas(ctx, value, at);
  }
}

function lintSchema(ctx: LintContext, schema: Json, where: string): void {
  if (!isObject(schema)) return;
  for (const key of Object.keys(schema)) {
    if (!key.startsWith("x-") && !ctx.config.schema_keywords.includes(key))
      add(
        ctx,
        "API009",
        where,
        `the keyword ${key} is outside the schema subset`,
        "Use only the keywords of schema_keywords in harness/config/api-contract.yaml; the validator cannot check anything else.",
      );
  }
  if (typeof schema.$ref === "string" && resolveRef(ctx.doc, schema.$ref) === undefined)
    add(
      ctx,
      "API010",
      where,
      `${schema.$ref} does not resolve`,
      "Point the reference at a schema of components.",
    );
  lintProperties(ctx, schema, where);
  for (const child of [
    schema.items,
    ...Object.values(asObject(schema.properties)),
    schema.additionalProperties,
  ])
    lintSchema(ctx, child, `${where}/…`);
}

function lintProperties(ctx: LintContext, schema: JsonObject, where: string): void {
  for (const [name, property] of Object.entries(asObject(schema.properties))) {
    if (!SNAKE.test(name))
      add(
        ctx,
        "API011",
        `${where}/properties/${name}`,
        "the property name is not snake_case",
        "Rename it in snake_case (rest-api.md 3-1).",
      );
    const prop = asObject(property);
    if (typeof prop.$ref !== "string" && !asString(prop.description))
      add(
        ctx,
        "API012",
        `${where}/properties/${name}`,
        "the property has no description",
        "Say in one sentence what the field holds.",
      );
  }
  for (const required of asArray(schema.required))
    if (typeof required === "string" && !(required in asObject(schema.properties)))
      add(
        ctx,
        "API012",
        where,
        `required names ${required}, which is not a property`,
        "Remove it from required or add the property.",
      );
}

export type { OperationRef };
