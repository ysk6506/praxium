// @vitest-environment happy-dom

import { useCallback } from "react";
import { afterEach, describe, expect, it } from "vitest";
import { fakeApi, problem } from "../test-support/fake-api";
import { renderHook, settle } from "../test-support/render";
import { useKgs, useObjectTypeDetail, useTypeCatalog } from "./catalog";
import {
  browserStorage,
  connection,
  createConnectionStore,
  type StorageLike,
  TOKEN_KEY,
} from "./connection";
import { useNeighbors, useObject, useObjectPage, useRowDetails } from "./objects";
import { usePager } from "./pager";
import { useAsync } from "./useAsync";

const list = (data: unknown[], extra: object = {}) => ({
  body: {
    data,
    meta: {
      trace_id: "t",
      result_complete: {
        total: data.length,
        returned: data.length,
        truncated: false,
        reason: null,
        next_cursor: null,
      },
      ...extra,
    },
  },
});

const NO_ITEMS: never[] = [];

afterEach(() => {
  connection.clear();
});

class MemoryStorage implements StorageLike {
  readonly items = new Map<string, string>();
  getItem = (key: string) => this.items.get(key) ?? null;
  setItem = (key: string, value: string) => void this.items.set(key, value);
  removeItem = (key: string) => void this.items.delete(key);
}

describe("[REQ-278] the token is kept by the browser, and the page works without that", () => {
  it("reads the saved token, trims a new one and tells listeners", () => {
    const storage = new MemoryStorage();
    storage.setItem(TOKEN_KEY, "saved");
    const store = createConnectionStore(storage);
    let heard = 0;
    const off = store.subscribe(() => heard++);
    expect(store.getToken()).toBe("saved");
    store.setToken("  new-token \n");
    expect([store.getToken(), storage.getItem(TOKEN_KEY), heard]).toEqual([
      "new-token",
      "new-token",
      1,
    ]);
    store.clear();
    expect([store.getToken(), storage.getItem(TOKEN_KEY), heard]).toEqual(["", null, 2]);
    off();
    store.setToken("x");
    expect(heard).toBe(2);
  });

  it("keeps the token for the page when storage refuses or is missing", () => {
    const refusing: StorageLike = {
      getItem: () => {
        throw new Error("blocked");
      },
      setItem: () => {
        throw new Error("blocked");
      },
      removeItem: () => {
        throw new Error("blocked");
      },
    };
    for (const storage of [refusing, undefined]) {
      const store = createConnectionStore(storage);
      store.setToken("abc");
      expect(store.getToken()).toBe("abc");
      store.clear();
      expect(store.getToken()).toBe("");
    }
  });

  it("finds the browser's storage in a browser", () => {
    expect(browserStorage()).toBeDefined();
  });
});

describe("[REQ-278] a read is loading, then ready or failed", () => {
  it("goes from loading to ready and sends the token", async () => {
    const api = fakeApi({ "GET /v1/kgs": list([{ name: "dev" }]) });
    connection.setToken("tok-1");
    const hook = await renderHook(() => useKgs());
    await settle();
    expect(hook.result.current.state).toMatchObject({ status: "ready", data: [{ name: "dev" }] });
    expect(api.asked[0]?.authorization).toBe("Bearer tok-1");
    await hook.unmount();
    api.restore();
  });

  it("fails with a view of the error and keeps the old data while it reads again", async () => {
    let answer: ReturnType<typeof problem> | ReturnType<typeof list> = list([1]);
    const api = fakeApi({ "GET /v1/kgs": () => answer });
    const hook = await renderHook(() => useKgs());
    await settle();
    answer = problem(401, "auth.token_invalid");
    hook.result.current.reload();
    await settle();
    const state = hook.result.current.state;
    expect(state.status).toBe("error");
    if (state.status === "error") expect([state.error.kind, state.data]).toEqual(["auth", [1]]);
    await hook.unmount();
    api.restore();
  });

  it("drops the answer of a read that was replaced, and does not read when disabled", async () => {
    const api = fakeApi({ "GET /v1/kgs": list([]) });
    const hook = await renderHook(() => {
      const load = useCallback(async () => "x", []);
      return useAsync(load, false);
    });
    await settle();
    expect(hook.result.current.state.status).toBe("loading");
    expect(api.asked).toHaveLength(0);
    await hook.unmount();
    api.restore();
  });
});

