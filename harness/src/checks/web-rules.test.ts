import { randomBytes } from "node:crypto";
import { readFileSync } from "node:fs";
import path from "node:path";
import { describe, expect, test } from "vitest";
import { miniRepo } from "../test-support/mini-repo";
import { loadWebConfig } from "./web";
import {
  contrastRatio,
  contrastResults,
  oklchToSrgb,
  parseOklch,
  readThemes,
  tokenColor,
} from "./web-colors";
import { budgetViolations, rulesViolations } from "./web-rules";

const CONFIG = `
package: apps/web/package.json
dependencies: []
dev_dependencies: []
token_file: apps/web/src/ui/tokens.css
scan: [apps/web/src]
contract: contract.json
generated_dir: apps/web/src/api/generated
generator_script: scripts/web-gen-api.ts
backend: {root: backend, embed_dir: e, placeholder: p, forbidden_reference: apps/web, gitignore: .gitignore, ignore_lines: []}
rules:
  scan: [apps/web/src, apps/web/index.html]
  contrast:
    - {on: fg, background: canvas, min: 4.5}
    - {on: line, background: canvas, min: 3.0, note: an edge}
  external_url_exempt: [apps/web/src/fixture.test.ts]
  budget: {dist: apps/web/dist/assets, js_gzip_bytes: 400, css_gzip_bytes: 400}
`;

const TOKENS = `
:root { --px-canvas: oklch(1 0 0); --px-fg: oklch(0.2 0 0); --px-line: oklch(0.6 0 0); }
@media (prefers-color-scheme: dark) {
  :root { --px-canvas: oklch(0.2 0 0); --px-fg: oklch(0.95 0 0); }
}
`;

const run = (files: Record<string, string>, requireBuild = false) => {
  const root = miniRepo(
    { "harness/config/web.yaml": CONFIG, "apps/web/src/ui/tokens.css": TOKENS, ...files },
    {},
  );
  return rulesViolations(root, loadWebConfig(root), requireBuild);
};
const codes = (files: Record<string, string>) => run(files).map((v) => v.code);

describe("[REQ-316] oklch becomes sRGB and a pair gets its WCAG contrast", () => {
  test("white, black and a mid gray give the known values", () => {
    const white = oklchToSrgb(1, 0, 0);
    const black = oklchToSrgb(0, 0, 0);
    expect(white.r).toBeCloseTo(1, 3);
    expect(black.g).toBeCloseTo(0, 3);
    expect(contrastRatio(white, black)).toBeCloseTo(21, 1);
    const gray = oklchToSrgb(0.5, 0, 0);
    expect(gray.r).toBeCloseTo(0.389, 2);
    expect(contrastRatio(gray, white)).toBeGreaterThan(5.5);
    expect(contrastRatio(gray, white)).toBeLessThan(6.5);
  });

  test("a saturated color is clipped into the gamut and a red is not dark", () => {
    const red = oklchToSrgb(0.628, 0.2577, 29.23);
    expect(red.r).toBeCloseTo(1, 1);
    expect(red.g).toBeLessThan(0.1);
  });

  test("percent lightness, alpha and other notations are read or refused", () => {
    expect(parseOklch("oklch(50% 0.1 120)")).toEqual({ l: 0.5, c: 0.1, h: 120, a: 1 });
    expect(parseOklch("oklch(0.2 0.02 258 / 0.4)")?.a).toBe(0.4);
    expect(parseOklch("#fff")).toBeUndefined();
    expect(tokenColor({ "--px-x": "oklch(0.2 0.02 258 / 0.4)" }, "x")).toBeUndefined();
  });

  test("the dark theme is the light one with the dark block laid over it", () => {
    const themes = readThemes(TOKENS);
    expect(themes.light["--px-fg"]).toBe("oklch(0.2 0 0)");
    expect(themes.dark["--px-fg"]).toBe("oklch(0.95 0 0)");
    expect(themes.dark["--px-line"]).toBe("oklch(0.6 0 0)");
    const results = contrastResults(TOKENS, [{ on: "fg", background: "canvas", min: 4.5 }]);
    expect(results.map((r) => r.theme)).toEqual(["light", "dark"]);
    expect(results.every((r) => (r.ratio ?? 0) > 10)).toBe(true);
  });
});

