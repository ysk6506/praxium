/** Operation-level rules of the contract lint (see api-lint.ts for the list of codes). */
import {
  asArray,
  asObject,
  asString,
  deref,
  isObject,
  type Json,
  type JsonObject,
  type OperationRef,
} from "./api-json";
import { add, type LintContext } from "./api-lint-context";

export function lintOperations(ctx: LintContext, operations: readonly OperationRef[]): void {
  const seen = new Map<string, string>();
  for (const op of operations) {
    const where = `${op.method} ${op.path}`;
    lintIdentity(ctx, op, where, seen);
    lintExtensions(ctx, op, where);
    lintSecurity(ctx, op, where);
    lintWrites(ctx, op, where);
    lintResponses(ctx, op, where);
    lintList(ctx, op, where);
    lintParameters(ctx, op, where);
  }
  lintComponentResponses(ctx);
  lintCatalogue(ctx);
}

function lintIdentity(
  ctx: LintContext,
  op: OperationRef,
  where: string,
  seen: Map<string, string>,
) {
  const id = asString(op.operation.operationId);
  if (!id)
    add(
      ctx,
      "API003",
      where,
      "no operationId",
      "Give the operation a unique camelCase operationId.",
    );
  else if (seen.has(id))
    add(
      ctx,
      "API003",
      where,
      `operationId ${id} is also used by ${seen.get(id)}`,
      "Make operationIds unique.",
    );
  else seen.set(id, where);
  if (!asString(op.operation.summary) || !asString(op.operation.description))
    add(
      ctx,
      "API012",
      where,
      "summary and description are required",
      "Say what the operation does and who it is for.",
    );
}

function rolesOf(op: OperationRef): string[] {
  return asArray(op.operation["x-required-role"]).map(asString);
}

function lintExtensions(ctx: LintContext, op: OperationRef, where: string): void {
  const roles = rolesOf(op);
  if (roles.length === 0 || roles.some((r) => !ctx.config.roles.includes(r)))
    add(
      ctx,
      "API004",
      where,
      `x-required-role is missing or names a role outside ${ctx.config.roles.join(", ")}`,
      "List who may call: anonymous, authenticated, read or role names (any one is enough).",
    );
  const safe = op.operation["x-safe"];
  if (typeof safe !== "boolean")
    add(
      ctx,
      "API004",
      where,
      "x-safe is missing",
      "Set x-safe true when the call changes no state, false otherwise.",
    );
  if (typeof op.operation["x-long-running"] !== "boolean")
    add(
      ctx,
      "API004",
      where,
      "x-long-running is missing",
      "Set x-long-running true for calls that answer 202 and a job.",
    );
  const readOnly = op.method === "GET" || op.method === "HEAD";
  if (
    (readOnly && safe !== true) ||
    (["PUT", "PATCH", "DELETE"].includes(op.method) && safe === true)
  )
    add(
      ctx,
      "API017",
      where,
      `x-safe ${String(safe)} does not fit ${op.method}`,
      "GET and HEAD are safe; PUT, PATCH and DELETE never are; a safe POST is a closed question.",
    );
}

function lintSecurity(ctx: LintContext, op: OperationRef, where: string): void {
  const anonymous = rolesOf(op).includes("anonymous");
  const security = op.operation.security;
  const open = Array.isArray(security) && security.length === 0;
  if (anonymous && !open)
    add(
      ctx,
      "API016",
      where,
      "an anonymous operation must say security: []",
      "Add security: [] so tools do not send a token.",
    );
  if (!anonymous && open)
    add(
      ctx,
      "API016",
      where,
      "security: [] on an operation that needs a login",
      "Remove it: the document's default security is the bearer token.",
    );
  const schemes = asObject(asObject(ctx.doc.components).securitySchemes);
  if (!anonymous && Object.keys(schemes).length === 0)
    add(
      ctx,
      "API016",
      where,
      "the document declares no security scheme",
      "Declare a bearer scheme under components.securitySchemes.",
    );
}

function parameterNames(ctx: LintContext, op: OperationRef): string[] {
  return asArray(op.operation.parameters).map((p) => asString(asObject(deref(ctx.doc, p)).name));
}

function lintWrites(ctx: LintContext, op: OperationRef, where: string): void {
  const names = parameterNames(ctx, op).map((n) => n.toLowerCase());
  const safe = op.operation["x-safe"] === true;
  if (op.method === "POST" && !safe && !names.includes("idempotency-key"))
    add(
      ctx,
      "API013",
      where,
      "a creating or acting POST needs the Idempotency-Key header",
      "Add the IdempotencyKey parameter (rest-api.md 3-6).",
    );
  if (op.method === "POST" && safe && names.includes("idempotency-key"))
    add(
      ctx,
      "API013",
      where,
      "a safe POST must not take Idempotency-Key",
      "Remove the header: a closed question can be sent again.",
    );
  if (["PATCH", "PUT", "DELETE"].includes(op.method) && !names.includes("if-match"))
    add(
      ctx,
      "API013",
      where,
      "a change needs the If-Match header",
      "Add the IfMatch parameter (rest-api.md 3-6).",
    );
}

function mediaOf(ctx: LintContext, response: Json): JsonObject {
  return asObject(asObject(deref(ctx.doc, response)).content);
}

function lintResponses(ctx: LintContext, op: OperationRef, where: string): void {
  const responses = asObject(op.operation.responses);
  for (const [status, response] of Object.entries(responses)) {
    if (deref(ctx.doc, response) === undefined)
      add(
        ctx,
        "API010",
        `${where} ${status}`,
        "the response reference does not resolve",
        "Point it at components.responses.",
      );
    const content = mediaOf(ctx, response);
    checkExamples(ctx, content, `${where} ${status}`);
    if (/^[45]/.test(status) || status === "default")
      checkProblem(ctx, content, `${where} ${status}`);
  }
  const roles = rolesOf(op);
  if (!roles.includes("anonymous") && !("401" in responses))
    add(ctx, "API008", where, "no 401 response", "Declare the Unauthorized response.");
  if (!("406" in responses))
    add(ctx, "API008", where, "no 406 response", "Declare the NotAcceptable response.");
}

