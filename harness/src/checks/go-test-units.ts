/**
 * Go tests as the units the test guard protects (ADR-0023). The lock unit is a top-level `TestXxx`, `FuzzXxx` or
 * `ExampleXxx` function, named by its package folder and function name (`go:backend/internal/model::TestStandardCounts`),
 * so moving a test to another file of the package changes nothing.
 *
 * The fingerprint of a test covers its own tokens, every test-only function, method, type, variable and constant of
 * the package's `_test.go` files that it reaches by name (transitively, so a golden constant or a shared table counts),
 * the `//go:embed` files of those, and the files under `testdata/` that a string in them points at. Layout and comments
 * do not count; the requirement tags `[REQ-NNN]` of a test do. Reaching by name over-approximates: a local variable
 * that shares the name of a package-level one pulls that one in. That can only make the lock stricter, never looser.
 *
 * Besides the digest the unit carries what a weakening would change (like the TypeScript units): the number of calls
 * that can fail the test, a digest of every literal it reaches, and the number of skips.
 *
 * GOTEST003 a Go test function whose code cannot fail the test (nothing that calls `t.Error`, `t.Fatal`, `t.Fail`...)
 */
import { existsSync, readFileSync, statSync } from "node:fs";
import path from "node:path";
import { type Violation, violation } from "../common";
import { listFiles, matchesGlob } from "../files";
import { sha16 } from "../ts-ast";
import { type GoConfig, type GoFile, goFiles, loadGoConfig } from "./go-config";
import { declsOf, type GoDecl, normalizedText } from "./go-decls";
import { closeOf, type GoTok, stringValue } from "./go-lex";
import type { Unit } from "./test-units";

export interface GoTestInfo {
  readonly key: string;
  readonly name: string;
  readonly file: string;
  readonly line: number;
  readonly digest: string;
  /** Calls that can fail the test, counting each use of a helper that fails tests as one. */
  readonly asserts: number;
  /** Digest of every string, number and rune literal of the test and what it reaches. */
  readonly literals: string;
  readonly skips: number;
  /** Requirement ids tagged on the test, sorted. */
  readonly reqs: readonly string[];
}

