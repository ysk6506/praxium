import { describe, expect, test } from "vitest";
import { ADR_DIR, adr, docsRepo, front, TODAY } from "../test-support/docs-repo";
import { run } from "./docs";

const ROUTES =
  'export const ROUTES = {\n  a: {\n    method: "GET",\n    path: "/api/worlds/:id",\n  },\n  b: {\n    method: "PUT",\n    path: "/api/worlds/:id",\n  },\n};\n';
const FACADE = 'router.on("GET", "/api/vcenter/vm", (q) => q);\n';
const repo = (guide: string, extra: Record<string, string> = {}): string =>
  docsRepo({
    "docs/guide.md": `${front()}# G\n${guide}\n`,
    "packages/core/src/contract/routes.ts": ROUTES,
    "packages/core/src/facades/vc.ts": FACADE,
    "packages/core/src/impact.ts": "export function impactOf(): number {\n  return 1;\n}\n",
    "packages/core/src/other.ts": "export const other = 1;\n",
    "apps/server/src/env.ts": "export const V = process.env.PRAXIUM_PORT;\n",
    "harness/src/note.ts": 'export const N = "NOTE x [QUAL001] y";\n',
    ...extra,
  });
const messages = (root: string): string[] =>
  run(root, TODAY)
    .filter((v) => v.code === "DOC094")
    .map((v) => v.message);

describe("what a document names must exist [REQ-020]", () => {
  test("things that exist pass: env var, script, route, facade route, rule code, symbol, ADR", () => {
    const root = repo(
      [
        "`PRAXIUM_PORT` sets it. Run `pnpm check`, `pnpm install` and `pnpm tsx x.ts`.",
        "`GET /api/worlds/:id` and `PUT /api/worlds/{id}` and `GET /api/vcenter/vm`.",
        "Codes `ARCH010` and `QUAL001`. `impact.ts`의 `impactOf`, `impactOf`(`impact.ts`).",
        "ADR-0001 and REQ-001 stay quiet when they exist.",
      ].join("\n"),
      {
        [`${ADR_DIR}/0001-x.md`]: adr(),
        "docs/product-specs/requirements.md": `${front()}| REQ-001 | x |\n`,
        "docs/index.md": `${front()}[g](guide.md) [d](design-docs/adr/) [r](product-specs/requirements.md)\n`,
      },
    );
    expect(messages(root)).toEqual([]);
  });

  test("an unknown environment variable, script or rule code fails", () => {
    const found = messages(repo("`PRAXIUM_GONE` and `pnpm nothing` and `ARCH999`."));
    expect(found).toHaveLength(3);
    expect(found.join("\n")).toMatch(/PRAXIUM_GONE.*pnpm nothing.*ARCH999/s);
  });

  test("a route fails when the contract lacks it or has it under another method", () => {
    const found = messages(
      repo("`PATCH /api/worlds/:id` and `GET /api/gone` and `DELETE /api/vcenter/vm`."),
    );
    expect(found).toHaveLength(3);
  });

  test("a name written next to a file must be in that file; names next to nothing are not checked", () => {
    const found = messages(
      repo("`other.ts`의 `impactOf` and `gone`(`impact.ts`) and plain `lastVerified`."),
    );
    expect(found).toHaveLength(2);
    expect(found.filter((m) => m.includes("impactOf"))).toHaveLength(1);
    expect(found.filter((m) => m.includes("`gone`"))).toHaveLength(1);
  });

  test("a missing ADR or REQ fails unless the line says it is being made", () => {
    const root = repo("ADR-0999 and REQ-999.\nADR-0998 (만드는 중).", {
      "docs/product-specs/requirements.md": `${front()}| REQ-001 | x |\n`,
      "docs/index.md": `${front()}[g](guide.md) [r](product-specs/requirements.md)\n`,
    });
    expect(messages(root)).toEqual([
      "ADR-0999 does not exist",
      "REQ-999 is not in the requirements",
    ]);
  });

  test("ADR bodies are not scanned", () => {
    const root = repo("fine", {
      [`${ADR_DIR}/0001-x.md`]: adr({}, "# D\n`PRAXIUM_GONE` ADR-0999\n"),
      "docs/index.md": `${front()}[g](guide.md) [d](design-docs/adr/)\n`,
    });
    expect(messages(root)).toEqual([]);
  });
});
