/** The context every rule of the contract lint shares. */
import { type Violation, violation } from "../common";
import type { ApiConfig } from "./api-config";
import type { JsonObject } from "./api-json";

export interface LintContext {
  readonly doc: JsonObject;
  readonly config: ApiConfig;
  readonly file: string;
  readonly out: Violation[];
}

export function add(ctx: LintContext, code: string, where: string, message: string, fix: string) {
  ctx.out.push(violation(code, ctx.file, 0, `${where}: ${message}`, fix));
}
