/** Throwaway repositories with a small Go module, for the Go checks' meta-tests (ADR-0019). */
import { miniRepo } from "./mini-repo";

const lines = (...rows: string[]): string => `${rows.join("\n")}\n`;

export const GO_ARCHITECTURE = lines(
  "units: {}",
  "go:",
  "  root: backend",
  "  module: example.test/app",
  "  units:",
  "    core.kernel: {path: backend/internal/kernel, may_import: []}",
  "    core.model: {path: backend/internal/model, may_import: [core.kernel]}",
  "    core.other: {path: backend/internal/other, may_import: [core.kernel]}",
  '    cmd.app: {path: backend/cmd/app, may_import: ["core.*"]}',
  "  composition_root: cmd.app",
  "  third_party: {}",
  "  restricted_stdlib: {os: [cmd.app], net/http: [cmd.app]}",
  "  nondeterminism_allowed: [backend/internal/kernel/entropy.go]",
  "  dependency_table: ARCHITECTURE.md",
);

export const GO_ARCHITECTURE_DOC = lines(
  "# Architecture",
  "",
  "| 구분 | 무엇 | 없을 때 |",
  "|---|---|---|",
  "| 필수 | PostgreSQL | 뜨지 않는다 |",
  "",
  "| 모듈 | 라이선스 | 까닭 | 쓰는 단위 |",
  "|---|---|---|---|",
  "| (아직 없다) | | | |",
);

export const GO_DEBT = lines(
  "register: docs/exec-plans/tech-debt.yaml",
  "baseline: harness/config/debt-baseline.json",
  "marker_dirs: [backend]",
  "quality_dirs: [backend]",
  "markers: [TODO, FIXME, HACK, XXX]",
  "max_open: 12",
  "max_open_high: 2",
  "limits: {max_file_lines: 60, max_function_lines: 12, max_complexity: 4, max_params: 3}",
);

export const GO_DEBT_REGISTER = lines(
  "items:",
  "  - {id: TD-0001, title: open item, severity: low, cost: c, created: '2026-01-01', due: '2099-01-01', status: open}",
  "  - {id: TD-0002, title: closed item, severity: low, cost: c, created: '2026-01-01', due: '2099-01-01', status: done, resolution: r}",
);

const BASE: Record<string, string> = {
  "package.json": "{}\n",
  "harness/config/architecture.yaml": GO_ARCHITECTURE,
  "harness/config/debt.yaml": GO_DEBT,
  "harness/config/ignores.yaml": lines("adr_dir: docs/design-docs/adr", "min_reason_chars: 8"),
  "harness/config/licenses.yaml": lines(
    "allowed: [MIT, Apache-2.0]",
    "allowed_unmodified: [MPL-2.0]",
  ),
  "harness/config/tests.yaml": lines(
    "coverage_floor: harness/config/coverage-floor.json",
    "min_floor: 90",
  ),
  "harness/config/coverage-floor.json": "{}\n",
  "docs/exec-plans/tech-debt.yaml": GO_DEBT_REGISTER,
  "docs/design-docs/adr/0001-sealed.md": lines(
    "---",
    "status: active",
    "decision_hash: abc",
    "---",
    "# sealed",
  ),
  "ARCHITECTURE.md": GO_ARCHITECTURE_DOC,
  "backend/go.mod": lines("module example.test/app", "", "go 1.24"),
  "backend/internal/kernel/doc.go": lines(
    "// Package kernel is the base of the fixture.",
    "package kernel",
  ),
  "backend/internal/kernel/kernel.go": lines(
    "package kernel",
    "",
    "// Answer returns a fixed number.",
    "func Answer() int { return 42 }",
    "",
    "// Pick returns a when ok, else b.",
    "func Pick(ok bool, a, b int) int {",
    "\tif ok {",
    "\t\treturn a",
    "\t}",
    "\treturn b",
    "}",
  ),
  "backend/internal/kernel/kernel_test.go": lines(
    "package kernel",
    "",
    'import "testing"',
    "",
    "func TestKernel(t *testing.T) {",
    "\tif Answer() != 42 || Pick(true, 1, 2) != 1 || Pick(false, 1, 2) != 2 {",
    '\t\tt.Fatal("unexpected")',
    "\t}",
    "}",
  ),
  "backend/internal/model/model.go": lines(
    "// Package model sits on the kernel.",
    "package model",
    "",
    'import "example.test/app/internal/kernel"',
    "",
    "// Double doubles the answer.",
    "func Double() int { return 2 * kernel.Answer() }",
  ),
  "backend/internal/model/model_test.go": lines(
    "package model",
    "",
    'import "testing"',
    "",
    "func TestDouble(t *testing.T) {",
    "\tif Double() != 84 {",
    '\t\tt.Fatal("unexpected")',
    "\t}",
    "}",
  ),
  "backend/internal/other/doc.go": lines(
    "// Package other is a sibling of model.",
    "package other",
  ),
  "backend/cmd/app/main.go": lines(
    "// Command app is the composition root of the fixture.",
    "package main",
    "",
    "func main() {}",
  ),
  "backend/cmd/app/main_test.go": lines(
    "package main",
    "",
    'import "testing"',
    "",
    "func TestRuns(t *testing.T) {",
    '\tif len("ab") != 2 {',
    '\t\tt.Fatal("length")',
    "\t}",
    "}",
  ),
};

/** A small, healthy repository with a Go module; `files` are written over it. */
export function goRepo(files: Record<string, string> = {}): string {
  return miniRepo(files, BASE);
}

export { lines as goLines };
