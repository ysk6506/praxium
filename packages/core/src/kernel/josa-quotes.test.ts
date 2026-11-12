import { describe, expect, test } from "vitest";
import { josa, withJosa } from ".";

describe("particles after quoted names [REQ-002]", () => {
  test("closing quotes and brackets are not read aloud", () => {
    expect(withJosa("하네스 'crah-1'", "을")).toBe("하네스 'crah-1'을");
    expect(withJosa("'새벽동 B2'", "을")).toBe("'새벽동 B2'를");
    expect(josa("(crah-1)", "이")).toBe("이");
    expect(josa("「분전반」", "은")).toBe("은");
    expect(josa("`get_entity`", "이")).toBe("가");
    expect(josa("''", "은")).toBe("은(는)");
  });
});
