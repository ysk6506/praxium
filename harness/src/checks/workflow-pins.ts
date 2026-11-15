/**
 * Actions that a workflow takes from outside are pinned to a commit (ADR-0064).
 *
 * WFL001 a workflow or composite action uses an outside action by a tag, a branch or no version at all
 *
 * A tag can be moved to other code after review; a commit hash cannot. So `uses: owner/name@<40 hex>` passes and
 * `uses: owner/name@v4` does not. An action of this repository (`./...`) is ours and passes. A container image
 * (`docker://`) must name its digest (`@sha256:`). The version in the comment after the hash is for people and is not
 * checked. The files read are the workflows (`.github/workflows`) and the composite actions (`.github/actions`).
 */
import { existsSync, readdirSync, statSync } from "node:fs";
import path from "node:path";
import { findRoot, finish, isMain, readText, rel, type Violation, violation } from "../common";

const DIRS = [".github/workflows", ".github/actions"];
const USES = /^\s*(?:-\s+)?uses\s*:\s*(.*?)\s*$/;
const COMMIT = /^[0-9a-f]{40}$/;

function yamlFiles(dir: string): string[] {
  if (!existsSync(dir)) return [];
  return readdirSync(dir)
    .sort()
    .flatMap((name) => {
      const full = path.join(dir, name);
      if (statSync(full).isDirectory()) return yamlFiles(full);
      return /\.ya?ml$/.test(name) ? [full] : [];
    });
}

/** The reference of a `uses:` value: quotes and the trailing comment are taken off. */
function referenceOf(value: string): string {
  const quoted = /^(["'])(.*?)\1/.exec(value);
  if (quoted) return quoted[2] as string;
  return value.replace(/\s+#.*$/, "").trim();
}

/** Why a reference is not pinned, or undefined when it is. */
function problemOf(reference: string): string | undefined {
  if (reference.startsWith("./")) return undefined;
  if (reference.startsWith("docker://")) {
    return /@sha256:[0-9a-f]{64}$/.test(reference)
      ? undefined
      : "a container image that is not named by its digest";
  }
  const at = reference.lastIndexOf("@");
  if (at < 0) return "no version";
  const version = reference.slice(at + 1);
  return COMMIT.test(version) ? undefined : `the version \`${version}\` is not a commit hash`;
}

export function workflowPinViolations(root: string): Violation[] {
  const out: Violation[] = [];
  for (const file of DIRS.flatMap((dir) => yamlFiles(path.join(root, dir)))) {
    readText(file)
      .split("\n")
      .forEach((line, index) => {
        const match = USES.exec(line);
        if (!match || (match[1] as string).startsWith("#")) return;
        const reference = referenceOf(match[1] as string);
        const problem = problemOf(reference);
        if (problem === undefined) return;
        out.push(
          violation(
            "WFL001",
            rel(root, file),
            index + 1,
            `\`${reference}\` is an outside action that is not pinned to a commit (${problem})`,
            "Use `uses: owner/name@<40-character commit hash> # vX.Y.Z`. Get the hash with `git ls-remote --tags https://github.com/owner/name` (the line with `^{}` is the commit of an annotated tag). See docs/guides/release.md, section 8.",
          ),
        );
      });
  }
  return out;
}

export function main(): number {
  return finish("workflow-pins", workflowPinViolations(findRoot()));
}

if (isMain(import.meta.url)) process.exit(main());
