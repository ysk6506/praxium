/**
 * Rules of the web UI that docs/design-docs/web-ui-rules.md states and a machine can check (ADR-0045, REQ-316, REQ-318).
 * The color pairs are WEB040 (web-colors.ts). The scans here read the sources of apps/web as text, like WEB020.
 *
 * WEB041 an unsafe sink: innerHTML or outerHTML assigned, dangerouslySetInnerHTML, insertAdjacentHTML, setHTMLUnsafe,
 *        document.write, eval, new Function, setTimeout or setInterval with a string
 * WEB042 an absolute http(s):// address in the sources or in index.html: the page asks nobody else for anything
 * WEB043 the focus outline is removed (outline-none, outline: none) with no visible replacement on the same element
 * WEB044 something moves (animation, transition, keyframes) and nothing in the CSS answers prefers-reduced-motion,
 *        or a Tailwind animate- utility is not behind motion-safe:
 * WEB045 the built JavaScript or CSS is gzipped to more than its budget
 */
import { existsSync, readdirSync, statSync } from "node:fs";
import path from "node:path";
import { gzipSync } from "node:zlib";
import { readText, type Violation, violation } from "../common";
import { type ContrastPair, contrastViolations } from "./web-colors";

export interface WebRules {
  /** Where the scans look: the sources of the page and its index.html (not the build configuration). */
  readonly scan: readonly string[];
  readonly contrast: readonly ContrastPair[];
  /** Files in which an absolute address is allowed (a test fixture for the client). Written one by one, never a folder. */
  readonly external_url_exempt: readonly string[];
  readonly budget: {
    readonly dist: string;
    readonly js_gzip_bytes: number;
    readonly css_gzip_bytes: number;
  };
}

interface RuleContext {
  readonly root: string;
  readonly scan: readonly string[];
  readonly generatedDir: string;
}

const TEST_FILE = /\.test\.[tj]sx?$|(^|\/)test-support\//;
const isComment = (line: string): boolean => /^\s*(\/\/|\/?\*)/.test(line);

function sourceFiles(root: string, entries: readonly string[]): string[] {
  const out: string[] = [];
  const visit = (rel: string): void => {
    const full = path.join(root, rel);
    if (!existsSync(full)) return;
    if (statSync(full).isFile()) {
      if (/\.(tsx?|css|html)$/.test(rel)) out.push(rel);
      return;
    }
    for (const name of readdirSync(full).sort()) {
      if (name !== "node_modules" && name !== "dist") visit(`${rel}/${name}`);
    }
  };
  for (const entry of entries) visit(entry);
  return out;
}

