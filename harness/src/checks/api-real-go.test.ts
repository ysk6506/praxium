import { mkdirSync, mkdtempSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import path from "node:path";
import { describe, expect, test } from "vitest";
import { API_CONFIG } from "../test-support/api-fixture";
import { defaultIo } from "./api-contract";

// A tiny module that does not import net/http: it prints what the real program prints for the two commands the
// check runs, so the real build-and-run path is exercised cheaply with an empty Go cache.
const PROGRAM = [
  "package main",
  "",
  'import "os"',
  "",
  "func main() {",
  '\tif len(os.Args) > 2 && os.Args[1] == "contract" {',
  '\t\t_, _ = os.Stdout.WriteString("{\\"openapi\\":\\"3.1.0\\"}\\n")',
  "\t\treturn",
  "\t}",
  '\t_, _ = os.Stdout.WriteString("{\\"routes\\":[{\\"method\\":\\"GET\\",\\"path\\":\\"/v1/things\\",\\"roles\\":[\\"read\\"]}]}")',
  "}",
  "",
].join("\n");

function tinyBackend(): string {
  const root = mkdtempSync(path.join(tmpdir(), "praxium-tiny-"));
  mkdirSync(path.join(root, "backend/cmd/praxium"), { recursive: true });
  writeFileSync(path.join(root, "backend/go.mod"), "module tiny\n\ngo 1.24\n");
  writeFileSync(path.join(root, "backend/cmd/praxium/main.go"), PROGRAM);
  return root;
}

describe("the default io builds and runs the real program [REQ-143]", () => {
  test("assemble and routes come from one build of a small module", () => {
    const made = defaultIo(tinyBackend(), API_CONFIG, undefined);
    try {
      expect(made.assemble()).toEqual({ text: '{"openapi":"3.1.0"}\n' });
      expect(made.listRoutes()).toEqual({
        routes: [{ method: "GET", path: "/v1/things", roles: ["read"] }],
      });
    } finally {
      made.dispose?.();
    }
  }, 20_000);
});
