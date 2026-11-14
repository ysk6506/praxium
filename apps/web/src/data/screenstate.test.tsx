// @vitest-environment happy-dom

import { describe, expect, it } from "vitest";
import type { TypeEntry } from "./catalog";
import type { StorageLike } from "./connection";
import {
  FRESHNESS_NAMES,
  freshnessName,
  KG_STATE_NAMES,
  kgRoleName,
  kgStateName,
  LAYER_NAMES,
  layerName,
} from "./display";
import { parseRoute, repairedHash, resolveRoute, routeHash, withParams } from "./router";
import { forwardNavigation, watchScroll } from "./scroll";
import {
  createTrail,
  nextSort,
  objectsViewParams,
  readObjectsView,
  readTypesView,
  sortTypes,
  typesViewParams,
} from "./urlstate";

const params = (query: string) => new URLSearchParams(query);

describe("[REQ-297] the screen of an address: unknown paths are said to be unknown, a bad tab is corrected", () => {
  it("tells an address that names no screen from the start page and from the screens", () => {
    expect(resolveRoute("", true)).toEqual({ page: "kgs" });
    expect(resolveRoute("#/", true)).toEqual({ page: "kgs" });
    expect(resolveRoute("#/kg/main", true)).toEqual({ page: "types", kg: "main" });
    expect(resolveRoute("#/kg/main/types/PhysicalServer", true)).toMatchObject({ page: "objects" });
    expect(resolveRoute("#/no/such/path", true)).toEqual({
      page: "notFound",
      path: "/no/such/path",
    });
    expect(resolveRoute("#/kg", true)).toMatchObject({ page: "notFound" });
    expect(resolveRoute("#/kg/main/objects", true)).toMatchObject({ page: "notFound" });
    expect(resolveRoute("#/kg/main/types/T/extra", true)).toMatchObject({ page: "notFound" });
    expect(resolveRoute("#/kgs/more", true)).toMatchObject({ page: "notFound" });
    expect(resolveRoute("#/connect", true)).toEqual({ page: "settings" });
    expect(resolveRoute("#/no/such/path", false)).toEqual({ page: "settings" });
    expect(routeHash({ page: "notFound", path: "/x/y" })).toBe("#/x/y");
    expect(parseRoute("#/no/such/path", true)).toEqual({ page: "kgs" });
  });

  it("corrects the address of an unknown tab and leaves other query pairs alone", () => {
    expect(repairedHash("#/kg/main/objects/o1?tab=bogus", true)).toBe("#/kg/main/objects/o1");
    expect(repairedHash("#/kg/main/objects/o1?tab=bogus&x=1", true)).toBe(
      "#/kg/main/objects/o1?x=1",
    );
    expect(repairedHash("#/kg/main/objects/o1?tab=neighbors", true)).toBeUndefined();
    expect(repairedHash("#/kg/main/objects/o1", true)).toBeUndefined();
    expect(repairedHash("#/kg/main/types?tab=bogus", true)).toBeUndefined();
  });

  it("writes pairs into the query of a hash and removes the ones without a value", () => {
    expect(withParams("#/kg/main/types", { q: "서버", all: "1" })).toBe(
      "#/kg/main/types?q=%EC%84%9C%EB%B2%84&all=1",
    );
    expect(withParams("#/a?x=1&y=2", { x: undefined })).toBe("#/a?y=2");
    expect(withParams("#/a?x=1", { x: undefined })).toBe("#/a");
  });
});

