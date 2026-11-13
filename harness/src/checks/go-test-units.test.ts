import { describe, expect, test } from "vitest";
import { goLines, goRepo } from "../test-support/go-repo";
import { testIntegrity } from "./go";
import { loadGoConfig } from "./go-config";
import {
  assertionViolations,
  type GoTestInfo,
  goCitations,
  goTestInfos,
  goTestUnits,
  strayTagViolations,
} from "./go-test-units";

const PKG = "backend/internal/model";
const src = (...rows: string[]) => goLines("package model", 'import "testing"', ...rows);
const infosOf = (files: Record<string, string>): GoTestInfo[] => goTestInfos(goRepo(files));
const find = (infos: readonly GoTestInfo[], name: string): GoTestInfo => {
  const found = infos.find((i) => i.name === name);
  if (!found) throw new Error(`no test ${name}`);
  return found;
};
const digestOf = (files: Record<string, string>, name: string): string =>
  find(infosOf(files), name).digest;

const TWO = src(
  'const golden = "abc"',
  "func wanted() string { return golden }",
  "func TestUses(t *testing.T) {",
  '\tif wanted() != "abc" {',
  '\t\tt.Fatal("differs")',
  "\t}",
  "}",
  "func TestAlone(t *testing.T) {",
  "\tif 1+1 != 2 {",
  '\t\tt.Error("arithmetic")',
  "\t}",
  "}",
);
const FILE = `${PKG}/a_test.go`;
const withA = (text: string) => ({ [FILE]: text });

describe("Go test units: which functions are tests, and how they are named [REQ-023]", () => {
  test("Test, Fuzz and Example functions are units named by package folder and function", () => {
    const text = src(
      "func TestOne(t *testing.T) { t.Fatal(1) }",
      "func FuzzTwo(f *testing.F) { f.Fuzz(func(t *testing.T, s string) { t.Error(s) }) }",
      "func ExampleThree() {\n\t// Output: 3\n}",
      "func Example_four() {\n\t// Output: 4\n}",
      "func BenchmarkFive(b *testing.B) { b.Fatal(5) }",
      "func TestMain(m *testing.M) { m.Run() }",
      "func Testify(t *testing.T) { t.Fatal(6) }",
      "func (s *thing) TestMethod(t *testing.T) { t.Fatal(7) }",
      "type thing struct{}",
      "func helper(t *testing.T) { t.Fatal(8) }",
    );
    const keys = infosOf(withA(text))
      .map((i) => i.key)
      .filter((k) => k.startsWith(`go:${PKG}::`) && !k.endsWith("TestDouble"));
    expect(keys).toEqual([
      `go:${PKG}::TestOne`,
      `go:${PKG}::FuzzTwo`,
      `go:${PKG}::ExampleThree`,
      `go:${PKG}::Example_four`,
    ]);
  });

  test("the lock units carry the key, the file and the line of the function", () => {
    const unit = goTestUnits(goRepo(withA(TWO))).find((u) => u.key === `go:${PKG}::TestAlone`);
    expect(unit).toMatchObject({ kind: "test", path: FILE, asserts: 1, skips: 0, precision: [] });
    expect(unit?.line).toBe(10);
    expect(unit?.literals).toHaveLength(1);
  });

  test("a repository without Go code has no Go tests", () => {
    expect(goTestInfos(goRepo({ "harness/config/architecture.yaml": "units: {}\n" }))).toEqual([]);
  });

  test("moving a test to another file of the package changes nothing, two with one name are told apart", () => {
    const test_ = "func TestMoves(t *testing.T) {\n\tt.Fatal(1)\n}\n";
    const here = digestOf(withA(src(test_)), "TestMoves");
    expect(digestOf({ [`${PKG}/z_test.go`]: src(test_) }, "TestMoves")).toBe(here);
    const keys = infosOf({
      [`${PKG}/b_test.go`]: src(test_),
      [`${PKG}/c_test.go`]: src(test_).replace("package model", "package model_test"),
    })
      .map((i) => i.key)
      .filter((k) => k.includes("TestMoves"));
    expect(keys).toEqual([`go:${PKG}::TestMoves`, `go:${PKG}::TestMoves#2`]);
  });
});

