/**
 * Rules of go-arch.ts that read the contract or the file name rather than the imports (ADR-0028): names the contract
 * points at, import paths written plainly, build constraints on test files.
 */
import { closeSync, existsSync, openSync, readSync } from "node:fs";
import path from "node:path";
import { git, type Violation, violation } from "../common";
import { listFiles } from "../files";
import type { GoConfig, GoFile } from "./go-config";

export const HINT =
  "If the rule itself is wrong, write an ADR and change the `go:` section of harness/config/architecture.yaml; " +
  "otherwise change the code.";

/** What a package path is made of. A backslash, a space or anything else means an escape or a trick. */
export const PLAIN_PATH = /^[A-Za-z0-9._~+/-]+$/;

/** Operating systems and architectures that Go reads from the end of a file name as a build constraint. */
const GOOS = new Set(
  "aix android darwin dragonfly freebsd hurd illumos ios js linux nacl netbsd openbsd plan9 solaris wasip1 windows zos".split(
    " ",
  ),
);
const GOARCH = new Set(
  "386 amd64 amd64p32 arm armbe arm64 arm64be loong64 mips mipsle mips64 mips64le mips64p32 mips64p32le ppc ppc64 ppc64le riscv riscv64 s390 s390x sparc sparc64 wasm".split(
    " ",
  ),
);
const CONSTRAINT_LINE = /^\/\/\s*(go:build\b|\+build\b)/;

/** Units and files that `restricted_stdlib` and `locked_support` name must exist. */
export function checkNamedScopes(
  root: string,
  config: GoConfig,
  names: ReadonlySet<string>,
): Violation[] {
  const out: Violation[] = [];
  const missing = (what: string, fix: string): number =>
    out.push(violation("GOARCH006", "harness/config/architecture.yaml", 0, what, `${fix} ${HINT}`));
  for (const [pkg, scope] of config.restrictedStdlib) {
    for (const entry of scope) {
      if (isFileEntry(entry)) {
        if (!existsSync(path.join(root, entry)))
          missing(
            `restricted_stdlib '${pkg}' names file ${entry}, which does not exist`,
            "Fix the path.",
          );
      } else if (!entry.endsWith(".*") && !names.has(entry)) {
        missing(
          `restricted_stdlib '${pkg}' names '${entry}', which is not a Go unit`,
          "Fix the name.",
        );
      }
    }
  }
  for (const entry of config.lockedSupport) {
    if (!existsSync(path.join(root, entry)))
      missing(
        `locked_support names ${entry}, which does not exist`,
        "Fix the path or remove the entry.",
      );
  }
  return out;
}

/** A restricted_stdlib entry that ends in `.go` is one file, anything else a unit name or pattern. */
export const isFileEntry = (entry: string): boolean => entry.endsWith(".go");

/**
 * A test file that only some builds compile is counted by the lock and the tracer and never runs on the rest. Test
 * files take no build constraint: neither a `//go:build` or `// +build` line before the package clause, nor an
 * operating-system or architecture suffix in the file name (`x_linux_test.go`, `x_arm64_test.go`).
 */
export function constraintProblems(file: GoFile): Violation[] {
  if (!file.isTest) return [];
  const out: Violation[] = [];
  const fix =
    "A test file must run everywhere: remove the constraint, or put the platform-specific part in production code behind an interface.";
  const clause = file.pkg?.line ?? Number.POSITIVE_INFINITY;
  for (const tok of file.toks) {
    if (tok.kind === "comment" && tok.line < clause && CONSTRAINT_LINE.test(tok.text))
      out.push(violation("GOARCH055", file.path, tok.line, "build constraint on a test file", fix));
  }
  const parts = path.posix
    .basename(file.path)
    .replace(/_test\.go$/, "")
    .split("_");
  const last = parts.at(-1) ?? "";
  if (parts.length > 1 && (GOOS.has(last) || GOARCH.has(last)))
    out.push(
      violation(
        "GOARCH055",
        file.path,
        1,
        `the file name ends in '_${last}', which is a build constraint on a test file`,
        fix,
      ),
    );
  return out;
}

/** The first bytes of an ELF, Mach-O, Java class or Windows executable. */
const BINARY_MAGIC = ["7f454c46", "feedface", "feedfacf", "cefaedfe", "cffaedfe", "cafebabe"].map(
  (hex) => Buffer.from(hex, "hex"),
);

function startsLikeBinary(file: string): boolean {
  const head = Buffer.alloc(4);
  let fd: number | undefined;
  try {
    fd = openSync(file, "r");
    readSync(fd, head, 0, 4, 0);
  } catch {
    return false;
  } finally {
    if (fd !== undefined) closeSync(fd);
  }
  return (
    head.subarray(0, 2).toString("latin1") === "MZ" || BINARY_MAGIC.some((m) => m.equals(head))
  );
}

/** The files of the module that a commit would take: tracked ones and new ones that `.gitignore` does not hide. */
function committable(root: string, config: GoConfig): string[] {
  const listed = git(
    root,
    "ls-files",
    "-z",
    "--cached",
    "--others",
    "--exclude-standard",
    "--",
    config.root,
  );
  return listed === null
    ? listFiles(root, [config.root], () => true)
    : listed.split("\0").filter((f) => f !== "");
}

/**
 * GOARCH056: a compiled program or test binary (`go build`, `go test -c`) among the files that a commit would take.
 * It is rebuilt from source, it is large, and a stale one runs old code. `.gitignore` keeps them out.
 */
export function binaryProblems(root: string, config: GoConfig): Violation[] {
  return committable(root, config)
    .filter((f) => existsSync(path.join(root, f)))
    .filter((f) => f.endsWith(".test") || startsLikeBinary(path.join(root, f)))
    .map((f) =>
      violation(
        "GOARCH056",
        f,
        0,
        "a compiled binary is among the files a commit would take",
        "Delete it and add its name to .gitignore (backend/cmd/praxium/praxium, *.test): build outputs are not committed.",
      ),
    );
}
