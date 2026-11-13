/** Running the Go toolchain for the Go checks (ADR-0019). Go is required to work on the backend, so it is used. */
import { spawnSync } from "node:child_process";

export interface GoResult {
  /** The program ran and exited with status 0. */
  readonly ok: boolean;
  /** The program is not installed. */
  readonly missing: boolean;
  readonly stdout: string;
  readonly stderr: string;
}

/**
 * The toolchain runs as installed: no download of another Go version, no workspace file, no module edits. A change
 * to the module's files is the author's decision, never a side effect of a check.
 */
const ENV = { GOTOOLCHAIN: "local", GOWORK: "off", GOFLAGS: "-mod=readonly" };

export function runTool(cwd: string, program: string, args: readonly string[]): GoResult {
  const result = spawnSync(program, [...args], {
    cwd,
    encoding: "utf8",
    env: { ...process.env, ...ENV },
    maxBuffer: 64 * 1024 * 1024,
    timeout: 10 * 60 * 1000,
  });
  const missing = (result.error as NodeJS.ErrnoException | undefined)?.code === "ENOENT";
  return {
    ok: result.status === 0 && !result.error,
    missing,
    stdout: result.stdout ?? "",
    stderr: result.stderr ?? result.error?.message ?? "",
  };
}

/** `gofmt` sits next to `go`; when it is not on the PATH, ask `go env GOROOT` for the folder. */
export function runGofmt(cwd: string, args: readonly string[]): GoResult {
  const direct = runTool(cwd, "gofmt", args);
  if (!direct.missing) return direct;
  const goroot = runTool(cwd, "go", ["env", "GOROOT"]);
  return goroot.ok ? runTool(cwd, `${goroot.stdout.trim()}/bin/gofmt`, args) : direct;
}

/** The last lines of a tool's output, for a message an agent can act on. */
export function tail(text: string, lines = 12): string {
  return text.trim().split("\n").slice(-lines).join("\n");
}