describe("[REQ-297] the state of the type and object screens lives in the address", () => {
  it("reads the object screen's pages, size and columns, and falls back to the default for what does not fit", () => {
    expect(readObjectsView(params(""))).toEqual({
      size: 25,
      page: 1,
      cursor: undefined,
      columns: undefined,
    });
    expect(readObjectsView(params("page=3&cursor=abc&size=100&cols=a,b"))).toEqual({
      size: 100,
      page: 3,
      cursor: "abc",
      columns: ["a", "b"],
    });
    expect(readObjectsView(params("page=3"))).toMatchObject({ page: 1, cursor: undefined });
    expect(readObjectsView(params("page=x&cursor=abc&size=7"))).toMatchObject({
      page: 1,
      size: 25,
    });
    expect(readObjectsView(params("cols="))).toMatchObject({ columns: [] });
  });

  it("writes only what differs from the default", () => {
    expect(objectsViewParams({ size: 25, page: 1, cursor: undefined, columns: undefined })).toEqual(
      {
        size: undefined,
        page: undefined,
        cursor: undefined,
        cols: undefined,
      },
    );
    const view = { size: 50, page: 2, cursor: "c2", columns: ["a", "b"] };
    expect(
      readObjectsView(
        new URLSearchParams(
          Object.entries(objectsViewParams(view)).flatMap(([k, v]) =>
            v === undefined ? [] : [[k, v]],
          ),
        ),
      ),
    ).toEqual(view);
  });

  it("reads and writes the type screen's search, toggle and sort", () => {
    expect(readTypesView(params(""))).toEqual({ text: "", onlyFilled: true, sort: undefined });
    const view = readTypesView(params("q=rack&all=1&sort=count&dir=desc"));
    expect(view).toEqual({
      text: "rack",
      onlyFilled: false,
      sort: { key: "count", direction: "descending" },
    });
    expect(typesViewParams(view)).toEqual({ q: "rack", all: "1", sort: "count", dir: "desc" });
    expect(typesViewParams(readTypesView(params("")))).toEqual({
      q: undefined,
      all: undefined,
      sort: undefined,
      dir: undefined,
    });
    expect(readTypesView(params("sort=bogus")).sort).toBeUndefined();
  });

  it("sorts by a column, reverses on the second press and returns to the default on the third", () => {
    let sort = nextSort(undefined, "name");
    expect(sort).toEqual({ key: "name", direction: "ascending" });
    sort = nextSort(sort, "name");
    expect(sort?.direction).toBe("descending");
    expect(nextSort(sort, "name")).toBeUndefined();
    expect(nextSort(undefined, "count")).toEqual({ key: "count", direction: "descending" });
    const types = [
      { name: "B", layer: "hardware", count: 1 },
      { name: "A", layer: "network", count: 9 },
      { name: "C", layer: "facility", count: 9 },
    ] as TypeEntry[];
    expect(sortTypes(types, undefined).map((t) => t.name)).toEqual(["B", "A", "C"]);
    expect(sortTypes(types, { key: "name", direction: "descending" }).map((t) => t.name)).toEqual([
      "C",
      "B",
      "A",
    ]);
    expect(sortTypes(types, { key: "count", direction: "descending" }).map((t) => t.name)).toEqual([
      "A",
      "C",
      "B",
    ]);
    expect(sortTypes(types, { key: "layer", direction: "ascending" }).map((t) => t.layer)).toEqual(
      ["facility", "network", "hardware"].sort((a, b) =>
        layerName(a).localeCompare(layerName(b), "ko"),
      ),
    );
  });

  it("keeps the cursors of the pages seen, in storage when there is one", () => {
    const items = new Map<string, string>();
    const storage: StorageLike = {
      getItem: (k) => items.get(k) ?? null,
      setItem: (k, v) => void items.set(k, v),
      removeItem: (k) => void items.delete(k),
    };
    const trail = createTrail(storage, "t");
    trail.set(2, "c2");
    expect(createTrail(storage, "t").get(2)).toBe("c2");
    expect(trail.get(3)).toBeUndefined();
    items.set("t", "not json");
    expect(createTrail(storage, "t").get(2)).toBeUndefined();
    const lone = createTrail(undefined, "t");
    lone.set(2, "x");
    expect(lone.get(2)).toBe("x");
  });
});

