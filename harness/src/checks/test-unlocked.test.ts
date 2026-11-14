import { describe, expect, test } from "vitest";
import { miniRepo } from "../test-support/mini-repo";
import { lockedTestFiles, testFiles } from "./test-units";

describe("test files that are not locked [REQ-279]", () => {
  const files = {
    "apps/web/src/ui/a.test.tsx": "export {};\n",
    "apps/web/src/screens/b.test.tsx": "export {};\n",
  };
  const globs = {
    test_globs: ["apps/**/*.test.tsx"],
    support_globs: [],
    data_globs: [],
  };

  test("unlocked_globs keep a file out of the lock and the trace, not out of the test files", () => {
    const root = miniRepo(files, {});
    const unlocked = { ...globs, unlocked_globs: ["apps/web/src/screens/**"] };
    expect(testFiles(root, unlocked)).toEqual([
      "apps/web/src/screens/b.test.tsx",
      "apps/web/src/ui/a.test.tsx",
    ]);
    expect(lockedTestFiles(root, unlocked)).toEqual(["apps/web/src/ui/a.test.tsx"]);
  });

  test("without unlocked_globs every test file is locked", () => {
    const root = miniRepo(files, {});
    expect(lockedTestFiles(root, globs)).toHaveLength(2);
  });
});
