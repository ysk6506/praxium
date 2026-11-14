// @vitest-environment happy-dom

import { act } from "react";
import { afterEach, describe, expect, it } from "vitest";
import { renderHook } from "../test-support/render";
import { createConnectionStore, type StorageLike, TOKEN_KEY } from "./connection";
import {
  applyEntry,
  createDemoMark,
  DEMO_KEY,
  DEMO_LANDING,
  type EntryHistory,
  parseEntry,
  useDemo,
} from "./entry";

class MemoryStorage implements StorageLike {
  readonly items = new Map<string, string>();
  getItem = (key: string) => this.items.get(key) ?? null;
  setItem = (key: string, value: string) => void this.items.set(key, value);
  removeItem = (key: string) => void this.items.delete(key);
}

const failing: StorageLike = {
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

function recordingHistory(): EntryHistory & { urls: string[] } {
  const urls: string[] = [];
  return { urls, replaceState: (_state, _unused, url) => void urls.push(url) };
}

afterEach(() => {
  document.body.innerHTML = "";
});

describe("[REQ-281] the page takes the token of a demo address and shows that it is a demo", () => {
  it("reads the token and the demo flag from the fragment, and nothing from any other address", () => {
    expect(parseEntry("#/connect?token=abc123&demo=1")).toEqual({ token: "abc123", demo: true });
    expect(parseEntry("#/connect?token=%20abc123%20")).toEqual({ token: "abc123", demo: false });
    expect(parseEntry("#/connect?token=")).toBeUndefined();
    expect(parseEntry("#/connect")).toBeUndefined();
    expect(parseEntry("#/kgs?token=abc123")).toBeUndefined();
    expect(parseEntry("")).toBeUndefined();
  });

  it("saves the token, marks the demo and replaces the address with the KG screen", () => {
    const storage = new MemoryStorage();
    const store = createConnectionStore(storage);
    const marks = createDemoMark(storage);
    const history = recordingHistory();
    const location = {
      hash: "#/connect?token=secret-token-0001&demo=1",
      pathname: "/",
      search: "",
    };
    expect(applyEntry(location, history, store, marks)).toBe(true);
    expect(storage.items.get(TOKEN_KEY)).toBe("secret-token-0001");
    expect(storage.items.get(DEMO_KEY)).toBe("secret-token-0001");
    expect(marks.is(store.getToken())).toBe(true);
    expect(history.urls).toEqual([`/${DEMO_LANDING}`]);
    expect(history.urls.join()).not.toContain("secret-token-0001");
  });

  it("takes a token without the demo flag as no demo, and leaves other addresses alone", () => {
    const storage = new MemoryStorage();
    const store = createConnectionStore(storage);
    const marks = createDemoMark(storage);
    const history = recordingHistory();
    marks.mark("old-token-xxxxxxxxx");
    expect(
      applyEntry(
        { hash: "#/connect?token=plain-token-0002", pathname: "/", search: "?a=1" },
        history,
        store,
        marks,
      ),
    ).toBe(true);
    expect(marks.is(store.getToken())).toBe(false);
    expect(history.urls).toEqual(["/?a=1#/kgs"]);
    expect(applyEntry({ hash: "#/kgs", pathname: "/", search: "" }, history, store, marks)).toBe(
      false,
    );
    expect(history.urls).toHaveLength(1);
  });

  it("is a demo only while the saved token is the one that came in by the demo address", () => {
    const marks = createDemoMark(new MemoryStorage());
    marks.mark("demo-token-0000001");
    expect(marks.is("demo-token-0000001")).toBe(true);
    expect(marks.is("another-token-0001")).toBe(false);
    expect(marks.is("")).toBe(false);
  });

  it("keeps the mark in memory when the browser gives no storage", () => {
    const store = createConnectionStore(failing);
    const marks = createDemoMark(failing);
    applyEntry(
      { hash: "#/connect?token=memory-token-0003&demo=1", pathname: "/", search: "" },
      recordingHistory(),
      store,
      marks,
    );
    expect(marks.is(store.getToken())).toBe(true);
  });

  it("tells a component about the demo and stops when the token is replaced or deleted", async () => {
    const storage = new MemoryStorage();
    const store = createConnectionStore(storage);
    const marks = createDemoMark(storage);
    applyEntry(
      { hash: "#/connect?token=hook-token-0004&demo=1", pathname: "/", search: "" },
      recordingHistory(),
      store,
      marks,
    );
    const hook = await renderHook(() => useDemo(store, marks));
    expect(hook.result.current).toBe(true);
    await act(async () => store.setToken("typed-token-0005"));
    expect(hook.result.current).toBe(false);
    await hook.unmount();
  });
});
