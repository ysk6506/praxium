import { readFileSync } from "node:fs";
import path from "node:path";
import { describe, expect, test } from "vitest";
import { API_CONFIG, apiRepo } from "../test-support/api-fixture";
import { writeFiles } from "../test-support/mini-repo";
import { checkAssembled, checkKeywords, goKeywords } from "./api-assembled";

const textOf = (root: string): string => readFileSync(path.join(root, API_CONFIG.openapi), "utf8");

describe("the committed contract is what the parts assemble to [REQ-143]", () => {
  test("an assembled document that equals the committed one passes", () => {
    const root = apiRepo();
    expect(checkAssembled(root, API_CONFIG, () => ({ text: textOf(root) }))).toEqual([]);
  });

  test.each([
    ["API020", () => ({ text: "{}\n" })],
    ["API021", () => ({ error: "part x.json: not JSON" })],
  ])("%s", (code, assemble) => {
    const [found] = checkAssembled(apiRepo(), API_CONFIG, assemble);
    expect(found?.code).toBe(code);
    expect(found?.fix.length).toBeGreaterThan(20);
  });

  test("a missing committed document is reported before anything is assembled", () => {
    const root = apiRepo({ [API_CONFIG.openapi]: "" });
    const empty = checkAssembled(root, API_CONFIG, () => ({ text: "" }));
    expect(empty).toEqual([]);
    const gone = checkAssembled("/nonexistent-root", API_CONFIG, () => {
      throw new Error("must not run");
    });
    expect(gone.map((v) => v.code)).toEqual(["API022"]);
  });
});

describe("the schema keywords of the harness and of the Go validator agree [REQ-143]", () => {
  test("the same words pass, in any order", () => {
    expect(checkKeywords(apiRepo(), API_CONFIG)).toEqual([]);
    const root = apiRepo();
    writeFiles(root, {
      [API_CONFIG.schema_keywords_source]:
        'func SchemaKeywords() []string {\n\treturn []string{"example", "description", "enum", "items", "required", "properties", "type", "$ref"}\n}\n',
    });
    expect(checkKeywords(root, API_CONFIG)).toEqual([]);
  });

  test("a different list or a missing function fails", () => {
    const extra = apiRepo({
      [API_CONFIG.schema_keywords_source]:
        'func SchemaKeywords() []string {\n\treturn []string{"$ref", "oneOf"}\n}\n',
    });
    expect(checkKeywords(extra, API_CONFIG).map((v) => v.code)).toEqual(["API091"]);
    const none = apiRepo({ [API_CONFIG.schema_keywords_source]: "package contract\n" });
    expect(checkKeywords(none, API_CONFIG)[0]?.message).toContain("not in the Go validator");
    expect(checkKeywords("/nonexistent-root", API_CONFIG)[0]?.code).toBe("API091");
  });

  test("the words are the literals of the function", () => {
    expect(
      goKeywords('x\nfunc SchemaKeywords() []string {\n\treturn []string{"a", "b"}\n}\n"c"'),
    ).toEqual(["a", "b"]);
    expect(goKeywords("func Other() {}")).toBeNull();
    expect(goKeywords('func SchemaKeywords() []string { return []string{"a"}')).toEqual(["a"]);
  });
});
