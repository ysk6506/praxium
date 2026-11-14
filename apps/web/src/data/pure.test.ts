import { describe, expect, it } from "vitest";
import { ApiError, CLIENT_CODES, type Property } from "../api";
import { joinTypes } from "./catalog";
import { columnCandidates, defaultColumns } from "./columns";
import { mapLimit } from "./concurrency";
import { describeError, needsToken } from "./errors";
import {
  basisLabel,
  directionLabel,
  EMPTY_CELL,
  formatCount,
  formatQuantity,
  formatTimestamp,
  formatValue,
  gradeLabel,
  gradeTone,
  shortId,
} from "./format";
import { groupNeighbors } from "./objects";
import { currentCursor, initialPager, pageNumber, pagerReducer } from "./pager";
import { parseRoute, type Route, routeHash } from "./router";

const apiError = (code: string, status = 400): ApiError =>
  new ApiError({ status, code, title: "T", traceId: "tr-1" });

describe("[REQ-278] errors are shown as words a person can act on", () => {
  it("tells a missing, wrong and expired token apart and sends the person to the settings", () => {
    for (const code of ["auth.token_missing", "auth.token_invalid", "auth.token_expired"]) {
      const view = describeError(apiError(code, 401));
      expect(view.kind).toBe("auth");
      expect(needsToken(view)).toBe(true);
      expect(view.hint).toContain("접속 설정");
      expect(view.traceId).toBe("tr-1");
    }
  });

  it("names a server that cannot be reached and an answer that is not the API's", () => {
    expect(describeError(apiError(CLIENT_CODES.network, 0)).kind).toBe("network");
    expect(describeError(apiError(CLIENT_CODES.badResponse, 502)).message).toContain("502");
  });

  it("falls back from the code to its group, then to the code itself", () => {
    expect(describeError(apiError("permission.role_missing", 403)).kind).toBe("permission");
    expect(describeError(apiError("permission.something_new", 403)).kind).toBe("permission");
    expect(describeError(apiError("server.internal", 500)).kind).toBe("server");
    expect(describeError(apiError("brand.new", 418))).toMatchObject({
      kind: "other",
      message: "brand.new",
    });
  });

  it("shows an error that did not come from the API without its stack", () => {
    expect(describeError(new Error("boom")).message).toBe("boom");
    expect(describeError("odd").title).toBe("예기치 않은 오류");
    expect(needsToken(describeError(new Error("x")))).toBe(false);
  });
});

describe("[REQ-278] values are written the way the glossary words them", () => {
  it("writes grades and bases in the standard terms", () => {
    expect([gradeLabel("fact"), gradeLabel("assumed"), gradeLabel("unknown")]).toEqual([
      "사실",
      "가정",
      "알 수 없음",
    ]);
    expect(gradeLabel("new_grade")).toBe("new_grade");
    expect(gradeLabel(null)).toBe(EMPTY_CELL);
    expect(basisLabel("observed")).toBe("관측");
    expect(basisLabel(undefined)).toBe(EMPTY_CELL);
    expect(directionLabel("out")).toBe("나가는 링크");
    expect(directionLabel("sideways")).toBe("sideways");
    expect([
      gradeTone("fact"),
      gradeTone("assumed"),
      gradeTone("unknown"),
      gradeTone(null),
    ]).toEqual(["ok", "warn", "neutral", "neutral"]);
  });

  it("writes values on one line", () => {
    expect(formatValue("a")).toBe("a");
    expect(formatValue("")).toBe(EMPTY_CELL);
    expect(formatValue(null)).toBe(EMPTY_CELL);
    expect(formatValue(4)).toBe("4");
    expect(formatValue(false)).toBe("false");
    expect(formatValue({ a: [1] })).toBe('{"a":[1]}');
    expect(formatQuantity("3141.8", "W")).toBe("3141.8 W");
    expect(formatQuantity(null, "W")).toBe(EMPTY_CELL);
    expect(formatQuantity(5, undefined)).toBe("5");
  });

  it("writes times, ids and counts", () => {
    expect(formatTimestamp("2026-11-14T03:42:02Z")).toBe("2026-11-14 03:42:02 UTC");
    expect(formatTimestamp("2026-11-14T03:42:02.123Z")).toBe("2026-11-14 03:42:02 UTC");
    expect(formatTimestamp("yesterday")).toBe("yesterday");
    expect(formatTimestamp(null)).toBe(EMPTY_CELL);
    expect(shortId("obj_1f6bdfd7bf636df9364f3bfaba59e7f0")).toBe("obj_1f6bdfd7…");
    expect(shortId("obj_1")).toBe("obj_1");
    expect(formatCount(12345)).toBe("12,345");
  });
});

