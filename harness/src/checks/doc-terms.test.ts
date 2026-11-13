import path from "node:path";
import { describe, expect, test } from "vitest";
import { findRoot, loadYaml } from "../common";
import { miniRepo } from "../test-support/mini-repo";
import {
  type DocumentsConfig,
  dictionaryProblems,
  documentsReport,
  findOld,
  parseDictionary,
  perDocument,
  proseLines,
  scanDocuments,
} from "./doc-terms";
import { configProblems, report, type WordsConfig } from "./ui-words";

const GLOSSARY = [
  "# 용어집",
  "",
  "| 표준 용어 | 옛 표기(쓰지 않음) | 뜻 | 영어 대응 |",
  "|---|---|---|---|",
  "| **테스트** | 시험, 시험 파일 | 확인하는 일 | test |",
  "| **페이지네이션** | 쪽 넘김 | 나눠 돌려주기 | pagination |",
  "| **마스터 키** | 으뜸 열쇠 | 키를 감싸는 키 | master key |",
  "| **개요** | | 요약 절 | overview |",
  "",
  "| 옛 표기 | 문맥 | 표준 용어 |",
  "|---|---|---|",
  "| 자리 | 여러 뜻 | 뜻마다 다름 |",
].join("\n");

const CONFIG: DocumentsConfig = {
  mode: "report",
  scan: ["docs/**/*.md", "README.md"],
  skip: ["docs/references/old.md"],
  dictionary: ["docs/glossary.md"],
  ambiguous_words: ["자리", "길"],
  keep: ["공급망"],
};

const terms = parseDictionary("docs/glossary.md", GLOSSARY);
const old = (text: string): string[] => findOld("d.md", text, terms).map((h) => h.term.old);

describe("the dictionary is the glossary tables [REQ-003]", () => {
  test("old spellings come from the second column of the glossary tables only", () => {
    expect(terms.map((t) => `${t.old}>${t.standard}`)).toEqual([
      "시험>테스트",
      "시험 파일>테스트",
      "쪽 넘김>페이지네이션",
      "으뜸 열쇠>마스터 키",
    ]);
  });

  test("a table of another shape is not read, and a standard term is never an entry", () => {
    expect(terms.some((t) => t.old === "자리")).toBe(false);
    expect(dictionaryProblems(terms, CONFIG)).toEqual([]);
  });

  test("an everyday word, a spelling that is also a standard term and two answers for one spelling are refused", () => {
    const bad = [
      { old: "자리", standard: "위치", source: "g.md" },
      { old: "테스트", standard: "검사", source: "g.md" },
      { old: "시험", standard: "테스트", source: "g.md" },
      { old: "시험", standard: "검증", source: "g.md" },
    ];
    expect(dictionaryProblems(bad, CONFIG).map((v) => v.code)).toEqual([
      "WORD004",
      "WORD004",
      "WORD004",
    ]);
  });
});

describe("only prose is read [REQ-003]", () => {
  test.each([
    ["시험 결과가 나왔다.", ["시험"]],
    ["쪽 넘김과 으뜸 열쇠", ["쪽 넘김", "으뜸 열쇠"]],
    ["시험 파일 하나", ["시험 파일"]],
    ["| 표 | 시험 |", ["시험"]],
    ["# 시험 절", ["시험"]],
  ])("%s", (text, expected) => {
    expect(old(text)).toEqual(expected);
  });

  test.each([
    "`시험` 은 코드",
    "``시험 `x` 시험``",
    "[링크](docs/시험.md)",
    "https://example.com/시험",
    "> 인용 속의 시험",
    "<!-- 시험 -->",
    "읽는 자리와 길은 뜻이 여럿이라 적지 않는다",
    "테스트와 개요",
  ])("%s", (text) => {
    expect(old(text)).toEqual([]);
  });

  test("code blocks, comments over several lines and front matter keys are skipped; title and reasons are read", () => {
    const text = [
      "---",
      "title: 시험 제목",
      "covers:",
      "  - docs/시험.md",
      "updates_docs_reason: 시험 때문이다",
      "---",
      "```",
      "시험",
      "```",
      "<!--",
      "시험",
      "-->",
      "본문 시험",
    ].join("\n");
    expect(findOld("d.md", text, terms).map((h) => h.line)).toEqual([2, 5, 13]);
  });

  test("a standard word that holds an old spelling can be kept", () => {
    const t = [{ old: "공급", standard: "피드", source: "g.md" }];
    expect(findOld("d.md", "공급망과 공급", t, false, []).length).toBe(2);
    expect(findOld("d.md", "공급망과 공급", t, false, ["공급망"]).length).toBe(1);
  });

  test("the glossary's own old-spelling column and the list of everyday words are not read", () => {
    expect(findOld("g.md", GLOSSARY, terms, true)).toEqual([]);
    expect(proseLines(GLOSSARY, true).join("\n")).not.toContain("자리");
  });

  test("the longest spelling wins and each place counts once", () => {
    expect(old("시험 파일과 시험")).toEqual(["시험 파일", "시험"]);
  });
});