describe("WEB040: a pair of tokens below its contrast in either theme [REQ-316]", () => {
  test("the pairs of the config pass in both themes", () => {
    expect(codes({})).toEqual([]);
  });

  test("a dark theme that is too dim for the text, and a light edge that is too faint, fail with problem and fix", () => {
    const found = run({
      "apps/web/src/ui/tokens.css": `
        :root { --px-canvas: oklch(1 0 0); --px-fg: oklch(0.2 0 0); --px-line: oklch(0.9 0 0); }
        @media (prefers-color-scheme: dark) { :root { --px-canvas: oklch(0.2 0 0); --px-fg: oklch(0.3 0 0); } }`,
    });
    expect(found.map((v) => v.code)).toEqual(["WEB040", "WEB040"]);
    expect(found.map((v) => v.message).join("\n")).toMatch(/fg on canvas \(dark\)/);
    expect(found.map((v) => v.message).join("\n")).toMatch(/line on canvas \(light\)/);
    expect(found[0]?.fix).toMatch(/lightness/);
  });

  test("a token that the pair names and the file lacks is WEB040", () => {
    expect(
      codes({ "apps/web/src/ui/tokens.css": ":root { --px-canvas: oklch(1 0 0); }" }),
    ).toContain("WEB040");
  });
});

describe("WEB041: sinks that run or insert text as code or markup [REQ-318]", () => {
  test.each([
    ["el.innerHTML = value;"],
    ["el.outerHTML += value;"],
    ["<div dangerouslySetInnerHTML={{ __html: x }} />"],
    ["el.insertAdjacentHTML('beforeend', x);"],
    ["el.setHTMLUnsafe(x);"],
    ["document.write(x);"],
    ["eval(x);"],
    ["const f = new Function('a', x);"],
    ["setTimeout('go()', 10);"],
  ])("%s is WEB041", (line) => {
    expect(codes({ "apps/web/src/a.tsx": `${line}\n` })).toEqual(["WEB041"]);
  });

  test("text, a function passed to setTimeout, a comparison, a comment and a test file are clean", () => {
    expect(
      codes({
        "apps/web/src/a.tsx":
          "el.textContent = x;\nsetTimeout(() => go(), 10);\nif (el.innerHTML === '') {}\n// eval(x) is refused\nretrieval(x);\n",
        "apps/web/src/a.test.tsx": "document.body.innerHTML = '';\n",
      }),
    ).toEqual([]);
  });
});

describe("WEB042: an absolute address in the sources [REQ-318]", () => {
  test("an http or https address in a source, in index.html or in CSS is WEB042", () => {
    expect(codes({ "apps/web/src/a.ts": "const u = 'https://example.org/x';\n" })).toEqual([
      "WEB042",
    ]);
    expect(
      codes({ "apps/web/index.html": '<script src="http://cdn.example/x.js"></script>\n' }),
    ).toEqual(["WEB042"]);
    expect(codes({ "apps/web/src/a.css": "@import url(https://fonts.example/a.css);\n" })).toEqual([
      "WEB042",
    ]);
  });

  test("a path of the same origin and an exempted fixture file are clean; a neighbor of the fixture is not", () => {
    expect(codes({ "apps/web/src/a.ts": "fetch('/v1/kgs');\n" })).toEqual([]);
    expect(codes({ "apps/web/src/fixture.test.ts": "new URL('http://fake');\n" })).toEqual([]);
    expect(codes({ "apps/web/src/other.test.ts": "new URL('http://fake');\n" })).toEqual([
      "WEB042",
    ]);
  });
});

describe("WEB043: the focus outline is removed [REQ-318]", () => {
  test("outline-none without a focus-visible replacement on the same line is WEB043", () => {
    expect(codes({ "apps/web/src/a.tsx": '<a className="px-2 outline-none">x</a>\n' })).toEqual([
      "WEB043",
    ]);
    expect(codes({ "apps/web/src/a.css": "a { outline: none; }\n" })).toEqual(["WEB043"]);
    expect(codes({ "apps/web/src/a.css": "a { outline: 0 }\n" })).toEqual(["WEB043"]);
  });

  test("a replacement on the same element, the focus-visible pattern and the browser's own outline are clean", () => {
    expect(
      codes({
        "apps/web/src/a.tsx":
          '<a className="outline-none focus-visible:outline-2">x</a>\n<b className="p-1" />\n',
        "apps/web/src/a.css":
          "a:focus:not(:focus-visible) { outline: none; }\n:focus-visible { outline: 2px solid red; }\n",
      }).filter((c) => c === "WEB043"),
    ).toEqual([]);
  });
});

