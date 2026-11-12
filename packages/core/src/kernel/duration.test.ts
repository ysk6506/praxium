import { describe, expect, test } from "vitest";
import { formatDuration } from ".";

describe("durations [REQ-002]", () => {
  test("durations read at a glance", () => {
    expect([0, 45, 60, 750, 3600, 3900, 3929, -5, 59.6].map(formatDuration)).toEqual([
      "0초",
      "45초",
      "1분",
      "12분 30초",
      "1시간",
      "1시간 5분",
      "1시간 5분",
      "0초",
      "1분",
    ]);
  });
});