describe("Go test units: the fingerprint [REQ-023]", () => {
  test("layout, comments and trailing commas do not change it", () => {
    const messy = TWO.replace(
      "func TestAlone(t *testing.T) {",
      "// A comment.\nfunc  TestAlone( t *testing.T )   {\n\n\t// inside",
    ).replace('t.Error("arithmetic")', 't.Error( "arithmetic" , )');
    expect(digestOf(withA(messy), "TestAlone")).toBe(digestOf(withA(TWO), "TestAlone"));
    expect(digestOf(withA(messy), "TestUses")).toBe(digestOf(withA(TWO), "TestUses"));
  });

  test("a changed body changes it, and only for that test", () => {
    const changed = TWO.replace('t.Error("arithmetic")', 't.Error("other words")');
    expect(digestOf(withA(changed), "TestAlone")).not.toBe(digestOf(withA(TWO), "TestAlone"));
    expect(digestOf(withA(changed), "TestUses")).toBe(digestOf(withA(TWO), "TestUses"));
  });

  test("a golden constant, a helper and a table it reaches by name are part of the test", () => {
    const changed = TWO.replace('"abc"', '"abd"');
    expect(digestOf(withA(changed), "TestUses")).not.toBe(digestOf(withA(TWO), "TestUses"));
    expect(digestOf(withA(changed), "TestAlone")).toBe(digestOf(withA(TWO), "TestAlone"));
    const helper = TWO.replace("return golden", "return golden + golden");
    expect(digestOf(withA(helper), "TestUses")).not.toBe(digestOf(withA(TWO), "TestUses"));
    const table = src(
      "var rows = []struct{ in, out int }{{1, 2}, {2, 4}}",
      "func TestTable(t *testing.T) {",
      "\tfor _, r := range rows {",
      "\t\tif r.in*2 != r.out {",
      '\t\t\tt.Fatal("row")',
      "\t\t}",
      "\t}",
      "}",
    );
    expect(digestOf(withA(table.replace("{2, 4}", "{2, 5}")), "TestTable")).not.toBe(
      digestOf(withA(table), "TestTable"),
    );
  });

  test("what a grouped variable or a type's methods hold is reached by name, the rest of the group is not", () => {
    const text = src(
      "var (",
      "\tgoldA = 1",
      "\tgoldB = 2",
      ")",
      "type fake struct{}",
      "func (fake) Want() int { return 7 }",
      "func TestA(t *testing.T) {",
      "\tif goldA != 1 || (fake{}).Want() != 7 {",
      '\t\tt.Fatal("a")',
      "\t}",
      "}",
    );
    const base = digestOf(withA(text), "TestA");
    expect(digestOf(withA(text.replace("goldB = 2", "goldB = 3")), "TestA")).toBe(base);
    expect(digestOf(withA(text.replace("goldA = 1", "goldA = 5")), "TestA")).not.toBe(base);
    expect(digestOf(withA(text.replace("return 7", "return 8")), "TestA")).not.toBe(base);
  });

  test("a helper reached through another helper counts; one nobody reaches does not", () => {
    const text = src(
      "const deep = 1",
      "const unused = 9",
      "func inner() int { return deep }",
      "func outer() int { return inner() }",
      "func TestA(t *testing.T) { t.Fatal(outer()) }",
    );
    const base = digestOf(withA(text), "TestA");
    expect(digestOf(withA(text.replace("deep = 1", "deep = 2")), "TestA")).not.toBe(base);
    expect(digestOf(withA(text.replace("unused = 9", "unused = 8")), "TestA")).toBe(base);
  });

  test("package hooks and blank variables reach every test", () => {
    const text = src(
      "var state = 1",
      "func init() { state = 2 }",
      "func TestA(t *testing.T) { t.Fatal(1) }",
    );
    const base = digestOf(withA(text), "TestA");
    expect(digestOf(withA(text.replace("state = 2", "state = 3")), "TestA")).not.toBe(base);
    const blank = src(
      "var _ = setup()",
      "func setup() int { return 1 }",
      "func TestA(t *testing.T) { t.Fatal(1) }",
    );
    expect(digestOf(withA(blank.replace("return 1", "return 2")), "TestA")).not.toBe(
      digestOf(withA(blank), "TestA"),
    );
  });

  test("a member name reaches a method, a selector does not reach a function of the same name", () => {
    const text = src(
      "type box struct{}",
      "func (box) check() int { return 1 }",
      "func check() int { return 5 }",
      "func TestA(t *testing.T) {",
      "\tvar b box",
      "\tt.Fatal(b.check())",
      "}",
    );
    const base = digestOf(withA(text), "TestA");
    expect(digestOf(withA(text.replace("return 1", "return 2")), "TestA")).not.toBe(base);
    expect(digestOf(withA(text.replace("return 5", "return 6")), "TestA")).toBe(base);
  });

  test("a helper in another test file of the package is reached too", () => {
    const helper = (n: number) => ({
      [`${PKG}/b_test.go`]: src(`func shared() int { return ${n} }`),
    });
    const user = src("func TestA(t *testing.T) { t.Fatal(shared()) }");
    const one = infosOf({ ...withA(user), ...helper(1) });
    const two = infosOf({ ...withA(user), ...helper(2) });
    expect(find(one, "TestA").digest).not.toBe(find(two, "TestA").digest);
  });

  test("a golden file under testdata is part of the test that names it", () => {
    const text = src(
      "func TestReads(t *testing.T) {",
      '\tif readFile("testdata/want.txt") != "x" {',
      '\t\tt.Fatal("differs")',
      "\t}",
      "}",
      'func readFile(string) string { return "x" }',
      "func TestOther(t *testing.T) { t.Fatal(1) }",
    );
    const files = (golden: string) => ({
      ...withA(text),
      [`${PKG}/testdata/want.txt`]: golden,
      [`${PKG}/testdata/other.txt`]: "unrelated",
    });
    expect(digestOf(files("1"), "TestReads")).not.toBe(digestOf(files("2"), "TestReads"));
    expect(digestOf(files("1"), "TestOther")).toBe(digestOf(files("2"), "TestOther"));
    const dir = text.replace("testdata/want.txt", "testdata");
    const whole = (other: string) => ({
      ...files("1"),
      ...withA(dir),
      [`${PKG}/testdata/other.txt`]: other,
    });
    expect(digestOf(whole("a"), "TestReads")).not.toBe(digestOf(whole("b"), "TestReads"));
  });

  test("a go:embed file of a variable the test reaches is part of the test", () => {
    const text = src(
      'import _ "embed"',
      "//go:embed testdata/*.json",
      "var golden string",
      "func TestA(t *testing.T) { t.Fatal(golden) }",
    );
    const files = (n: string) => ({ ...withA(text), [`${PKG}/testdata/x.json`]: n });
    expect(digestOf(files("1"), "TestA")).not.toBe(digestOf(files("2"), "TestA"));
  });

  test("a path that leaves the repository or does not exist is not followed", () => {
    const text = (path: string) =>
      src(`func TestA(t *testing.T) { t.Fatal("${path}", "testdata/missing") }`);
    const inside = "../../../outside/testdata/x";
    const files = (n: string) => ({ ...withA(text(inside)), "outside/testdata/x": n });
    expect(digestOf(files("1"), "TestA")).not.toBe(digestOf(files("2"), "TestA"));
    const escaping = "../../../../../elsewhere/testdata/x";
    expect(digestOf(withA(text(escaping)), "TestA")).toBe(digestOf(withA(text(escaping)), "TestA"));
  });

  test("the requirement tags are part of the fingerprint", () => {
    const tagged = src("// [REQ-001]", "func TestA(t *testing.T) { t.Fatal(1) }");
    const none = src("func TestA(t *testing.T) { t.Fatal(1) }");
    expect(digestOf(withA(tagged), "TestA")).not.toBe(digestOf(withA(none), "TestA"));
  });
});

