/** The configuration of the API contract checks (harness/config/api-contract.yaml, ADR-0024). */
import { existsSync } from "node:fs";
import path from "node:path";
import { loadYaml } from "../common";

export const API_CONFIG_PATH = "harness/config/api-contract.yaml";

export interface ApiConfig {
  readonly openapi: string;
  readonly parts: string;
  readonly planned: string;
  readonly feature_routes: string;
  readonly adr_dir: string;
  readonly go_root: string;
  readonly program: string;
  readonly assemble_args: readonly string[];
  readonly routes_args: readonly string[];
  readonly schema_keywords_source: string;
  readonly schema_keywords: readonly string[];
  readonly roles: readonly string[];
  readonly phases: readonly string[];
  readonly singular_segments: readonly string[];
  readonly kg_resources: readonly string[];
  readonly action_verbs: readonly string[];
}

/** The configuration, or null when the repository has no API contract (the checks then do nothing). */
export function loadApiConfig(root: string): ApiConfig | null {
  const file = path.join(root, API_CONFIG_PATH);
  return existsSync(file) ? loadYaml<ApiConfig>(file) : null;
}

/** One route of the contract, of the planned registry or of the server. */
export interface RouteKey {
  readonly method: string;
  readonly path: string;
}

export const routeId = (r: RouteKey): string => `${r.method} ${r.path}`;