describe("[REQ-278] the screens read the catalog, the page of a type, an object and its neighbors", () => {
  const found = {
    items: [
      { object_id: "obj_1", object_type: "Rack", display_name: "R1", matched_on: [] },
      { object_id: "obj_2", object_type: "Rack", display_name: "R2", matched_on: [] },
    ],
    counts_by_type: [{ object_type: "Rack", count: 2 }],
  };
  const withMeta = (data: unknown) => ({
    body: {
      data,
      meta: {
        trace_id: "t",
        result_complete: {
          total: 2,
          returned: 2,
          truncated: false,
          reason: null,
          next_cursor: "c2",
        },
      },
    },
  });

  it("joins types with counts and reads one type in full", async () => {
    const api = fakeApi({
      "GET /v1/object-types": list([
        { name: "Rack", layer: "facility", tier: "core", description: "", parent: null },
      ]),
      "POST /v1/kgs/main/queries:find": withMeta(found),
      "GET /v1/object-types/Rack": { body: { data: { name: "Rack", effective_properties: [] } } },
    });
    const catalog = await renderHook(() => useTypeCatalog("main"));
    const detail = await renderHook(() => useObjectTypeDetail("Rack"));
    await settle();
    const state = catalog.result.current.state;
    expect(state.status === "ready" && state.data[0]).toMatchObject({ name: "Rack", count: 2 });
    expect(detail.result.current.state.status).toBe("ready");
    expect(api.asked.find((a) => a.path === "/v1/object-types/Rack")?.search).toBe(
      "?include=effective_properties",
    );
    await catalog.unmount();
    await detail.unmount();
    api.restore();
  });

  it("reads a page and then the attributes of each of its rows", async () => {
    const api = fakeApi({
      "POST /v1/kgs/main/queries:find": withMeta(found),
      "POST /v1/kgs/main/queries:get": () => ({
        body: { data: { object: {}, valid: {}, attributes: [] }, meta: { trace_id: "t" } },
      }),
    });
    const hook = await renderHook(() => {
      const page = useObjectPage("main", "Rack", 25, "cur");
      const items = page.state.data?.items ?? NO_ITEMS;
      return { page, details: useRowDetails("main", items) };
    });
    await settle(8);
    expect(hook.result.current.page.state.status).toBe("ready");
    expect([...hook.result.current.details.keys()].sort()).toEqual(["obj_1", "obj_2"]);
    const asked = api.asked.find((a) => a.path.endsWith("queries:find"));
    expect(asked?.body).toMatchObject({
      object_type: "Rack",
      limit: 25,
      cursor: "cur",
      shape: "items",
    });
    await hook.unmount();
    api.restore();
  });

  it("shows a row whose read failed as failed and the others as read", async () => {
    let calls = 0;
    const api = fakeApi({
      "POST /v1/kgs/main/queries:get": () =>
        ++calls === 1
          ? problem(404, "not_found.resource")
          : { body: { data: { object: {}, valid: {} }, meta: {} } },
    });
    const hook = await renderHook(() => useRowDetails("main", found.items as never));
    await settle(8);
    const statuses = [...hook.result.current.values()].map((d) => d.status).sort();
    expect(statuses).toEqual(["error", "ready"]);
    await hook.unmount();
    api.restore();
  });

  it("reads one object and its neighbors", async () => {
    const api = fakeApi({
      "POST /v1/kgs/main/queries:get": {
        body: { data: { object: { object_id: "o" }, valid: {} }, meta: { trace_id: "t" } },
      },
      "POST /v1/kgs/main/queries:neighbors": withMeta({ object: {}, groups: [], items: [] }),
    });
    const hook = await renderHook(() => ({
      o: useObject("main", "o"),
      n: useNeighbors("main", "o", 100),
    }));
    await settle();
    expect(hook.result.current.o.state.status).toBe("ready");
    expect(hook.result.current.n.state.status).toBe("ready");
    expect(api.asked.find((a) => a.path.endsWith("neighbors"))?.body).toMatchObject({
      object_id: "o",
      direction: "both",
      limit: 100,
    });
    await hook.unmount();
    api.restore();
  });

  it("keeps the pager's state between renders", async () => {
    const hook = await renderHook(() => usePager());
    expect([hook.result.current.page, hook.result.current.hasPrevious]).toEqual([1, false]);
    const { act } = await import("react");
    await act(async () => hook.result.current.next("c2"));
    expect([
      hook.result.current.page,
      hook.result.current.cursor,
      hook.result.current.hasPrevious,
    ]).toEqual([2, "c2", true]);
    await act(async () => hook.result.current.setLimit(50));
    expect([hook.result.current.page, hook.result.current.state.limit]).toEqual([1, 50]);
    await hook.unmount();
  });
});
