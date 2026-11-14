// @vitest-environment happy-dom

import { act } from "react";
import { afterEach, describe, expect, it } from "vitest";
import type { Property } from "../api";
import { renderHook } from "../test-support/render";
import { orderedColumns } from "./columns";
import { groupKey, groupTotals } from "./objects";
import { replaceHash, useHashParams, useRoute } from "./router";
import { createTrail, tabStorage } from "./urlstate";

afterEach(() => {
  globalThis.location.hash = "";
});

describe("[REQ-297] the hooks that read and write the address", () => {
  it("follows the address, corrects an unknown tab in place and tells the route", async () => {
    replaceHash("#/kg/main/objects/o1?tab=bogus");
    const hook = await renderHook(() => useRoute(true));
    await act(async () => undefined);
    expect(hook.result.current.route).toEqual({
      page: "object",
      kg: "main",
      id: "o1",
      tab: "attributes",
    });
    expect(globalThis.location.hash).toBe("#/kg/main/objects/o1");
    await act(async () => hook.result.current.go({ page: "kgs" }));
    expect(globalThis.location.hash).toBe("#/kgs");
    await hook.unmount();
  });

  it("changes pairs of the query, by replacing the address or by adding a history entry", async () => {
    replaceHash("#/kg/main/types");
    const hook = await renderHook(() => useHashParams());
    await act(async () => hook.result.current.set({ q: "rack" }));
    expect(hook.result.current.params.get("q")).toBe("rack");
    expect(globalThis.location.hash).toBe("#/kg/main/types?q=rack");
    await act(async () => hook.result.current.set({ q: undefined, all: "1" }, "push"));
    expect(globalThis.location.hash).toBe("#/kg/main/types?all=1");
    await hook.unmount();
  });
});

describe("[REQ-297] the table's columns and the totals of the neighbor groups", () => {
  it("lists the required properties first, then the rest in the order of the model", () => {
    const prop = (name: string, required: boolean) =>
      ({ name, required, status: "active" }) as Property;
    expect(
      orderedColumns([prop("a", false), prop("b", true), prop("name", true), prop("c", false)]).map(
        (p) => p.name,
      ),
    ).toEqual(["b", "a", "c"]);
  });

  it("sums the groups of the API by link type and direction", () => {
    const totals = groupTotals({
      object: {},
      groups: [
        { link_type: "L", direction: "in", count: 5, peer_type: "A", meaning: "m" },
        { link_type: "L", direction: "in", count: 7, peer_type: "B", meaning: "m" },
        { link_type: "L", direction: "out", count: 1, peer_type: "A", meaning: "m" },
      ],
    } as never);
    expect(totals.get(groupKey("L", "in"))).toBe(12);
    expect(totals.get(groupKey("L", "out"))).toBe(1);
  });

  it("keeps the trail in memory when the storage refuses, and finds the tab's storage", () => {
    const refusing = {
      getItem: () => {
        throw new Error("no");
      },
      setItem: () => {
        throw new Error("no");
      },
      removeItem: () => undefined,
    };
    const trail = createTrail(refusing, "k");
    trail.set(2, "c");
    expect(trail.get(2)).toBe("c");
    expect(tabStorage()).toBeDefined();
  });
});
