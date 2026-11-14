import { describe, expect, it } from "vitest";
import { parseRoute, resolveRoute, routeHash } from "./router";
import { fallbackTitle } from "./screen";

describe("[REQ-374] the impact screen has an address of its own", () => {
  it("reads #/kg/{kg}/impact with its query as the impact screen, and writes it back without the query", () => {
    const route = resolveRoute("#/kg/main/impact?dead=obj_1&view=list", true);
    expect(route).toEqual({ page: "impact", kg: "main" });
    expect(routeHash({ page: "impact", kg: "a b" })).toBe("#/kg/a%20b/impact");
    expect(parseRoute(routeHash(route), true)).toEqual(route);
  });

  it("does not take a longer path for the impact screen, and needs a token like the other screens", () => {
    expect(resolveRoute("#/kg/main/impact/extra", true)).toMatchObject({ page: "notFound" });
    expect(resolveRoute("#/kg/main/impact", false)).toEqual({ page: "settings" });
  });

  it("is named for the tab before it has read what it shows", () => {
    expect(fallbackTitle({ page: "impact", kg: "main" })).toBe("영향 범위 · main · 프락시움");
  });
});
