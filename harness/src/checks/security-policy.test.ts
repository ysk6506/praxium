import { describe, expect, test } from "vitest";
import { findRoot, readText } from "../common";

const read = (name: string) => readText(`${findRoot()}/${name}`);

describe("the repository tells how to report a vulnerability [REQ-470]", () => {
  const policy = read("SECURITY.md");

  test("SECURITY.md names the private report of GitHub and says not to use a public issue", () => {
    expect(policy).toContain("Report a vulnerability");
    expect(policy).toContain("공개 이슈로 올리지 마세요");
  });

  test("it asks for the version, the steps to reproduce and the impact, and promises no date", () => {
    for (const word of ["영향받는 버전", "재현 방법", "영향"]) expect(policy).toContain(word);
    expect(policy).not.toMatch(/\d+\s*(일|시간|영업일|주)\s*(이내|안에)/);
  });

  test("it has no e-mail address and stays short", () => {
    expect(policy).not.toMatch(/[\w.+-]+@[\w-]+\.[\w.-]+/);
    expect(policy.trimEnd().split("\n").length).toBeLessThanOrEqual(40);
  });

  test("the README points to it", () => {
    expect(read("README.md")).toContain("(SECURITY.md)");
  });
});
