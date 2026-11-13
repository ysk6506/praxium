/**
 * The Go checks of ADR-0019 as the parts run-all.ts adds to the checks that already exist (the list of check names
 * is fixed by common.test.ts): architecture, debt, ignores and test integrity. Each returns nothing when the
 * repository has no `go:` section in its architecture contract.
 */
import type { Violation } from "../common";
import { loadDebtConfig, loadRegister } from "./debt";
import * as arch from "./go-arch";
import { loadGoConfig } from "./go-config";
import * as cover from "./go-cover";
import * as debt from "./go-debt";
import * as deps from "./go-deps";
import * as ignores from "./go-ignores";
import * as tools from "./go-tools";
import { loadIgnoresConfig, sealedAdrs } from "./ignores";

/** Imports, units, modules, formatting and vet: GOARCH*, GODEP*, GOTOOL*. */
export function architecture(root: string): Violation[] {
  const config = loadGoConfig(root);
  if (!config) return [];
  return [...arch.run(root, config), ...deps.run(root, config), ...tools.run(root, config)];
}

/** Size, complexity and task markers of Go code: GODEBT*. */
export function debtOf(root: string): Violation[] {
  const config = loadGoConfig(root);
  if (!config) return [];
  const debtConfig = loadDebtConfig(root);
  return debt.run(root, config, debtConfig, loadRegister(root, debtConfig));
}

/** Directives and skipped tests of Go code: GOIGN*. */
export function ignoresOf(root: string): Violation[] {
  const config = loadGoConfig(root);
  if (!config) return [];
  const ignoresConfig = loadIgnoresConfig(root);
  const items = loadRegister(root, loadDebtConfig(root));
  const status = new Map(items.map((i) => [String(i.id), String(i.status)]));
  return ignores.check(
    ignores.collect(root, config),
    status,
    sealedAdrs(root, ignoresConfig),
    ignoresConfig.min_reason_chars,
  );
}

/** A unit with code and no test: GOTEST002. (Running the tests, GOTEST001, is part of the coverage check.) */
export function testIntegrity(root: string): Violation[] {
  const config = loadGoConfig(root);
  return config ? cover.checkPresence(root, config) : [];
}
