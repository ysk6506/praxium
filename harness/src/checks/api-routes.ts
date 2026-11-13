/**
 * The table of feature routes, the contract and the planned-route registry agree (rest-api.md 5-3, ADR-0024).
 *
 * API040 a route of the table is in neither the contract nor the registry   API041 a contract operation is not in the table
 * API042 a registry route is not in the table                                API043 a route is both in the contract and the registry
 * API044 a registry entry is malformed or repeated                           API045 a span of the table is not an absolute route
 */
import { existsSync, readdirSync, readFileSync } from "node:fs";
import path from "node:path";
import { type Violation, violation } from "../common";
import { type ApiConfig, type RouteKey, routeId } from "./api-config";
import { asArray, asObject, asString, type JsonObject, operationsOf } from "./api-json";

const METHOD = "(?:GET|POST|PUT|PATCH|DELETE)";
const ABSOLUTE = new RegExp(`^(${METHOD}(?:, ${METHOD})*) (/v1[^\\s?]*)(?:\\?\\S*)?$`);
const LOOSE = /^(?:\.\.\.|:[a-z]|\/v1|(?:GET|POST|PUT|PATCH|DELETE)\b)/;

export interface TableRoute extends RouteKey {
  readonly line: number;
}

export interface TableRead {
  readonly routes: TableRoute[];
  readonly problems: Array<{ line: number; span: string }>;
}

/** Read the routes of the second column of every table: spans that are `METHOD[, METHOD] /v1/path`. */
export function readTable(markdown: string): TableRead {
  const routes: TableRoute[] = [];
  const problems: Array<{ line: number; span: string }> = [];
  markdown.split("\n").forEach((text, index) => {
    const cells = /^\| (.*?) \| (.*) \|$/.exec(text);
    if (!cells || text.startsWith("| 기능") || text.startsWith("|---")) return;
    for (const match of (cells[2] ?? "").matchAll(/`([^`]*)`/g)) {
      const span = match[1] ?? "";
      const absolute = ABSOLUTE.exec(span);
      if (absolute) {
        for (const method of (absolute[1] ?? "").split(", "))
          routes.push({ method, path: absolute[2] ?? "", line: index + 1 });
      } else if (LOOSE.test(span)) {
        problems.push({ line: index + 1, span });
      }
    }
  });
  return { routes, problems };
}

export interface Planned extends RouteKey {
  readonly feature: string;
  readonly adr: string;
  readonly phase: string;
  readonly status: string;
}

export function readPlanned(text: string): Planned[] | null {
  try {
    const data = asObject(JSON.parse(text) as unknown);
    return asArray(data.routes).map((entry) => {
      const r = asObject(entry);
      return {
        method: asString(r.method),
        path: asString(r.path),
        feature: asString(r.feature),
        adr: asString(r.adr),
        phase: asString(r.phase),
        status: asString(r.status),
      };
    });
  } catch {
    return null;
  }
}

export function checkRoutes(root: string, config: ApiConfig, doc: JsonObject): Violation[] {
  const tablePath = path.join(root, config.feature_routes);
  const plannedPath = path.join(root, config.planned);
  if (!existsSync(tablePath))
    return [
      violation(
        "API040",
        config.feature_routes,
        0,
        "the table of feature routes is missing",
        "Restore the document: it is the basis of the route check.",
      ),
    ];
  const planned = existsSync(plannedPath) ? readPlanned(readFileSync(plannedPath, "utf8")) : null;
  if (planned === null)
    return [
      violation(
        "API044",
        config.planned,
        0,
        "the planned route registry is missing or is not JSON",
        'Write {"version": 1, "routes": [...]} (see the entries already there).',
      ),
    ];
  const table = readTable(readFileSync(tablePath, "utf8"));
  return [
    ...table.problems.map((p) =>
      violation(
        "API045",
        config.feature_routes,
        p.line,
        `the span \`${p.span}\` is not an absolute route`,
        "Write every route as `METHOD /v1/full/path` (several methods: `GET, POST /v1/...`); no `...` shorthand, no bare path, no lone :action.",
      ),
    ),
    ...compare(config, table.routes, planned, operationsOf(doc)),
    ...checkPlanned(root, config, planned),
  ];
}

function compare(
  config: ApiConfig,
  table: readonly TableRoute[],
  planned: readonly Planned[],
  operations: ReturnType<typeof operationsOf>,
): Violation[] {
  const out: Violation[] = [];
  const inTable = new Map(table.map((r) => [routeId(r), r]));
  const specified = new Set(operations.map((o) => `${o.method} ${o.path}`));
  const registered = new Set(planned.map(routeId));
  for (const [id, route] of inTable)
    if (!specified.has(id) && !registered.has(id))
      out.push(
        violation(
          "API040",
          config.feature_routes,
          route.line,
          `${id} is in neither the contract nor the planned registry`,
          "Specify it in a part of the contract, or add it to the planned route registry with its ADR and phase.",
        ),
      );
  for (const id of specified)
    if (!inTable.has(id))
      out.push(
        violation(
          "API041",
          config.feature_routes,
          0,
          `the contract has ${id} and the table does not`,
          "Add the route to the table of feature routes first (rest-api.md 5-1).",
        ),
      );
  for (const id of registered) {
    if (!inTable.has(id))
      out.push(
        violation(
          "API042",
          config.planned,
          0,
          `the registry has ${id} and the table does not`,
          "Add the route to the table, or remove it from the registry.",
        ),
      );
    if (specified.has(id))
      out.push(
        violation(
          "API043",
          config.planned,
          0,
          `${id} is specified in the contract and still planned`,
          "Remove it from the planned registry: it is no longer planned.",
        ),
      );
  }
  return out;
}

function checkPlanned(root: string, config: ApiConfig, planned: readonly Planned[]): Violation[] {
  const out: Violation[] = [];
  const seen = new Set<string>();
  const adrs = adrNumbers(root, config);
  for (const route of planned) {
    const id = routeId(route);
    const bad = (message: string) =>
      out.push(
        violation(
          "API044",
          config.planned,
          0,
          `${id}: ${message}`,
          "Give every entry a method, a /v1 path, a feature, an existing ADR, a known phase and status planned.",
        ),
      );
    if (seen.has(id)) bad("the route is registered twice");
    seen.add(id);
    if (!/^(GET|POST|PUT|PATCH|DELETE)$/.test(route.method) || !route.path.startsWith("/v1/"))
      bad("the method or the path is wrong");
    if (!route.feature) bad("no feature");
    if (!config.phases.includes(route.phase))
      bad(`the phase ${route.phase} is not one of ${config.phases.join(", ")}`);
    if (route.status !== "planned") bad(`the status ${route.status} is not planned`);
    if (!adrs.has(route.adr.replace(/^ADR-/, "")))
      bad(`${route.adr} is not an ADR of the repository`);
  }
  return out;
}

function adrNumbers(root: string, config: ApiConfig): Set<string> {
  const dir = path.join(root, config.adr_dir);
  if (!existsSync(dir)) return new Set();
  return new Set(
    readdirSync(dir)
      .map((name) => /^(\d{4})-/.exec(name)?.[1])
      .filter((n): n is string => n !== undefined),
  );
}