const TEST_NAME = /^(Test|Fuzz|Example)($|[^a-z])/;
const FAILING = new Set(["Error", "Errorf", "Fatal", "Fatalf", "Fail", "FailNow"]);
const SKIPPING = new Set(["Skip", "Skipf", "SkipNow"]);
const TESTING_TYPES = new Set(["T", "B", "F", "TB"]);
const TAG = /\[(REQ-\d{3})\]/g;
const TESTDATA = /(^|\/)testdata(\/|$)/;
const EMBED = /^\/\/go:embed\s+(.+)$/;
const LIST_TAG = /\[\s*REQ-\d{3}\s*[,;]/;
const OUTPUT = /^\/\/\s*(unordered output|output):/i;

/** A declaration of the package's test files with what it mentions. */
interface Entry {
  readonly decl: GoDecl;
  readonly file: GoFile;
  readonly text: string;
  /** Identifiers that are not a member selector (`x.name`). */
  readonly refs: ReadonlySet<string>;
  /** Identifiers right after a dot: a method may hide behind them. */
  readonly members: ReadonlySet<string>;
  /** Names of the testing handles it declares: `t` in `t *testing.T`. */
  readonly handles: ReadonlySet<string>;
}

const isPunct = (tok: GoTok | undefined, text: string): boolean =>
  tok?.kind === "punct" && tok.text === text;

function handlesOf(toks: readonly GoTok[]): Set<string> {
  const names = new Set<string>();
  toks.forEach((tok, k) => {
    if (tok.kind !== "ident") return;
    const at = isPunct(toks[k + 1], "*") ? k + 2 : k + 1;
    if (
      toks[at]?.text === "testing" &&
      isPunct(toks[at + 1], ".") &&
      TESTING_TYPES.has(toks[at + 2]?.text ?? "")
    )
      names.add(tok.text);
  });
  return names;
}

/** Index of the name token of a function declaration: after `func`, or after the receiver of a method. */
function nameAt(toks: readonly GoTok[]): number {
  return isPunct(toks[1], "(") ? closeOf(toks, 1) + 1 : 1;
}

function entryOf(decl: GoDecl, file: GoFile): Entry {
  const refs = new Set<string>();
  const members = new Set<string>();
  const own = decl.kind === "func" ? nameAt(decl.toks) : -1;
  decl.toks.forEach((tok, k) => {
    if (tok.kind !== "ident" || k === own) return;
    (isPunct(decl.toks[k - 1], ".") ? members : refs).add(tok.text);
  });
  const kind = decl.recv ? `${decl.kind} (${decl.recv})` : decl.kind;
  return {
    decl,
    file,
    text: `${kind} ${normalizedText(decl.toks)}`,
    refs,
    members,
    handles: handlesOf(decl.toks),
  };
}

/** The test-only declarations of one package folder, and how they reach each other. */
class Pool {
  private readonly byName = new Map<string, Entry[]>();
  private readonly byMethod = new Map<string, Entry[]>();
  private readonly byRecv = new Map<string, Entry[]>();
  private readonly always: Entry[] = [];

  constructor(readonly entries: readonly Entry[]) {
    for (const e of entries) {
      const { decl } = e;
      if (decl.recv) {
        push(this.byMethod, decl.names[0] ?? "", e);
        push(this.byRecv, decl.recv, e);
        continue;
      }
      for (const name of decl.names) if (name !== "_") push(this.byName, name, e);
      const hook =
        decl.kind === "func" && (decl.names[0] === "init" || decl.names[0] === "TestMain");
      if (hook || decl.names.includes("_")) this.always.push(e);
    }
  }

  /** Everything `start` reaches by name, `start` itself left out. Package hooks and blank variables are always in. */
  closure(start: Entry): Entry[] {
    const seen = new Set<Entry>([start, ...this.always]);
    const queue = [start, ...this.always];
    const visit = (e: Entry | undefined): void => {
      if (!e || seen.has(e)) return;
      seen.add(e);
      queue.push(e);
      if (e.decl.kind === "type") for (const n of e.decl.names) this.byRecv.get(n)?.forEach(visit);
    };
    for (let e = queue.pop(); e; e = queue.pop()) {
      for (const ref of e.refs) this.byName.get(ref)?.forEach(visit);
      for (const member of e.members) this.byMethod.get(member)?.forEach(visit);
    }
    seen.delete(start);
    return [...seen];
  }
}

function push(map: Map<string, Entry[]>, key: string, entry: Entry): void {
  const list = map.get(key);
  if (list) list.push(entry);
  else map.set(key, [entry]);
}

/** Calls `handle.Method(` for a handle name and a method set. */
function countCalls(
  toks: readonly GoTok[],
  handles: ReadonlySet<string>,
  methods: ReadonlySet<string>,
): number {
  let count = 0;
  toks.forEach((tok, k) => {
    if (
      tok.kind === "ident" &&
      handles.has(tok.text) &&
      isPunct(toks[k + 1], ".") &&
      methods.has(toks[k + 2]?.text ?? "") &&
      isPunct(toks[k + 3], "(")
    )
      count++;
  });
  return count;
}

/** `testing.Short(` gates a test the same way a skip does. */
function shortGates(toks: readonly GoTok[]): number {
  return toks.filter(
    (tok, k) =>
      tok.text === "Short" &&
      isPunct(toks[k - 1], ".") &&
      toks[k - 2]?.text === "testing" &&
      isPunct(toks[k + 1], "("),
  ).length;
}

/** Names of the functions and methods among `entries` that can fail a test, directly or through another one. */
function failingHelpers(entries: readonly Entry[]): Set<string> {
  const helpers = new Set<string>();
  const funcs = entries.filter((e) => e.decl.kind === "func");
  for (let changed = true; changed; ) {
    changed = false;
    for (const e of funcs) {
      const name = e.decl.names[0] as string;
      if (helpers.has(name)) continue;
      const fails =
        countCalls(e.decl.toks, e.handles, FAILING) > 0 ||
        e.decl.toks.some((t) => t.kind === "ident" && helpers.has(t.text));
      if (fails) helpers.add(name);
      changed ||= fails;
    }
  }
  return helpers;
}

const tagsIn = (text: string): string[] => [...text.matchAll(TAG)].map((m) => m[1] as string);

/** Tags in the doc comment above a test and in the string literals of its body (subtest names, table rows). */
function requirementsOf(test: Entry): string[] {
  const found = test.decl.doc.flatMap((c) => tagsIn(c.text));
  for (const tok of test.decl.toks)
    if (tok.kind === "string" || tok.kind === "raw") found.push(...tagsIn(tok.text));
  return [...new Set(found)].sort();
}

/** The files a package's tests can read, found by what their strings and `go:embed` lines name. */
class Files {
  private readonly listed = new Map<string, string[]>();
  private readonly hashes = new Map<string, string>();

  constructor(private readonly root: string) {}

  private under(dir: string): string[] {
    const hit = this.listed.get(dir);
    if (hit) return hit;
    const files = listFiles(this.root, [dir], () => true);
    this.listed.set(dir, files);
    return files;
  }

  private hash(file: string): string {
    const hit = this.hashes.get(file);
    if (hit) return hit;
    const value = sha16(readFileSync(path.join(this.root, file), "latin1"));
    this.hashes.set(file, value);
    return value;
  }

  private embedded(dir: string, entry: Entry): string[] {
    if (entry.decl.kind !== "var") return [];
    const patterns = entry.decl.doc.flatMap((c) =>
      (EMBED.exec(c.text)?.[1] ?? "")
        .split(/\s+/)
        .map((p) => p.replace(/^["`]|["`]$/g, "").replace(/^all:/, ""))
        .filter((p) => p !== ""),
    );
    return patterns.flatMap((pattern) =>
      this.under(dir).filter((f) => {
        const inner = f.slice(dir.length + 1);
        return matchesGlob(inner, pattern) || inner.startsWith(`${pattern.replace(/\/$/, "")}/`);
      }),
    );
  }

  private named(dir: string, entry: Entry): string[] {
    const out: string[] = [];
    for (const tok of entry.decl.toks) {
      if (tok.kind !== "string" && tok.kind !== "raw") continue;
      const value = stringValue(tok);
      if (!TESTDATA.test(value)) continue;
      const resolved = path.posix.normalize(path.posix.join(dir, value));
      const abs = path.join(this.root, resolved);
      if (resolved.startsWith("..") || !existsSync(abs)) continue;
      out.push(...(statSync(abs).isDirectory() ? this.under(resolved) : [resolved]));
    }
    return out;
  }

  /** `path:digest` lines of every file the entries embed or name, sorted. */
  fingerprint(dir: string, entries: readonly Entry[]): string[] {
    const files = new Set(
      entries.flatMap((e) => [...this.embedded(dir, e), ...this.named(dir, e)]),
    );
    return [...files].sort().map((f) => `file ${f} ${this.hash(f)}`);
  }
}

function describe(files: Files, dir: string, pool: Pool, test: Entry, key: string): GoTestInfo {
  const reached = pool.closure(test);
  const all = [test, ...reached];
  const helpers = failingHelpers(reached);
  const reqs = requirementsOf(test);
  const decl = test.decl;
  const own = countCalls(decl.toks, test.handles, FAILING);
  const useOfHelpers = decl.toks.filter((t) => t.kind === "ident" && helpers.has(t.text)).length;
  const outputs = decl.names[0]?.startsWith("Example") && hasOutput(test) ? 1 : 0;
  const literals = all
    .flatMap((e) => e.decl.toks)
    .filter((t) => ["string", "raw", "rune", "number"].includes(t.kind))
    .map((t) => t.text)
    .sort();
  const skips = all.reduce(
    (sum, e) => sum + countCalls(e.decl.toks, e.handles, SKIPPING) + shortGates(e.decl.toks),
    0,
  );
  const parts = [
    test.text,
    ...reached.map((e) => e.text).sort(),
    ...files.fingerprint(dir, all),
    `reqs ${reqs.join(",")}`,
  ];
  return {
    key,
    name: decl.names[0] as string,
    file: test.file.path,
    line: decl.line,
    digest: sha16(parts.join("\n")),
    asserts: own + useOfHelpers + outputs,
    literals: sha16(literals.join("\u0000")),
    skips,
    reqs,
  };
}

/** An Example is a test through its `// Output:` comment. */
function hasOutput(test: Entry): boolean {
  return test.file.toks.some(
    (t) =>
      t.kind === "comment" &&
      t.line >= test.decl.line &&
      t.endLine <= test.decl.endLine &&
      OUTPUT.test(t.text),
  );
}

/** One package folder: its test files' declarations and the test functions among them. */
interface Pack {
  readonly dir: string;
  readonly files: readonly GoFile[];
  readonly pool: Pool;
  readonly tests: readonly Entry[];
}

const isTestFunc = (e: Entry): boolean =>
  e.decl.kind === "func" &&
  !e.decl.recv &&
  TEST_NAME.test(e.decl.names[0] ?? "") &&
  e.decl.names[0] !== "TestMain";

function packagesOf(root: string, config: GoConfig): Pack[] {
  const byDir = new Map<string, GoFile[]>();
  for (const file of goFiles(root, config).filter((f) => f.isTest)) {
    const dir = path.posix.dirname(file.path);
    byDir.set(dir, [...(byDir.get(dir) ?? []), file]);
  }
  return [...byDir]
    .sort(([a], [b]) => (a < b ? -1 : 1))
    .map(([dir, files]) => {
      const entries = files.flatMap((file) =>
        declsOf(file.toks, file.code).map((decl) => entryOf(decl, file)),
      );
      return { dir, files, pool: new Pool(entries), tests: entries.filter(isTestFunc) };
    });
}

/** Every Go test function of the module with its fingerprint; empty when the repository has no Go code. */
export function goTestInfos(root: string, config = loadGoConfig(root)): GoTestInfo[] {
  if (!config) return [];
  const files = new Files(root);
  const out: GoTestInfo[] = [];
  for (const pack of packagesOf(root, config)) {
    const seen = new Map<string, number>();
    for (const test of pack.tests) {
      const base = `go:${pack.dir}::${test.decl.names[0]}`;
      const count = (seen.get(base) ?? 0) + 1;
      seen.set(base, count);
      const key = count === 1 ? base : `${base}#${count}`;
      out.push(describe(files, pack.dir, pack.pool, test, key));
    }
  }
  return out;
}

/** The lock units of the Go tests. `literals` holds one entry: the digest of the literals the test reaches. */
export function goTestUnits(root: string): Unit[] {
  return goTestInfos(root).map((info) => ({
    key: info.key,
    kind: "test",
    digest: info.digest,
    path: info.file,
    line: info.line,
    asserts: info.asserts,
    literals: [info.literals],
    precision: [],
    skips: info.skips,
    reqs: info.reqs,
  }));
}

/** Requirement tags of the Go tests: `[id, file, line]`. */
export function goCitations(root: string): Array<[string, string, number]> {
  return goTestInfos(root).flatMap((i) =>
    i.reqs.map((id): [string, string, number] => [id, i.file, i.line]),
  );
}

/** GOTEST003: a Go test whose code, and the code it reaches, has no call that can fail it. */
export function assertionViolations(root: string, config: GoConfig): Violation[] {
  return goTestInfos(root, config)
    .filter((info) => info.asserts === 0)
    .map((info) =>
      violation(
        "GOTEST003",
        info.file,
        info.line,
        `Go test ${info.name} asserts nothing`,
        "Call t.Error, t.Fatal or a helper that does when the behaviour is wrong; a test that cannot fail proves nothing.",
      ),
    );
}

const STRAY_TAG = /\[\s*REQ-\d{3}/;

/**
 * TRACE007: a requirement tag in a comment of a Go test file that links nothing. Only the doc comment right above a
 * test function is read (and the string literals inside a test), so a tag anywhere else, or several ids in one
 * bracket, would look like a link and be none.
 */
export function strayTagViolations(root: string, config: GoConfig): Violation[] {
  const out: Violation[] = [];
  for (const pack of packagesOf(root, config)) {
    const linked = new Set<GoTok>(pack.tests.flatMap((t) => t.decl.doc));
    for (const file of pack.files) {
      for (const tok of file.toks) {
        const stray =
          tok.kind === "comment" &&
          STRAY_TAG.test(tok.text) &&
          (!linked.has(tok) || LIST_TAG.test(tok.text));
        if (stray)
          out.push(
            violation(
              "TRACE007",
              file.path,
              tok.line,
              "requirement tag in a comment that links no test",
              "Put one [REQ-NNN] per bracket in the doc comment directly above the Test function: // [REQ-001] [REQ-002]",
            ),
          );
      }
    }
  }
  return out;
}
