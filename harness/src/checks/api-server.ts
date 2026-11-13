/**
 * The routes the server registers equal the contract's paths (rest-api.md 5-2, ADR-0024).
 *
 * API050 the server registers a route the contract does not have, or the other way round
 * API051 the server's routes cannot be listed                      API052 a route needs other roles than the contract says
 */
import type { Violation } from "../common";
import { violation } from "../common";
import type { ApiConfig } from "./api-config";
import { asArray, asObject, asString, type JsonObject, operationsOf } from "./api-json";

export interface ServedRoute {
  readonly method: string;
  readonly path: string;
  readonly roles: readonly string[];
}

/** Runs the routes command and returns what the server registers, or an error text. */
export type RouteLister = () =>
  | { readonly routes: readonly ServedRoute[] }
  | { readonly error: string };

/** Read the output of `praxium api routes --json`. */
export function readServed(text: string): ServedRoute[] | null {
  try {
    const data = asObject(JSON.parse(text) as unknown);
    if (!Array.isArray(data.routes)) return null;
    return asArray(data.routes).map((entry) => {
      const r = asObject(entry);
      return {
        method: asString(r.method),
        path: asString(r.path),
        roles: asArray(r.roles).map(asString),
      };
    });
  } catch {
    return null;
  }
}

export function checkServer(config: ApiConfig, doc: JsonObject, list: RouteLister): Violation[] {
  const listed = list();
  if ("error" in listed)
    return [
      violation(
        "API051",
        config.go_root,
        0,
        `the server's routes cannot be listed: ${listed.error}`,
        "Make `go run ./cmd/praxium api routes --json` work in backend/.",
      ),
    ];
  const served = new Map(listed.routes.map((r) => [`${r.method} ${r.path}`, r]));
  const out: Violation[] = [];
  const specified = new Map(operationsOf(doc).map((o) => [`${o.method} ${o.path}`, o]));
  for (const [id, op] of specified) {
    const route = served.get(id);
    if (!route) {
      out.push(
        violation(
          "API050",
          config.openapi,
          0,
          `the contract has ${id} and the server does not register it`,
          "Register the route in the server's route table, or take it out of the contract (and plan it in the registry).",
        ),
      );
      continue;
    }
    const wanted = asArray(op.operation["x-required-role"]).map(asString).sort().join(",");
    if (wanted !== [...route.roles].sort().join(","))
      out.push(
        violation(
          "API052",
          config.openapi,
          0,
          `${id} needs [${[...route.roles].join(", ")}] on the server and [${wanted}] in the contract`,
          "Make x-required-role and the server's route table say the same.",
        ),
      );
  }
  for (const id of served.keys())
    if (!specified.has(id))
      out.push(
        violation(
          "API050",
          config.openapi,
          0,
          `the server registers ${id} and the contract does not have it`,
          "Specify the route in a part of the contract first: the contract comes before the server (rest-api.md 5-1).",
        ),
      );
  return out;
}