describe("WEB044: motion that does not stop for a person who asked for less [REQ-317]", () => {
  test("keyframes or a transition with no prefers-reduced-motion anywhere in the CSS is WEB044", () => {
    expect(codes({ "apps/web/src/a.css": "@keyframes s { to { opacity: 1 } }\n" })).toEqual([
      "WEB044",
    ]);
    expect(codes({ "apps/web/src/a.css": "a { transition: color 1s }\n" })).toEqual(["WEB044"]);
  });

  test("one rule for reduced motion anywhere in the CSS answers it", () => {
    expect(
      codes({
        "apps/web/src/a.css": "a { transition: color 1s }\n",
        "apps/web/src/b.css":
          "@media (prefers-reduced-motion: reduce) { html { scroll-behavior: auto } }\n",
      }),
    ).toEqual([]);
  });

  test("an animate- utility is clean only behind motion-safe:", () => {
    const css = {
      "apps/web/src/b.css": "@media (prefers-reduced-motion: reduce) { a { color: red } }\n",
    };
    expect(
      codes({ ...css, "apps/web/src/a.tsx": '<i className="size-4 animate-spin" />\n' }),
    ).toEqual(["WEB044"]);
    expect(
      codes({ ...css, "apps/web/src/a.tsx": '<i className="size-4 hover:animate-pulse" />\n' }),
    ).toEqual(["WEB044"]);
    expect(
      codes({
        ...css,
        "apps/web/src/a.tsx": '<i className="motion-safe:animate-spin animate-none" />\n',
      }),
    ).toEqual([]);
  });
});

describe("WEB045: the build is over its budget [REQ-318]", () => {
  const big = randomBytes(3000).toString("hex");
  test("a small build passes and a large one is WEB045 for the kind that is large", () => {
    expect(codes({ "apps/web/dist/assets/a.js": "a", "apps/web/dist/assets/a.css": "b" })).toEqual(
      [],
    );
    const found = run({ "apps/web/dist/assets/a.js": big, "apps/web/dist/assets/a.css": "b" });
    expect(found.map((v) => v.code)).toEqual(["WEB045"]);
    expect(found[0]?.message).toMatch(/JS of the build is \d+ bytes gzipped/);
    expect(found[0]?.fix).toMatch(/ADR/);
  });

  test("with no build the check is silent, unless the build is required (the CI step after the web build)", () => {
    expect(codes({})).toEqual([]);
    const root = miniRepo({ "harness/config/web.yaml": CONFIG }, {});
    const rules = loadWebConfig(root).rules;
    expect(rules && budgetViolations(root, rules, true).map((v) => v.code)).toEqual(["WEB045"]);
  });
});

describe("[REQ-319] the support floor is written down and the build targets it", () => {
  const root = path.resolve(__dirname, "../../..");
  const read = (file: string) => readFileSync(path.join(root, file), "utf8");

  test("the build target names the browsers of the floor, not a language edition", () => {
    const vite = read("apps/web/config/vite.config.ts");
    for (const target of ["chrome111", "edge111", "firefox128", "safari16.4"])
      expect(vite).toContain(`"${target}"`);
    expect(vite).not.toMatch(/target:\s*"es\d+"/);
  });

  test("the rules document states the floor, the policy, what is not adopted and the license, and AGENTS.md leads to it", () => {
    const doc = read("docs/design-docs/web-ui-rules.md");
    for (const must of [
      "Baseline Widely available",
      "Newly available",
      "크롬·엣지 111",
      "파이어폭스 128",
      "사파리 16.4",
      "CC BY 4.0",
      "5e5cd33",
      "채택하지 않는 것",
    ])
      expect(doc).toContain(must);
    expect(read("AGENTS.md")).toContain("docs/design-docs/web-ui-rules.md");
  });
});