describe("Go test units: what a weakening would change [REQ-023]", () => {
  const one = (...rows: string[]): GoTestInfo =>
    find(infosOf(withA(src("func TestA(t *testing.T) {", ...rows, "}"))), "TestA");

  test("calls that can fail the test are counted, an error's own Error method is not", () => {
    expect(
      one(
        "\tif false {",
        "\t\tt.Error(1)",
        '\t\tt.Errorf("x")',
        "\t\tt.Fatal(2)",
        '\t\tt.Fatalf("y")',
        "\t\tt.Fail()",
        "\t\tt.FailNow()",
        "\t}",
      ).asserts,
    ).toBe(6);
    expect(one("\tvar err error", "\t_ = err.Error()", "\tt.Log(1)").asserts).toBe(0);
  });

  test("calls inside a subtest count with their own handle name", () => {
    expect(one('\tt.Run("sub", func(sub *testing.T) {', "\t\tsub.Fatal(1)", "\t})").asserts).toBe(
      1,
    );
  });

  test("a helper that fails the test counts for each place the test uses it, through other helpers too", () => {
    const text = src(
      "func must(t *testing.T, ok bool) { if !ok { t.Fatal(1) } }",
      "func mustAll(t *testing.T) { must(t, true) }",
      "func TestA(t *testing.T) {",
      "\tmust(t, true)",
      "\tmustAll(t)",
      "}",
      "func TestB(t *testing.T) { t.Log(1) }",
    );
    const infos = infosOf(withA(text));
    expect(find(infos, "TestA").asserts).toBe(2);
    expect(find(infos, "TestB").asserts).toBe(0);
  });

  test("an Example asserts through its Output comment", () => {
    const text = src("func ExampleA() {\n\t// Output: 1\n}", "func ExampleB() {\n\t_ = 1\n}");
    const infos = infosOf(withA(text));
    expect(find(infos, "ExampleA").asserts).toBe(1);
    expect(find(infos, "ExampleB").asserts).toBe(0);
  });

  test("skips and the testing.Short gate are counted, in the test and in what it reaches", () => {
    expect(one('\tt.Skip("x")', "\tt.SkipNow()", '\tt.Skipf("y")').skips).toBe(3);
    expect(one("\tif testing.Short() {", "\t\treturn", "\t}").skips).toBe(1);
    const text = src(
      'func guard(t *testing.T) { t.Skip("x") }',
      "func TestA(t *testing.T) { guard(t); t.Fatal(1) }",
    );
    expect(find(infosOf(withA(text)), "TestA").skips).toBe(1);
  });

  test("the literals digest follows every literal the test reaches", () => {
    const text = TWO;
    const base = find(infosOf(withA(text)), "TestUses").literals;
    expect(find(infosOf(withA(text.replace('"abc"', '"abd"'))), "TestUses").literals).not.toBe(
      base,
    );
    expect(find(infosOf(withA(text.replace('"abc"', '"abd"'))), "TestAlone").literals).toBe(
      find(infosOf(withA(text)), "TestAlone").literals,
    );
    const renamed = text.replaceAll("wanted", "wantedToo");
    expect(find(infosOf(withA(renamed)), "TestUses").literals).toBe(base);
  });
});

