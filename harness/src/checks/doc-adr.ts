/** ADR rules: sealing, supersession and enforcement links (DOC071, DOC070, DOC060, DOC080). */
import { existsSync, readFileSync } from "node:fs";
import path from "node:path";
import { type Violation, violation } from "../common";
import { listFiles } from "../files";
import { bodyFingerprint } from "./doc-print";

const RULE_CODE = /^[A-Z]{2,6}\d{3}$/;

let ruleCodeCache: { root: string; text: string } | undefined;

/** Text of every harness source file, used to prove a rule code really exists. */
export function harnessText(root: string): string {
  if (ruleCodeCache?.root !== root) {
    const files = listFiles(
      root,
      ["harness/src"],
      (name) => name.endsWith(".ts") && !name.endsWith(".test.ts"),
    );
    ruleCodeCache = {
      root,
      text: files.map((f) => readFileSync(path.join(root, f), "utf8")).join("\n"),
    };
  }
  return ruleCodeCache.text;
}

export function checkAdr(
  root: string,
  file: string,
  data: Record<string, unknown>,
  body: string,
): Violation[] {
  const out: Violation[] = [];
  const status = data.status;
  if (status === "active" || status === "superseded") {
    if (typeof data.decision_hash !== "string") {
      out.push(
        violation(
          "DOC071",
          file,
          1,
          "accepted ADR has no decision_hash",
          `Seal it: pnpm tsx harness/src/checks/docs.ts seal-adr ${file}`,
        ),
      );
    } else if (data.decision_hash !== bodyFingerprint(body)) {
      out.push(
        violation(
          "DOC070",
          file,
          1,
          "an accepted ADR's body was edited (ADRs are immutable)",
          "Revert the edit. To change a decision, write a new ADR that supersedes this one and set superseded_by here.",
        ),
      );
    }
  }
  if (status === "superseded") {
    const target = data.superseded_by;
    if (typeof target !== "string" || !existsSync(path.join(root, path.dirname(file), target))) {
      out.push(
        violation(
          "DOC060",
          file,
          1,
          "superseded ADR needs an existing superseded_by file",
          "Set superseded_by to the filename of the ADR that replaces it.",
        ),
      );
    }
  }
  if (status === "active") out.push(...checkEnforcement(root, file, data));
  return out;
}

function checkEnforcement(root: string, file: string, data: Record<string, unknown>): Violation[] {
  const enforced = data.enforced_by;
  if (!Array.isArray(enforced) || enforced.length === 0) {
    return [
      violation(
        "DOC080",
        file,
        1,
        "ADR has no enforced_by list",
        "List the rule codes or tests that enforce it, or [none] plus a no_enforcement_reason.",
      ),
    ];
  }
  if (enforced.length === 1 && enforced[0] === "none") {
    return typeof data.no_enforcement_reason === "string" && data.no_enforcement_reason.trim()
      ? []
      : [
          violation(
            "DOC080",
            file,
            1,
            "enforced_by: [none] needs no_enforcement_reason",
            "Explain why the decision cannot be checked mechanically.",
          ),
        ];
  }
  const out: Violation[] = [];
  for (const item of enforced.map(String)) {
    if (RULE_CODE.test(item)) {
      if (!harnessText(root).includes(`"${item}"`)) {
        out.push(
          violation(
            "DOC080",
            file,
            1,
            `rule code ${item} is not defined by any check`,
            "Use a real rule code from harness/src/checks or a test path.",
          ),
        );
      }
    } else if (!existsSync(path.join(root, item.split("::")[0] as string))) {
      out.push(
        violation(
          "DOC080",
          file,
          1,
          `enforcement target '${item}' does not exist`,
          "Point enforced_by at an existing test file or rule code.",
        ),
      );
    }
  }
  return out;
}
