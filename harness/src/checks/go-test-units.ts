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
 * Non-test code that the contract names in `go.locked_support` (a conformance suite, a case table) is fingerprinted
 * the same way: a test whose file imports such a package, or that sits in its folder, reaches that package's
 * declarations by `package.Name`, and from there everything they reach inside the package. Removing a case or
 * weakening one inside the table then changes the digest of the test that runs it (ADR-0028).
 *
 * Besides the digest the unit carries what a weakening would change (like the TypeScript units): the number of calls
 * that can fail the test, a digest of every literal it reaches, and the number of skips.
 *
 * GOTEST003 a Go test function whose code cannot fail the test (nothing that calls `t.Error`, `t.Fatal`, `t.Fail`...)
 */
import path from "node:path";
import { type Violation, violation } from "../common";
import { sha16 } from "../ts-ast";
import { type GoConfig, type GoFile, goFiles, loadGoConfig, moduleDir } from "./go-config";
import { declsOf } from "./go-decls";
import { type GoTok, localName } from "./go-lex";
import { type Entry, entryOf, failingHelpers, isPunct } from "./go-test-entry";
import { Files } from "./go-test-files";
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
const TAG = /\[(REQ-\d{3})\]/g;
const LIST_TAG = /\[\s*REQ-\d{3}\s*[,;]/;
const OUTPUT = /^\/\/\s*(unordered output|output):/i;

/** The declarations of one locked-support package folder, indexed the way a pool indexes its own. */
class Support {
  readonly byName = new Map<string, Entry[]>();
  readonly byMethod = new Map<string, Entry[]>();
  readonly byRecv = new Map<string, Entry[]>();

  constructor(
    readonly dir: string,
    entries: readonly Entry[],
  ) {
    for (const e of entries) {
      if (e.decl.recv) {
        push(this.byMethod, e.decl.names[0] ?? "", e);
        push(this.byRecv, e.decl.recv, e);
      } else for (const name of e.decl.names) if (name !== "_") push(this.byName, name, e);
    }
  }
}

/**
 * The test-only declarations of one package folder, and how they reach each other. The one-step reach of a declaration
 * is worked out once and kept (ADR-0124); a search then walks those lists in the order it always did.
 */
class Pool {
  private readonly byName = new Map<string, Entry[]>();
  private readonly byMethod = new Map<string, Entry[]>();
  private readonly byRecv = new Map<string, Entry[]>();
  private readonly always: Entry[] = [];
  private readonly steps = new Map<Entry, readonly Entry[]>();
  private readonly methods = new Map<Entry, readonly Entry[]>();

  constructor(
    readonly entries: readonly Entry[],
    private readonly supports: readonly Support[] = [],
    private readonly config?: GoConfig,
  ) {
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

  private supportOf(e: Entry): Support | undefined {
    return e.supportDir === undefined
      ? undefined
      : this.supports.find((sup) => sup.dir === e.supportDir);
  }

  /**
   * Declarations of a locked-support package that `e` reaches: by bare name from inside the package (its own files and
   * the tests of the same folder), by `package.Name` from anywhere that imports it. Methods follow by member name.
   */
  private reachSupport(e: Entry, visit: (e: Entry) => void): void {
    for (const sup of this.supports) {
      const inside = e.supportDir === sup.dir || path.posix.dirname(e.file.path) === sup.dir;
      const qualifiers = importedAs(this.config, e.file, sup.dir);
      const names = [...(inside ? e.refs : []), ...qualifiedNames(e.decl.toks, qualifiers)];
      for (const name of names) sup.byName.get(name)?.forEach(visit);
      if (inside || qualifiers.size > 0)
        for (const member of e.members) sup.byMethod.get(member)?.forEach(visit);
    }
  }

  /** What `e` reaches in one step by its names, members and locked-support references, in visiting order. */
  private stepsOf(e: Entry): readonly Entry[] {
    const hit = this.steps.get(e);
    if (hit) return hit;
    const out: Entry[] = [];
    const add = (next: Entry): void => {
      out.push(next);
    };
    for (const ref of e.refs) this.byName.get(ref)?.forEach(add);
    for (const member of e.members) this.byMethod.get(member)?.forEach(add);
    this.reachSupport(e, add);
    this.steps.set(e, out);
    return out;
  }

  /** The methods that come with a type when it is reached: those of the pool, then those of its support package. */
  private methodsOf(e: Entry): readonly Entry[] {
    const hit = this.methods.get(e);
    if (hit) return hit;
    const out: Entry[] = [];
    if (e.decl.kind === "type")
      for (const n of e.decl.names) {
        out.push(...(this.byRecv.get(n) ?? []));
        out.push(...(this.supportOf(e)?.byRecv.get(n) ?? []));
      }
    this.methods.set(e, out);
    return out;
  }

  /** Everything `start` reaches by name, `start` itself left out. Package hooks and blank variables are always in. */
  closure(start: Entry): Entry[] {
    const seen = new Set<Entry>([start, ...this.always]);
    const queue = [start, ...this.always];
    const visit = (e: Entry): void => {
      if (seen.has(e)) return;
      seen.add(e);
      queue.push(e);
      for (const method of this.methodsOf(e)) visit(method);
    };
    for (let e = queue.pop(); e; e = queue.pop()) for (const next of this.stepsOf(e)) visit(next);
    seen.delete(start);
    return [...seen];
  }
}

/** The names a file gives to the imports that lead into the folder `dir` of this module. */
function importedAs(config: GoConfig | undefined, file: GoFile, dir: string): Set<string> {
  const names = new Set<string>();
  if (!config) return names;
  for (const imp of file.imports) {
    const name = moduleDir(config, imp.path) === dir ? localName(imp) : undefined;
    if (name) names.add(name);
  }
  return names;
}

/** Names that follow `qualifier.` in the tokens: `graphcheck.Cases` gives `Cases`. */
function qualifiedNames(toks: readonly GoTok[], qualifiers: ReadonlySet<string>): string[] {
  const found: string[] = [];
  toks.forEach((tok, k) => {
    const member = toks[k + 2];
    if (
      tok.kind === "ident" &&
      qualifiers.has(tok.text) &&
      isPunct(toks[k + 1], ".") &&
      !isPunct(toks[k - 1], ".") &&
      member?.kind === "ident"
    )
      found.push(member.text);
  });
  return found;
}

function push(map: Map<string, Entry[]>, key: string, entry: Entry): void {
  const list = map.get(key);
  if (list) list.push(entry);
  else map.set(key, [entry]);
}

const tagsIn = (text: string): string[] => [...text.matchAll(TAG)].map((m) => m[1] as string);

/** Tags in the doc comment above a test and in the string literals of its body (subtest names, table rows). */
function requirementsOf(test: Entry): string[] {
  const found = test.decl.doc.flatMap((c) => tagsIn(c.text));
  for (const tok of test.decl.toks)
    if (tok.kind === "string" || tok.kind === "raw") found.push(...tagsIn(tok.text));
  return [...new Set(found)].sort();
}

function describe(files: Files, dir: string, pool: Pool, test: Entry, key: string): GoTestInfo {
  const reached = pool.closure(test);
  const all = [test, ...reached];
  const helpers = failingHelpers(reached);
  const reqs = requirementsOf(test);
  const decl = test.decl;
  const useOfHelpers = decl.toks.filter((t) => t.kind === "ident" && helpers.has(t.text)).length;
  const outputs = decl.names[0]?.startsWith("Example") && hasOutput(test) ? 1 : 0;
  const literals = all.flatMap((e) => e.literals).sort();
  const skips = all.reduce((sum, e) => sum + e.skips, 0);
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
    asserts: test.fails + useOfHelpers + outputs,
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

const isTestPath = (file: string): boolean => file.endsWith("_test.go");

/** A non-test file of a folder or file that `go.locked_support` names. */
const isSupportPath = (config: GoConfig, file: string): boolean =>
  !isTestPath(file) &&
  config.lockedSupport.some((s) => file === s || path.posix.dirname(file) === s);

/** The non-test declarations of every folder or file that `go.locked_support` names, grouped by package folder. */
function supportsOf(named: readonly GoFile[]): Support[] {
  const byDir = new Map<string, Entry[]>();
  for (const file of named) {
    const dir = path.posix.dirname(file.path);
    const entries = declsOf(file.toks, file.code).map((decl) => entryOf(decl, file, dir));
    byDir.set(dir, [...(byDir.get(dir) ?? []), ...entries]);
  }
  return [...byDir].map(([dir, entries]) => new Support(dir, entries));
}

/** The test packages of the module. Only test files and locked-support files are read, each once (ADR-0124). */
function packagesOf(root: string, config: GoConfig): Pack[] {
  const byDir = new Map<string, GoFile[]>();
  const read = goFiles(root, config, (f) => isTestPath(f) || isSupportPath(config, f));
  const supports = supportsOf(read.filter((f) => !f.isTest));
  for (const file of read.filter((f) => f.isTest)) {
    const dir = path.posix.dirname(file.path);
    byDir.set(dir, [...(byDir.get(dir) ?? []), file]);
  }
  return [...byDir]
    .sort(([a], [b]) => (a < b ? -1 : 1))
    .map(([dir, files]) => {
      const entries = files.flatMap((file) =>
        declsOf(file.toks, file.code).map((decl) => entryOf(decl, file)),
      );
      return {
        dir,
        files,
        pool: new Pool(entries, supports, config),
        tests: entries.filter(isTestFunc),
      };
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
