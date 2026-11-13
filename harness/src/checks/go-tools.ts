/**
 * Formatting and vet for the Go module (ADR-0019), run as part of the architecture check.
 *
 * GOTOOL000 the Go toolchain is not installed   GOTOOL001 a file is not gofmt-formatted
 * GOTOOL002 `go vet` found a problem or the module does not compile
 */
import { existsSync } from "node:fs";
import path from "node:path";
import { type Violation, violation } from "../common";
import type { GoConfig } from "./go-config";
import { runGofmt, runTool, tail } from "./go-run";

const VET_LINE = /^(?:vet: )?(.+?\.go):(\d+):\d+: (.*)$/;

export function run(root: string, config: GoConfig): Violation[] {
  const cwd = path.join(root, config.root);
  if (!existsSync(path.join(cwd, "go.mod"))) return [];
  const fmt = runGofmt(cwd, ["-l", "."]);
  if (fmt.missing) {
    return [
      violation(
        "GOTOOL000",
        config.root,
        0,
        "the Go toolchain (gofmt) is not installed",
        "Install Go (the version in go.mod) and put it on the PATH.",
      ),
    ];
  }
  const out: Violation[] = fmt.stdout
    .split("\n")
    .filter((line) => line.endsWith(".go") && !/(^|\/)(testdata|vendor)\//.test(line))
    .map((file) =>
      violation(
        "GOTOOL001",
        `${config.root}/${file}`,
        1,
        "file is not gofmt-formatted",
        `Run: gofmt -w ${config.root}`,
      ),
    );
  if (!fmt.ok && out.length === 0) {
    out.push(
      violation(
        "GOTOOL001",
        config.root,
        0,
        `gofmt could not read the code: ${tail(fmt.stderr, 3)}`,
        "Fix the syntax error first.",
      ),
    );
  }
  const vet = runTool(cwd, "go", ["vet", "./..."]);
  if (vet.missing) {
    out.push(
      violation(
        "GOTOOL000",
        config.root,
        0,
        "the Go toolchain is not installed",
        "Install Go (the version in go.mod) and put it on the PATH.",
      ),
    );
  } else if (!vet.ok) {
    out.push(...vetProblems(config, vet.stderr));
  }
  return out;
}

function vetProblems(config: GoConfig, output: string): Violation[] {
  const found = output
    .split("\n")
    .map((line) => VET_LINE.exec(line))
    .filter((match): match is RegExpExecArray => match !== null)
    .map((m) =>
      violation(
        "GOTOOL002",
        `${config.root}/${(m[1] as string).replace(/^\.\//, "")}`,
        Number(m[2]),
        `go vet: ${m[3]}`,
        "Fix what vet reports; do not silence it.",
      ),
    );
  return found.length > 0
    ? found
    : [
        violation(
          "GOTOOL002",
          config.root,
          0,
          `go vet failed: ${tail(output, 6)}`,
          "Fix what vet reports; do not silence it.",
        ),
      ];
}