describe("[REQ-278] paging goes forward by cursor and back by the cursors already seen", () => {
  it("steps forward, back and to the first page, and resets on a new page size", () => {
    let state = initialPager();
    expect([state.limit, pageNumber(state), currentCursor(state)]).toEqual([25, 1, undefined]);
    state = pagerReducer(state, { type: "next", cursor: "c2" });
    state = pagerReducer(state, { type: "next", cursor: "c3" });
    expect([pageNumber(state), currentCursor(state)]).toEqual([3, "c3"]);
    state = pagerReducer(state, { type: "previous" });
    expect(currentCursor(state)).toBe("c2");
    state = pagerReducer(state, { type: "first" });
    expect(pageNumber(state)).toBe(1);
    expect(pagerReducer(state, { type: "previous" })).toBe(state);
    state = pagerReducer(pagerReducer(state, { type: "next", cursor: "c2" }), {
      type: "limit",
      limit: 100,
    });
    expect([state.limit, pageNumber(state)]).toEqual([100, 1]);
  });
});

describe("[REQ-278] the address names the screen", () => {
  const routes: Route[] = [
    { page: "settings" },
    { page: "kgs" },
    { page: "types", kg: "main" },
    { page: "objects", kg: "main", type: "PhysicalServer" },
    { page: "object", kg: "kg-1", id: "obj_a/b", tab: "attributes" },
    { page: "object", kg: "main", id: "obj_x", tab: "neighbors" },
  ];

  it("reads back what it wrote", () => {
    for (const route of routes) expect(parseRoute(routeHash(route), true)).toEqual(route);
  });

  it("sends a visit without a token to the settings, whatever the address", () => {
    expect(parseRoute("#/kg/main/types", false)).toEqual({ page: "settings" });
    expect(parseRoute("", false)).toEqual({ page: "settings" });
  });

  it("falls back to the KG list for an address it does not know, and to the first tab for an unknown tab", () => {
    expect(parseRoute("", true)).toEqual({ page: "kgs" });
    expect(parseRoute("#/nonsense", true)).toEqual({ page: "kgs" });
    expect(parseRoute("#/kg/main", true)).toEqual({ page: "types", kg: "main" });
    expect(parseRoute("#/kg/main/objects/o1?tab=bogus", true)).toMatchObject({ tab: "attributes" });
    expect(parseRoute("#/kg/%E0%A4%A/types", true)).toMatchObject({ page: "types" });
  });
});

describe("[REQ-278] the table of one type picks its columns", () => {
  const prop = (name: string, required = false, status = "active"): Property =>
    ({ name, required, status }) as Property;
  const properties = [
    prop("name", true),
    prop("description"),
    prop("state", true),
    prop("model"),
    prop("serial"),
    prop("old", false, "retired"),
    prop("a"),
    prop("b"),
  ];

  it("leaves out the name and nested attributes and retired ones", () => {
    expect(columnCandidates(properties).map((p) => p.name)).toEqual([
      "state",
      "model",
      "serial",
      "a",
      "b",
    ]);
  });

  it("puts the required ones first and stops at the count", () => {
    expect(defaultColumns(properties, 3)).toEqual(["state", "model", "serial"]);
    expect(defaultColumns(properties)).toEqual(["state", "model", "serial", "a", "b"]);
  });
});

describe("[REQ-278] the catalog of types and the groups of neighbors", () => {
  it("joins the model's types with the counts, by layer, the largest first", () => {
    const types = [
      { name: "B", layer: "hardware", tier: "core", description: "", parent: null },
      { name: "A", layer: "hardware", tier: "core", description: "", parent: null },
      { name: "C", layer: "facility", tier: "core", description: "", parent: "A" },
    ] as never;
    const joined = joinTypes(types, [{ object_type: "B", count: 7 }]);
    expect(joined.map((t) => [t.name, t.count])).toEqual([
      ["C", 0],
      ["B", 7],
      ["A", 0],
    ]);
  });

  it("groups neighbors by link type and direction in the listed order", () => {
    const item = (link_type: string, direction: string, id: string) =>
      ({ link_type, direction, peer_object_id: id }) as never;
    const groups = groupNeighbors({
      object: {},
      groups: [],
      items: [
        item("L", "in", "1"),
        item("M", "out", "2"),
        item("L", "in", "3"),
        item("L", "out", "4"),
      ],
    } as never);
    expect(groups.map((g) => [g.linkType, g.direction, g.items.length])).toEqual([
      ["L", "in", 2],
      ["M", "out", 1],
      ["L", "out", 1],
    ]);
    expect(groupNeighbors({ object: {}, groups: [] } as never)).toEqual([]);
  });
});

describe("[REQ-278] a list is read with a few calls at a time", () => {
  it("keeps the order of the results and never runs more than the limit", async () => {
    let running = 0;
    let peak = 0;
    const out = await mapLimit([1, 2, 3, 4, 5, 6, 7], 3, async (n) => {
      running++;
      peak = Math.max(peak, running);
      await new Promise((resolve) => setTimeout(resolve, 5 - (n % 3)));
      running--;
      return n * 2;
    });
    expect(out).toEqual([2, 4, 6, 8, 10, 12, 14]);
    expect(peak).toBeLessThanOrEqual(3);
    expect(await mapLimit([], 3, async (n: number) => n)).toEqual([]);
  });
});
