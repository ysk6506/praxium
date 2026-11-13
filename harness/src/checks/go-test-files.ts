/**
 * The files a Go test can read (ADR-0023): `go:embed` files and the paths under `testdata/` that a string names. Their
 * contents go into the fingerprint of the tests that reach them.
 */
import { existsSync, readFileSync, statSync } from "node:fs";
import path from "node:path";
import { listFiles, matchesGlob } from "../files";
import { sha16 } from "../ts-ast";
import type { GoDecl } from "./go-decls";
import { stringValue } from "./go-lex";

const TESTDATA = /(^|\/)testdata(\/|$)/;
const EMBED = /^\/\/go:embed\s+(.+)$/;

/** Whatever has a declaration: an entry of a test pool. */
interface Declared {
  readonly decl: GoDecl;
}

/** The files a package's tests can read, found by what their strings and `go:embed` lines name. */
export class Files {
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

  private embedded(dir: string, entry: Declared): string[] {
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

  private named(dir: string, entry: Declared): string[] {
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
  fingerprint(dir: string, entries: readonly Declared[]): string[] {
    const files = new Set(
      entries.flatMap((e) => [...this.embedded(dir, e), ...this.named(dir, e)]),
    );
    return [...files].sort().map((f) => `file ${f} ${this.hash(f)}`);
  }
}
