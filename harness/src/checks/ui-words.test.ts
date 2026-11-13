import path from "node:path";
import { describe, expect, test } from "vitest";
import { findRoot, loadYaml } from "../common";
import { miniRepo } from "../test-support/mini-repo";
import {
  checkText,
  configProblems,
  run,
  visibleText,
  type WordRule,
  type WordsConfig,
} from "./ui-words";

const config = loadYaml<WordsConfig>(path.join(findRoot(), "harness/config/ui-words.yaml"));
const found = (source: string, file = "apps/web/src/ui/pages/x.tsx"): string[] =>
  checkText(file, source, config.rules).map((v) => `${v.line}:${v.message.split('"')[1]}`);

describe("the word list: old words on screen fail with the word to use [REQ-003]", () => {
  test.each([
    ['const a = "이미 돌린 씨앗이에요";', "씨앗"],
    ['const a = "서비스 개체에 저장돼요";', "개체"],
    ['const a = "관계 종류를 골라요";', "관계 종류"],
    ['const a = "매핑하지 못한 라벨·관계";', "라벨·관계"],
    ['const a = "알림 넘기기 처리";', "알림 넘기기"],
    ['const a = "넘긴 일 2번";', "넘긴 일"],
    ['const a = "이 세계를 E2E로 가려면";', "E2E로 가려면"],
    ["const state = { 씨앗_번호: 3 };", "씨앗"],
  ])("%s", (source, word) => {
    expect(found(source).map((f) => f.split(":")[1])).toEqual([word]);
  });

  test("it names the line, the found word and the word to use", () => {
    const [v] = checkText("apps/web/src/a.ts", 'const a = 1;\nconst b = "개체";\n', config.rules);
    expect(v).toMatchObject({ code: "WORD001", path: "apps/web/src/a.ts", line: 2 });
    expect(v?.message).toContain('"개체"');
    expect(v?.fix).toContain('"객체"');
  });

  test.each([
    'const a = "3번 중 1번 합격했어요";',
    'const a = "툴 호출 3번째";',
    'const a = "회사 회색 개회 회의";',
    'const a = "냉방 관계가 없는 랙이에요";',
    'const a = "관계없이 돌아가요";',
    'const a = "객체 종류와 링크 종류";',
    'const a = "3회 실행하고 횟수를 세요";',
    'const a = "시드를 고르세요";',
    'const a = "에스컬레이션 처리";',
    'const a = "escalate 툴을 불러요";',
    'const a = "3 states";',
    "const a = 3; // 3회 횟수 시드 개체 in a comment",
    "/** 시드 in a doc comment */ export const b = 1;",
    'import x from "./시드";',
  ])("%s", (source) => {
    expect(found(source)).toEqual([]);
  });

  test("only text a person can read is scanned: comments, imports and English strings are not", () => {
    const text = visibleText(
      "a.tsx",
      '// 3회\nimport a from "./횟수";\nconst x = "plain";\nconst y = "한 번";\nconst z = <b>글자</b>;\n',
    ).map((p) => p.text.trim());
    expect(text).toEqual(["한 번", "글자"]);
  });

  test("an exception holds for its phrase in its path, and nowhere else", () => {
    const rule: WordRule = {
      id: "t",
      pattern: "횟수",
      use: "몇 번",
      allow: [{ path: "apps/web/src/moss/**", phrase: "횟수", reason: "typed input" }],
    };
    const src = 'const a = ["횟수"];';
    expect(checkText("apps/web/src/moss/r.ts", src, [rule])).toEqual([]);
    expect(checkText("apps/web/src/ui/r.ts", src, [rule])).toHaveLength(1);
    const wide: WordRule = { ...rule, allow: [{ phrase: "돌릴 횟수", reason: "r" }] };
    expect(checkText("a.ts", 'const a = "돌릴 횟수";', [wide])).toEqual([]);
    expect(checkText("a.ts", 'const a = "툴 호출 횟수";', [wide])).toHaveLength(1);
    const files: WordRule = { ...rule, allow: [{ path: "apps/web/src/legacy/**", reason: "r" }] };
    expect(checkText("apps/web/src/legacy/a.ts", src, [files])).toEqual([]);
  });

  test("the word list is checked too: a bad pattern, no word to use, an exception without a reason", () => {
    const bad: WordsConfig = {
      scan_dirs: ["apps/web/src"],
      rules: [
        { id: "a", pattern: "(", use: "x" },
        { id: "b", pattern: "b", use: "" },
        { id: "c", pattern: "c", use: "x", allow: [{ phrase: "c", reason: "" }] },
      ],
    };
    expect(configProblems(bad).map((v) => v.code)).toEqual(["WORD002", "WORD002", "WORD002"]);
    expect(configProblems(config)).toEqual([]);
  });

  test("a whole repo: test files are skipped, source files are not", () => {
    const root = miniRepo({
      "harness/config/ui-words.yaml": [
        "scan_dirs: [apps/web/src]",
        "rules:",
        "  - {id: hoe, pattern: '(?<![가-힣])회(?![가-힣])', use: '번'}",
      ].join("\n"),
      "apps/web/src/ok.ts": 'export const a = "3번";\n',
      "apps/web/src/bad.ts": 'export const a = "3회";\n',
      "apps/web/src/bad.test.ts": 'export const a = "3회";\n',
    });
    expect(run(root).map((v) => `${v.code} ${v.path}:${v.line}`)).toEqual([
      "WORD001 apps/web/src/bad.ts:1",
    ]);
  });
});