describe("a whole repository: report mode counts, fail mode fails [REQ-003]", () => {
  const files = {
    "docs/glossary.md": GLOSSARY,
    "docs/a.md": "시험 하나\n시험 둘\n",
    "docs/b.md": "시험 하나\n",
    "docs/c.md": "깨끗하다\n",
    "docs/references/old.md": "시험\n",
    "README.md": "으뜸 열쇠\n",
    "src/x.md": "시험\n",
  };

  test("the documents are counted per document, worst first; skipped and unlisted files are left out", () => {
    const root = miniRepo(files);
    const { hits } = scanDocuments(root, CONFIG);
    expect(perDocument(hits)).toEqual([
      ["docs/a.md", 2],
      ["docs/b.md", 1],
      ["README.md", 1],
    ]);
  });

  test("report mode gives a notice and no failure; fail mode gives one WORD003 per place", () => {
    const root = miniRepo(files);
    const report = documentsReport(root, CONFIG);
    expect(report.violations).toEqual([]);
    expect(report.notes[0]).toContain("옛 표기 4곳(문서 3개, 보고 모드)");
    const failing = documentsReport(root, { ...CONFIG, mode: "fail" });
    expect(failing.notes).toEqual([]);
    expect(failing.violations.map((v) => `${v.code} ${v.path}:${v.line}`)).toEqual([
      "WORD003 README.md:1",
      "WORD003 docs/a.md:1",
      "WORD003 docs/a.md:2",
      "WORD003 docs/b.md:1",
    ]);
    expect(failing.violations[1]?.fix).toContain('"테스트"');
  });

  test("a glossary without a dictionary table is a WORD004 failure", () => {
    const root = miniRepo({ ...files, "docs/glossary.md": "# 용어집\n" });
    expect(documentsReport(root, CONFIG).violations.map((v) => v.code)).toEqual(["WORD004"]);
  });

  test("the config is checked: an unknown mode and a missing dictionary", () => {
    const base: WordsConfig = { scan_dirs: ["apps"], rules: [] };
    expect(
      configProblems({ ...base, documents: { ...CONFIG, mode: "warn" as never } }),
    ).toHaveLength(1);
    expect(configProblems({ ...base, documents: { ...CONFIG, dictionary: [] } })).toHaveLength(1);
    expect(configProblems({ ...base, documents: CONFIG })).toEqual([]);
  });

  test("the real repository: the glossary parses, the config is sound and the check reports", () => {
    const root = findRoot();
    const config = loadYaml<WordsConfig>(path.join(root, "harness/config/ui-words.yaml"));
    expect(config.documents?.dictionary).toContain("docs/design-docs/glossary.md");
    const docs = config.documents as DocumentsConfig;
    expect(docs.ambiguous_words).toEqual(
      expect.arrayContaining(["길", "묶음", "자리", "칸", "쪽", "뜻"]),
    );
    const { hits, problems } = scanDocuments(root, docs);
    expect(problems).toEqual([]);
    expect(hits.some((h) => h.file.includes("glossary"))).toBe(false);
    expect(report(root).violations.filter((v) => v.code === "WORD004")).toEqual([]);
  });
});
