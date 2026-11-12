/**
 * One vocabulary on every screen (ADR-0183). The words people read must not drift back to the old ones.
 *
 * WORD001 a user-visible string (string literal, template text or JSX text of a non-test source file) holds a
 *         banned word from `harness/config/ui-words.yaml`; the message names the word to use instead
 * WORD002 the word list itself is malformed (bad pattern, missing replacement, exception without a reason)
 */
import path from "node:path";
import ts from "typescript";
import {
  findRoot,
  finish,
  flag,
  isMain,
  loadYaml,
  parseArgs,
  readText,
  type Violation,
  violation,
} from "../common";
import { isSourceFile, isTestFile, listFiles, matchesGlob } from "../files";
import { lineOf, parse, walk } from "../ts-ast";

const CONFIG_PATH = "harness/config/ui-words.yaml";

export interface Allow {
  /** An exact phrase that may contain the banned word. */
  readonly phrase?: string;
  /** Files (glob over repo-relative paths) where it applies; with no phrase, the rule is off in those files. */
  readonly path?: string;
  readonly reason: string;
}

export interface WordRule {
  readonly id: string;
  /** A regular expression (unicode) tested against each piece of visible text. */
  readonly pattern: string;
  /** The word or phrase to write instead. */
  readonly use: string;
  readonly allow?: readonly Allow[];
}

export interface WordsConfig {
  readonly scan_dirs: readonly string[];
  readonly rules: readonly WordRule[];
}

export interface TextPiece {
  readonly text: string;
  readonly line: number;
}

/** Every piece of text a person can read in a source file: string literals, template text, JSX text. */
export function visibleText(file: string, source: string): TextPiece[] {
  const sf = parse(file, source);
  const pieces: TextPiece[] = [];
  const add = (node: ts.Node, text: string): void => {
    if (/[가-힣]/.test(text)) pieces.push({ text, line: lineOf(sf, node.getStart(sf)) });
  };
  walk(sf, (node) => {
    const parent = node.parent;
    if (parent && (ts.isImportDeclaration(parent) || ts.isExportDeclaration(parent))) return;
    if (ts.isStringLiteral(node) || ts.isNoSubstitutionTemplateLiteral(node)) add(node, node.text);
    else if (ts.isTemplateHead(node) || ts.isTemplateMiddle(node) || ts.isTemplateTail(node))
      add(node, node.text);
    else if (ts.isJsxText(node)) add(node, node.text);
    // A Korean key such as `횟수: ...` names a field of the state 이끼 reads aloud, so it is read like text.
    else if (ts.isPropertyAssignment(node) && ts.isIdentifier(node.name))
      add(node.name, node.name.text.replaceAll("_", " "));
  });
  return pieces;
}

/** An exception holds when its path (if any) matches the file and its phrase (if any) covers the found word. */
function allowed(rule: WordRule, file: string, text: string, at: number, len: number): boolean {
  for (const a of rule.allow ?? []) {
    if (a.path && !matchesGlob(file, a.path)) continue;
    if (!a.phrase) return true;
    for (let from = text.indexOf(a.phrase); from >= 0; from = text.indexOf(a.phrase, from + 1))
      if (at >= from && at + len <= from + a.phrase.length) return true;
  }
  return false;
}

/** The violations of one file's visible text against the rules. */
export function checkText(file: string, source: string, rules: readonly WordRule[]): Violation[] {
  const found: Violation[] = [];
  const compiled = rules.map((rule) => ({ rule, re: new RegExp(rule.pattern, "gu") }));
  for (const piece of visibleText(file, source)) {
    for (const { rule, re } of compiled) {
      for (const m of piece.text.matchAll(re)) {
        if (allowed(rule, file, piece.text, m.index ?? 0, m[0].length)) continue;
        found.push(
          violation(
            "WORD001",
            file,
            piece.line,
            `"${m[0].trim()}" is an old word on screen (rule ${rule.id})`,
            `Write ${rule.use} instead. The word list is ${CONFIG_PATH}; the vocabulary is ADR-0183.`,
          ),
        );
      }
    }
  }
  return found;
}

export function configProblems(config: WordsConfig): Violation[] {
  const out: Violation[] = [];
  const bad = (message: string): number =>
    out.push(violation("WORD002", CONFIG_PATH, 0, message, "Fix the entry."));
  if (!config.scan_dirs?.length) bad("scan_dirs is empty");
  for (const rule of config.rules ?? []) {
    try {
      new RegExp(rule.pattern, "gu");
    } catch {
      bad(`rule ${rule.id}: pattern does not compile`);
    }
    if (!rule.use) bad(`rule ${rule.id}: names no word to use instead`);
    for (const a of rule.allow ?? [])
      if (!a.reason || (!a.phrase && !a.path))
        bad(`rule ${rule.id}: an exception needs a phrase or a path, and a reason`);
  }
  return out;
}

export function run(root: string): Violation[] {
  const config = loadYaml<WordsConfig>(path.join(root, CONFIG_PATH));
  const problems = configProblems(config);
  if (problems.length > 0) return problems;
  const out: Violation[] = [];
  const files = listFiles(root, config.scan_dirs, isSourceFile).filter((f) => !isTestFile(f));
  for (const file of files)
    out.push(...checkText(file, readText(path.join(root, file)), config.rules));
  return out;
}

export function main(argv: readonly string[], out: (line: string) => void = console.log): number {
  const { flags } = parseArgs(argv);
  return finish("ui-words", run(findRoot(flag(flags, "root"))), out);
}

if (isMain(import.meta.url)) process.exit(main(process.argv.slice(2)));
