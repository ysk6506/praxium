/**
 * What a document is verified against (ADR-0194), and the cheap lookups the docs check shares in one run:
 * the file list is read once, every file is hashed once, a glob is expanded once.
 *
 * A target is one file. `covers` and `follows` entries are expanded to the files they match, and each file is its own
 * target, so a change to one covered file only makes that file's record stale and re-verifying it touches one file.
 */
import { existsSync, readFileSync, statSync } from "node:fs";
import path from "node:path";
import { splitFrontMatter } from "../common";
import { listFiles, matchesGlob } from "../files";
import { normalized, sha16 } from "../ts-ast";
import { bodyFingerprint } from "./doc-print";
import { RECORDS_DIR, RecordStore } from "./doc-records";

const WILDCARD = /[*?]/;

const strings = (value: unknown): string[] => (Array.isArray(value) ? value.map(String) : []);

export class DocFiles {
  private list: string[] | undefined;
  private readonly expanded = new Map<string, string[]>();
  private readonly hashes = new Map<string, string>();

  constructor(readonly root: string) {}

  /** Every repository file (posix, sorted), without the verification records themselves. */
  all(): string[] {
    this.list ??= listFiles(this.root, ["."], () => true)
      .map((f) => f.replace(/^\.\//, ""))
      .filter((f) => !f.startsWith(`${RECORDS_DIR}/`));
    return this.list;
  }

  /** Files matching one glob; a directory matches everything inside it. */
  expand(glob: string): string[] {
    const hit = this.expanded.get(glob);
    if (hit) return hit;
    const found = this.match(glob);
    this.expanded.set(glob, found);
    return found;
  }

  private match(glob: string): string[] {
    const plain = glob.replace(/\/$/, "");
    const full = path.join(this.root, plain);
    if (!WILDCARD.test(glob) && existsSync(full) && statSync(full).isDirectory())
      return this.all().filter((f) => f.startsWith(`${plain}/`));
    const prefix = glob.slice(0, glob.search(WILDCARD) < 0 ? glob.length : glob.search(WILDCARD));
    return this.all().filter((f) => f.startsWith(prefix) && matchesGlob(f, glob));
  }

  /** The content hash of a file: TypeScript by structure, Markdown by body, anything else by bytes. */
  hash(file: string): string {
    const known = this.hashes.get(file);
    if (known) return known;
    const text = readFileSync(path.join(this.root, file), "utf8");
    let value: string;
    if (/\.tsx?$/.test(file)) value = sha16(normalized(file, text));
    else if (file.endsWith(".md")) value = bodyFingerprint(splitFrontMatter(text).body);
    else value = sha16(text);
    this.hashes.set(file, value);
    return value;
  }
}

/** Everything one run of the docs check shares. */
export class DocContext {
  readonly files: DocFiles;
  readonly records: RecordStore;
  constructor(readonly root: string) {
    this.files = new DocFiles(root);
    this.records = new RecordStore(root);
  }
}

export interface DeclaredTargets {
  readonly covers: string[];
  readonly follows: string[];
  /** Files matched by `covers`, then by `follows`, each once. */
  readonly targets: string[];
  /** The entries that match no file, per key. */
  readonly empty: { readonly covers: string[]; readonly follows: string[] };
}

/** The targets a document's `covers` and `follows` declare, with the entries that match nothing. */
export function declaredTargets(files: DocFiles, data: Record<string, unknown>): DeclaredTargets {
  const covers = strings(data.covers);
  const follows = strings(data.follows);
  const matched = (globs: string[]): string[] => globs.flatMap((g) => files.expand(g));
  const emptyOf = (globs: string[]): string[] => (matched(globs).length === 0 ? globs : []);
  const targets = [...new Set([...matched(covers), ...matched(follows)])].sort();
  return {
    covers,
    follows,
    targets,
    empty: { covers: emptyOf(covers), follows: emptyOf(follows) },
  };
}