const SINKS: readonly [RegExp, string][] = [
  [/\bdangerouslySetInnerHTML\b/, "dangerouslySetInnerHTML"],
  [/\b(?:innerHTML|outerHTML)\s*(?:\+|-)?=(?!=)/, "an assignment to innerHTML or outerHTML"],
  [/\binsertAdjacentHTML\s*\(/, "insertAdjacentHTML"],
  [/\bsetHTMLUnsafe\s*\(/, "setHTMLUnsafe"],
  [/\bdocument\.write(?:ln)?\s*\(/, "document.write"],
  [/(?<![\w.])eval\s*\(/, "eval"],
  [/\bnew\s+Function\s*\(/, "new Function"],
  [/\b(?:setTimeout|setInterval)\s*\(\s*["'`]/, "setTimeout or setInterval with a string"],
];

export function sinkViolations(ctx: RuleContext): Violation[] {
  const out: Violation[] = [];
  for (const file of sourceFiles(ctx.root, ctx.scan)) {
    if (!/\.tsx?$/.test(file) || TEST_FILE.test(file)) continue;
    readText(path.join(ctx.root, file))
      .split("\n")
      .forEach((line, index) => {
        if (isComment(line)) return;
        for (const [pattern, name] of SINKS) {
          if (!pattern.test(line)) continue;
          out.push(
            violation(
              "WEB041",
              file,
              index + 1,
              `${name}: a way to run or insert text as code or markup`,
              "Draw values as text (React children, textContent). A value from the API is data and is never parsed as HTML or code.",
            ),
          );
        }
      });
  }
  return out;
}

const ABSOLUTE_URL = /https?:\/\//;

export function addressViolations(ctx: RuleContext, rules: WebRules): Violation[] {
  const out: Violation[] = [];
  for (const file of sourceFiles(ctx.root, ctx.scan)) {
    if (file.startsWith(`${ctx.generatedDir}/`) || rules.external_url_exempt.includes(file))
      continue;
    readText(path.join(ctx.root, file))
      .split("\n")
      .forEach((line, index) => {
        if (!ABSOLUTE_URL.test(line)) return;
        out.push(
          violation(
            "WEB042",
            file,
            index + 1,
            "an absolute http(s):// address: the page must work with no network but its own server",
            "Use a path of the same origin (/v1/...). A fixture of a test that needs an address is listed, file by file, in external_url_exempt of harness/config/web.yaml.",
          ),
        );
      });
  }
  return out;
}

const OUTLINE_OFF_CLASS = /(?<![\w-])outline-(?:none|0)(?![\w-])/;
const OUTLINE_OFF_CSS = /outline(?:-style)?\s*:\s*(?:none|0)\b/;

/** The rule blocks of a CSS text: the selector and the body, one level deep (media blocks are walked through). */
function cssRules(text: string): { selector: string; body: string; index: number }[] {
  const out: { selector: string; body: string; index: number }[] = [];
  const clean = text.replace(/\/\*[\s\S]*?\*\//g, (m) => m.replace(/[^\n]/g, " "));
  const pattern = /([^{}]+)\{([^{}]*)\}/g;
  for (const m of clean.matchAll(pattern)) {
    out.push({ selector: (m[1] as string).trim(), body: m[2] as string, index: m.index ?? 0 });
  }
  return out;
}

export function focusViolations(ctx: RuleContext): Violation[] {
  const out: Violation[] = [];
  const fix =
    "Keep the browser's outline, or replace it on the same element with a visible one (focus-visible:outline-2, focus-visible:ring...). WCAG 2.4.7, 2.4.11.";
  for (const file of sourceFiles(ctx.root, ctx.scan)) {
    if (TEST_FILE.test(file)) continue;
    const text = readText(path.join(ctx.root, file));
    if (/\.css$/.test(file)) {
      for (const rule of cssRules(text)) {
        if (!OUTLINE_OFF_CSS.test(rule.body)) continue;
        if (
          /:focus-visible/.test(rule.selector) ||
          /:focus:not\(:focus-visible\)/.test(rule.selector)
        )
          continue;
        const line = text
          .slice(0, text.indexOf(rule.body, Math.max(0, rule.index)))
          .split("\n").length;
        out.push(
          violation("WEB043", file, line, `'${rule.selector}' removes the focus outline`, fix),
        );
      }
    } else {
      text.split("\n").forEach((line, index) => {
        if (isComment(line) || !OUTLINE_OFF_CLASS.test(line) || /focus-visible:/.test(line)) return;
        out.push(
          violation(
            "WEB043",
            file,
            index + 1,
            "outline-none with no focus-visible: replacement on the same element",
            fix,
          ),
        );
      });
    }
  }
  return out;
}

const MOVES_IN_CSS =
  /@keyframes|(?<![\w-])animation\s*:|(?<![\w-])transition(?:-duration)?\s*:|--animate-|--default-transition-duration/;
const ANIMATE_CLASS = /(?<![\w-])((?:[\w[\]=.-]+:)*)animate-([\w-]+)/g;

export function motionViolations(ctx: RuleContext): Violation[] {
  const out: Violation[] = [];
  const files = sourceFiles(ctx.root, ctx.scan);
  const css = files.filter((f) => f.endsWith(".css"));
  const answers = css.some((f) => /prefers-reduced-motion/.test(readText(path.join(ctx.root, f))));
  for (const file of css) {
    const found = readText(path.join(ctx.root, file))
      .split("\n")
      .findIndex((l) => MOVES_IN_CSS.test(l));
    if (found >= 0 && !answers) {
      out.push(
        violation(
          "WEB044",
          file,
          found + 1,
          "something moves and no CSS answers prefers-reduced-motion",
          "Add @media (prefers-reduced-motion: reduce) { ... } that stops what moves (WCAG 2.3.3), or write the animation only inside prefers-reduced-motion: no-preference.",
        ),
      );
    }
  }
  for (const file of files) {
    if (!/\.tsx?$/.test(file) || TEST_FILE.test(file) || file.startsWith(`${ctx.generatedDir}/`))
      continue;
    readText(path.join(ctx.root, file))
      .split("\n")
      .forEach((line, index) => {
        for (const m of line.matchAll(ANIMATE_CLASS)) {
          if (m[2] === "none" || (m[1] as string).includes("motion-safe:")) continue;
          out.push(
            violation(
              "WEB044",
              file,
              index + 1,
              `${m[0]} is an animation that does not stop for a person who asked for less motion`,
              "Write it as motion-safe:animate-... so it runs only when the system does not ask for reduced motion.",
            ),
          );
        }
      });
  }
  return out;
}

/** The budget check reads the build output; with no build (the usual `pnpm check`) there is nothing to measure. */
export function budgetViolations(root: string, rules: WebRules, requireBuild = false): Violation[] {
  const dir = path.join(root, rules.budget.dist);
  if (!existsSync(dir)) {
    return requireBuild
      ? [
          violation(
            "WEB045",
            rules.budget.dist,
            0,
            "there is no build output to measure",
            "Run `pnpm --filter @praxium/web build` first.",
          ),
        ]
      : [];
  }
  const sum = { js: 0, css: 0 };
  for (const name of readdirSync(dir)) {
    const kind = name.endsWith(".js") ? "js" : name.endsWith(".css") ? "css" : undefined;
    if (kind) sum[kind] += gzipSync(readText(path.join(dir, name))).length;
  }
  const out: Violation[] = [];
  const limits = { js: rules.budget.js_gzip_bytes, css: rules.budget.css_gzip_bytes };
  for (const kind of ["js", "css"] as const) {
    if (sum[kind] <= limits[kind]) continue;
    out.push(
      violation(
        "WEB045",
        rules.budget.dist,
        0,
        `the ${kind.toUpperCase()} of the build is ${sum[kind]} bytes gzipped, over the budget of ${limits[kind]}`,
        "Find what grew (a dependency, a duplicated module) and remove it. The budget is raised only by an ADR that says why.",
      ),
    );
  }
  return out;
}

export function rulesViolations(
  root: string,
  config: {
    readonly rules?: WebRules;
    readonly token_file: string;
    readonly generated_dir: string;
  },
  requireBuild = false,
): Violation[] {
  const rules = config.rules;
  if (!rules) return [];
  const ctx: RuleContext = {
    root,
    scan: rules.scan,
    generatedDir: config.generated_dir,
  };
  return [
    ...contrastViolations(root, config.token_file, rules.contrast),
    ...sinkViolations(ctx),
    ...addressViolations(ctx, rules),
    ...focusViolations(ctx),
    ...motionViolations(ctx),
    ...budgetViolations(root, rules, requireBuild),
  ];
}