function checkExamples(ctx: LintContext, content: JsonObject, where: string): void {
  for (const [media, entry] of Object.entries(content)) {
    const body = asObject(entry);
    if (body.example === undefined && body.examples === undefined)
      add(
        ctx,
        "API005",
        where,
        `the ${media} body has no example`,
        "Add an example that fits the schema; every response has one, errors too (rest-api.md 5-3).",
      );
    else lintExampleCode(ctx, body.example, where);
  }
}

function lintExampleCode(ctx: LintContext, example: Json, where: string): void {
  const code = asObject(example).code;
  if (typeof code !== "string") return;
  const enumValues = asArray(
    asObject(asObject(asObject(ctx.doc.components).schemas).ProblemCode).enum,
  );
  if (!enumValues.includes(code))
    add(
      ctx,
      "API006",
      where,
      `the example uses the code ${code}, which is not in the ProblemCode list`,
      "Use a code of the closed catalogue, or add the code to the catalogue first.",
    );
}

function checkProblem(ctx: LintContext, content: JsonObject, where: string): void {
  const problem = asObject(content["application/problem+json"]);
  const schema = asObject(problem.schema);
  if (Object.keys(content).length !== 1 || schema.$ref !== "#/components/schemas/Problem")
    add(
      ctx,
      "API008",
      where,
      "an error response must be application/problem+json with the Problem schema",
      "Use the shared components.responses entries.",
    );
}

function dataIsList(ctx: LintContext, op: OperationRef): boolean {
  const ok = asObject(asObject(op.operation.responses)["200"]);
  const schema = asObject(
    deref(ctx.doc, asObject(asObject(mediaOf(ctx, ok)["application/json"]).schema)),
  );
  const data = asObject(deref(ctx.doc, asObject(schema.properties).data));
  return asObject(data).type === "array";
}

function lintList(ctx: LintContext, op: OperationRef, where: string): void {
  if (op.method !== "GET" || !dataIsList(ctx, op)) return;
  const names = parameterNames(ctx, op);
  for (const need of ["limit", "cursor"])
    if (!names.includes(need))
      add(
        ctx,
        "API007",
        where,
        `a list needs the ${need} parameter`,
        "Add the Limit and Cursor parameters (rest-api.md 3-3).",
      );
  const ok = asObject(asObject(op.operation.responses)["200"]);
  const schema = asObject(
    deref(ctx.doc, asObject(asObject(mediaOf(ctx, ok)["application/json"]).schema)),
  );
  const meta = asObject(deref(ctx.doc, asObject(schema.properties).meta));
  if (!("result_complete" in asObject(meta.properties)))
    add(
      ctx,
      "API007",
      where,
      "the list meta has no result_complete",
      "Put result_complete (total, returned, truncated, reason, next_cursor) in the meta schema.",
    );
}

function lintParameters(ctx: LintContext, op: OperationRef, where: string): void {
  for (const raw of asArray(op.operation.parameters)) {
    const parameter = deref(ctx.doc, raw);
    if (!isObject(parameter)) {
      add(
        ctx,
        "API010",
        where,
        "a parameter reference does not resolve",
        "Point it at components.parameters.",
      );
      continue;
    }
    const name = asString(parameter.name);
    if (parameter.in !== "header" && !/^[a-z][a-z0-9_]*$/.test(name))
      add(
        ctx,
        "API011",
        where,
        `the parameter ${name} is not snake_case`,
        "Name query and path parameters in snake_case.",
      );
    if (!asString(parameter.description))
      add(
        ctx,
        "API012",
        where,
        `the parameter ${name} has no description`,
        "Describe the parameter.",
      );
  }
}

/** The closed list of error codes must be in the contract and must be consistent (rest-api.md 5-3). */
function lintCatalogue(ctx: LintContext): void {
  const schema = asObject(asObject(asObject(ctx.doc.components).schemas).ProblemCode);
  const codes = asArray(schema.enum).map(asString);
  const catalogue = asObject(schema["x-catalogue"]);
  if (codes.length === 0) {
    add(
      ctx,
      "API006",
      "components/schemas/ProblemCode",
      "the contract has no closed list of error codes",
      "Add the ProblemCode schema with an enum and an x-catalogue of group and status.",
    );
    return;
  }
  for (const code of codes) {
    const entry = asObject(catalogue[code]);
    const [group] = code.split(".");
    if (
      !/^[a-z_]+\.[a-z_]+$/.test(code) ||
      entry.group !== group ||
      typeof entry.status !== "number"
    )
      add(
        ctx,
        "API006",
        `ProblemCode ${code}`,
        "the code is not <group>.<detail> or its x-catalogue entry is wrong",
        "Write each code as group.detail with its group and HTTP status in x-catalogue.",
      );
  }
  for (const code of Object.keys(catalogue))
    if (!codes.includes(code))
      add(
        ctx,
        "API006",
        `ProblemCode ${code}`,
        "x-catalogue names a code that is not in the enum",
        "Keep enum and x-catalogue the same list.",
      );
}

/** The shared responses carry examples too, even when no operation uses them yet. */
function lintComponentResponses(ctx: LintContext): void {
  const responses = asObject(asObject(ctx.doc.components).responses);
  for (const [name, response] of Object.entries(responses))
    checkExamples(ctx, asObject(asObject(response).content), `components/responses/${name}`);
}
