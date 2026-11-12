import { describe, expect, test } from "vitest";
import { josa, withJosa } from ".";

describe("Korean particles [REQ-002]", () => {
  test("Hangul words take the particle that matches their final consonant", () => {
    expect(withJosa("냉방기", "은")).toBe("냉방기는");
    expect(withJosa("분전반", "는")).toBe("분전반은");
    expect(withJosa("서버", "이")).toBe("서버가");
    expect(withJosa("랙", "가")).toBe("랙이");
    expect(withJosa("스위치", "을")).toBe("스위치를");
    expect(withJosa("층", "를")).toBe("층을");
    expect(withJosa("전원", "와")).toBe("전원과");
  });

  test("(으)로 drops 으 after ㄹ as well as after a vowel", () => {
    expect(withJosa("서울", "으로")).toBe("서울로");
    expect(withJosa("서버", "으로")).toBe("서버로");
    expect(withJosa("건물", "로")).toBe("건물로");
    expect(withJosa("분전반", "로")).toBe("분전반으로");
    expect(withJosa("crah-1", "으로")).toBe("crah-1로");
    expect(withJosa("srv-3", "로")).toBe("srv-3으로");
  });

  test("ids, digits and Latin letters are judged by how they are read aloud", () => {
    expect(withJosa("crah-1", "은")).toBe("crah-1은");
    expect(withJosa("srv-a2", "은")).toBe("srv-a2는");
    expect(withJosa("PDU", "이")).toBe("PDU가");
    expect(withJosa("vm-web", "을")).toBe("vm-web을");
    expect(withJosa("panel", "이")).toBe("panel이");
    expect(withJosa("UPS", "은")).toBe("UPS는");
    expect(withJosa("10", "이")).toBe("10이");
  });

  test("when the ending cannot be judged, both forms are shown", () => {
    expect(josa("(?)", "은")).toBe("은(는)");
    expect(josa("", "이")).toBe("이(가)");
    expect(josa("✓", "으로")).toBe("으로(로)");
  });
});

describe("Korean particles after English words [REQ-002]", () => {
  test("acronyms are read letter by letter, words as words", () => {
    expect(withJosa("html", "을")).toBe("html을");
    expect(withJosa("HTML", "을")).toBe("HTML을");
    expect(withJosa("rack", "이")).toBe("rack이");
    expect(withJosa("server", "이")).toBe("server가");
    expect(withJosa("host", "은")).toBe("host는");
    expect(withJosa("rack-a", "은")).toBe("rack-a는");
  });
});
