import { describe, expect, it } from "vitest";
import type { ImpactData } from "../api";
import { cappedImpact, smallImpact } from "../test-support/impact-world";
import { serverCounts, singlePoints, totalCount, unfilledCount } from "./impact-counts";
import {
  channelName,
  impactNotices,
  type NoticeFacts,
  nextItemLimit,
  warningText,
} from "./impact-notices";
import { expectedItems } from "./useImpact";

const asData = (sample: unknown): ImpactData => sample as ImpactData;

const facts = (
  over: Partial<NoticeFacts> = {},
  computation: Partial<NoticeFacts["computation"]> = {},
): NoticeFacts => ({
  computation: {
    complete: true,
    items_over_cap: false,
    max_hops: 0,
    max_items: 2000,
    stopped_by: null,
    time_budget_seconds: 30,
    weak_links: 0,
    stale_inputs: 0,
    ...computation,
  },
  warnings: [],
  reading: "facts_only",
  unfilled: undefined,
  stopped: false,
  received: 100,
  expected: 100,
  ...over,
});

const ids = (f: NoticeFacts): string[] => impactNotices(f).map((n) => n.id);

describe("[REQ-379] a result that is cut short says why, in a way that tells the causes apart", () => {
  it("has nothing to say about a complete answer without assumptions", () => {
    expect(impactNotices(facts())).toEqual([]);
  });

  it("tells the item limit from the time limit, the hop limit and a stop by the person", () => {
    const over = impactNotices(facts({}, { items_over_cap: true, max_items: 2000 }));
    expect(over[0]).toMatchObject({ id: "items_over_cap", tone: "warn" });
    expect(over[0]?.action).toEqual({
      kind: "raise",
      label: "한도를 5000개로 올려 다시 묻기",
      max: 5000,
    });

    const time = impactNotices(facts({}, { stopped_by: "time_budget", complete: false }));
    expect(time[0]).toMatchObject({ id: "time_budget" });
    expect(time[0]?.text).toContain("30초");

    const hops = impactNotices(facts({}, { stopped_by: "max_hops", max_hops: 3, complete: false }));
    expect(hops[0]).toMatchObject({ id: "max_hops" });
    expect(hops[0]?.text).toContain("3단계");
    expect(hops[0]?.action).toMatchObject({ kind: "unlimited-hops" });

    const stopped = impactNotices(facts({ stopped: true, received: 40, expected: 100 }));
    expect(stopped[0]).toMatchObject({ id: "stopped" });
    expect(stopped[0]?.text).toContain("100개 중 40개");
    expect(stopped[0]?.action).toMatchObject({ kind: "again" });
  });

  it("offers no higher limit when the highest was asked for already", () => {
    const notices = impactNotices(facts({}, { items_over_cap: true, max_items: 20000 }));
    expect(notices[0]?.action).toBeUndefined();
    expect(nextItemLimit(500)).toBe(2000);
    expect(nextItemLimit(2000)).toBe(5000);
    expect(nextItemLimit(20000)).toBeUndefined();
  });

  it("puts what cuts the result short before what it rests on", () => {
    const all = ids(
      facts(
        { reading: "with_assumptions", unfilled: 3, warnings: ["capacity_group_unknown"] },
        { items_over_cap: true, stale_inputs: 2 },
      ),
    );
    expect(all).toEqual([
      "items_over_cap",
      "unfilled",
      "assumptions",
      "stale",
      "warning:capacity_group_unknown",
    ]);
  });

  it("is made from the capped samples of the test world", () => {
    for (const by of ["items", "time_budget", "max_hops"] as const) {
      const data = asData(cappedImpact(by));
      expect(impactNotices(facts({}, data.computation))[0]?.id).toBe(
        by === "items" ? "items_over_cap" : by,
      );
    }
  });
});

describe("[REQ-379] what the calculation assumed and what the data lacks are told", () => {
  it("says the reading includes assumptions, or how many weak links the facts-only reading left out", () => {
    expect(impactNotices(facts({ reading: "with_assumptions" }))[0]?.title).toContain(
      "가정을 포함",
    );
    const weak = impactNotices(facts({}, { weak_links: 250 }));
    expect(weak[0]?.title).toContain("사실만");
    expect(weak[0]?.text).toContain("250개");
  });

  it("counts the objects whose needed channel has no supplier, and says nothing when there are none", () => {
    expect(ids(facts({ unfilled: 0 }))).toEqual([]);
    const notice = impactNotices(facts({ unfilled: 711 }))[0];
    expect(notice?.title).toContain("711개");
    expect(notice?.text).toContain("공급 링크");
  });

  it("says the notes of the server as sentences, and an unknown note as it came", () => {
    expect(warningText("stale_observation_not_used")).toContain("관측값");
    expect(warningText("capacity_group_unknown")).toContain("용량");
    expect(warningText("something_new")).toBe("something_new");
    expect(channelName("power")).toBe("전원");
    expect(channelName("gravity")).toBe("gravity");
  });
});

describe("[REQ-377] the counts are the server's, over the whole answer", () => {
  it("counts a downstream answer by result, the most severe first, and an upstream one by effect", () => {
    const data = asData({
      direction: "downstream",
      facts_only: {
        counts_by_result: { degraded: 5, down: 2, unknown: 0 },
        unfilled_channel_objects: 4,
      },
      with_assumptions: { counts_by_result: { down: 3 }, unfilled_channel_objects: 9 },
    });
    expect(serverCounts(data, "facts_only")).toEqual([
      { grade: "down", count: 2 },
      { grade: "degraded", count: 5 },
    ]);
    expect(totalCount(data, "facts_only")).toBe(7);
    expect(totalCount(data, "with_assumptions")).toBe(3);
    expect(unfilledCount(data, "with_assumptions")).toBe(9);
    expect(singlePoints(data)).toBeUndefined();

    const up = asData({
      direction: "upstream",
      upstream: { total: 43, single_points_of_failure: 6, counts_by_effect: { none: 4, down: 6 } },
    });
    expect(serverCounts(up, "facts_only").map((c) => c.grade)).toEqual(["down", "none"]);
    expect(totalCount(up, "facts_only")).toBe(43);
    expect(unfilledCount(up, "facts_only")).toBeUndefined();
    expect(singlePoints(up)).toBe(6);
  });

  it("expects as many items as the server counted, up to the limit asked for", () => {
    const data = asData(smallImpact());
    const total = totalCount(data, "facts_only");
    expect(expectedItems(data, "facts_only", 2000)).toBe(total);
    expect(expectedItems(data, "facts_only", 3)).toBe(3);
  });
});