describe("Go test units: requirement tags [REQ-025]", () => {
  const tagsOf = (text: string) => find(infosOf(withA(text)), "TestA").reqs;

  test("the doc comment above the function carries one tag per bracket", () => {
    expect(
      tagsOf(src("// [REQ-002] [REQ-001]", "func TestA(t *testing.T) { t.Fatal(1) }")),
    ).toEqual(["REQ-001", "REQ-002"]);
    expect(
      tagsOf(
        src(
          "// Explains the test.",
          "// [REQ-003] and why",
          "func TestA(t *testing.T) { t.Fatal(1) }",
        ),
      ),
    ).toEqual(["REQ-003"]);
  });

  test("subtest names and table row names inside the test carry tags too", () => {
    const text = src(
      "func TestA(t *testing.T) {",
      '\tt.Run("[REQ-004] first", func(t *testing.T) { t.Fatal(1) })',
      '\trows := []struct{ name string }{{"[REQ-005] second"}, {"[REQ-004] again"}}',
      "\t_ = rows",
      "}",
    );
    expect(tagsOf(text)).toEqual(["REQ-004", "REQ-005"]);
  });

  test("a comment inside the body, a list in one bracket and a tag far above are no tags", () => {
    expect(
      tagsOf(src("func TestA(t *testing.T) {\n\t// [REQ-001] inside\n\tt.Fatal(1)\n}")),
    ).toEqual([]);
    expect(tagsOf(src("// [REQ-001, REQ-002]", "func TestA(t *testing.T) { t.Fatal(1) }"))).toEqual(
      [],
    );
    expect(tagsOf(src("// [REQ-001]", "", "func TestA(t *testing.T) { t.Fatal(1) }"))).toEqual([]);
  });

  test("the citations list every tag with the file and line of its test", () => {
    const root = goRepo(withA(src("// [REQ-001]", "func TestA(t *testing.T) { t.Fatal(1) }")));
    expect(goCitations(root)).toEqual([["REQ-001", FILE, 4]]);
  });

  test("a tag in a comment that links no test is TRACE007", () => {
    const text = src(
      "// [REQ-001] on a helper",
      "func helper() {}",
      "// [REQ-002, REQ-003] a list",
      "func TestA(t *testing.T) {",
      "\t// [REQ-004] inside the body",
      "\tt.Fatal(1)",
      "}",
      "// [REQ-005]",
      "func TestB(t *testing.T) { t.Fatal(1) }",
    );
    const root = goRepo(withA(text));
    const config = loadGoConfig(root);
    if (!config) throw new Error("no go config");
    const lines = strayTagViolations(root, config).map((v) => `${v.code}:${v.line}`);
    expect(lines).toEqual(["TRACE007:3", "TRACE007:5", "TRACE007:7"]);
  });
});