describe("[REQ-299] the closed values of the product are shown by their display names", () => {
  it("names the layers, the freshness and the KG state and role, and shows an unknown code as it came", () => {
    expect(layerName("cross_cutting")).toBe("공통");
    expect(Object.keys(LAYER_NAMES)).toHaveLength(10);
    expect(freshnessName("no_expected_period")).toBe("갱신 주기 없음");
    expect(Object.keys(FRESHNESS_NAMES)).toEqual(["fresh", "stale", "no_expected_period"]);
    expect(kgStateName("serving")).toBe("읽기 가능");
    expect(Object.keys(KG_STATE_NAMES)).toEqual(["serving", "building"]);
    expect(kgRoleName("production")).toBe("운영");
    expect([layerName("new_layer"), freshnessName("x"), kgStateName("y"), kgRoleName("z")]).toEqual(
      ["new_layer", "x", "y", "z"],
    );
  });
});

type Listener = (event: Event) => void;

function fakeWindow() {
  const listeners = new Map<string, Listener[]>();
  const timers: (() => void)[] = [];
  const win = {
    scrollY: 0,
    height: 5000,
    location: { hash: "#/a" },
    history: { scrollRestoration: "auto" as "auto" | "manual" },
    scrollTo(_x: number, y: number) {
      win.scrollY = Math.min(y, win.height);
    },
    addEventListener: (type: string, l: Listener) =>
      void listeners.set(type, [...(listeners.get(type) ?? []), l]),
    removeEventListener: (type: string, l: Listener) =>
      void listeners.set(
        type,
        (listeners.get(type) ?? []).filter((x) => x !== l),
      ),
    setTimeout: (h: () => void) => void timers.push(h),
  };
  const fire = (type: string, event: unknown = {}) => {
    for (const l of listeners.get(type) ?? []) l(event as Event);
  };
  const navigate = (to: string) => {
    const old = win.location.hash;
    win.location.hash = to;
    fire("hashchange", { oldURL: `http://x/${old}`, newURL: `http://x/${to}` });
  };
  const flush = () => {
    while (timers.length > 0) timers.shift()?.();
  };
  return { win, fire, navigate, flush, listeners };
}

describe("[REQ-297] a new screen opens at the top, and back returns to where the person was", () => {
  it("scrolls to the top for a link to another screen and restores the position on back", () => {
    const f = fakeWindow();
    const memory = watchScroll(f.win);
    expect(f.win.history.scrollRestoration).toBe("manual");
    f.win.scrollY = 900;
    f.fire("scroll");
    f.fire("click", {
      target: { closest: () => ({}) },
      button: 0,
      metaKey: false,
      ctrlKey: false,
      shiftKey: false,
      defaultPrevented: false,
    });
    f.navigate("#/b");
    expect(f.win.scrollY).toBe(0);
    f.navigate("#/a");
    expect(f.win.scrollY).toBe(900);
    memory.dispose();
    expect(f.win.history.scrollRestoration).toBe("auto");
  });

  it("keeps waiting for the content when the page is not tall enough yet", () => {
    const f = fakeWindow();
    const memory = watchScroll(f.win);
    f.win.scrollY = 900;
    f.fire("scroll");
    f.win.location.hash = "#/b";
    f.win.height = 300;
    f.win.scrollY = 0;
    f.navigate("#/a");
    expect(f.win.scrollY).toBe(300);
    f.win.height = 5000;
    f.flush();
    expect(f.win.scrollY).toBe(900);
    memory.dispose();
  });

  it("keeps the position when only the query changed by a button, and goes to the top for an entry address", () => {
    const f = fakeWindow();
    const memory = watchScroll(f.win);
    f.win.scrollY = 400;
    forwardNavigation();
    f.navigate("#/a?tab=x");
    expect(f.win.scrollY).toBe(400);
    f.navigate("#/connect?token=t1&demo=1");
    expect(f.win.scrollY).toBe(0);
    f.fire("click", {
      target: { closest: () => null },
      button: 0,
      metaKey: false,
      ctrlKey: false,
      shiftKey: false,
      defaultPrevented: false,
    });
    f.fire("click", {
      target: {},
      button: 1,
      metaKey: false,
      ctrlKey: false,
      shiftKey: false,
      defaultPrevented: false,
    });
    memory.dispose();
    forwardNavigation();
  });
});