describe("Go test units: a test that cannot fail is GOTEST003 [REQ-023]", () => {
  test("only functions that cannot fail the test are reported", () => {
    const text = src(
      "func TestNothing(t *testing.T) { t.Log(1) }",
      "func TestFine(t *testing.T) { t.Fatal(1) }",
      "func ExampleNothing() {}",
    );
    const root = goRepo({
      ...withA(text),
      "backend/cmd/app/main_test.go": src("func TestRuns(t *testing.T) { t.Error(1) }").replace(
        "package model",
        "package main",
      ),
    });
    const config = loadGoConfig(root);
    if (!config) throw new Error("no go config");
    const found = assertionViolations(root, config);
    expect(found.map((v) => v.code)).toEqual(["GOTEST003", "GOTEST003"]);
    expect(found.map((v) => v.message)).toEqual([
      "Go test TestNothing asserts nothing",
      "Go test ExampleNothing asserts nothing",
    ]);
    expect(testIntegrity(root).map((v) => v.code)).toEqual(["GOTEST003", "GOTEST003"]);
  });

  test("a repository whose tests all assert is clean", () => {
    const asserting =
      'package main\nimport "testing"\nfunc TestRuns(t *testing.T) { t.Error(1) }\n';
    expect(testIntegrity(goRepo({ "backend/cmd/app/main_test.go": asserting }))).toEqual([]);
  });

  test("a test file that declares nothing adds no unit", () => {
    const count = (files: Record<string, string>) => goTestInfos(goRepo(files)).length;
    expect(count({ [`${PKG}/empty_test.go`]: "package model\n" })).toBe(count({}));
  });
});
