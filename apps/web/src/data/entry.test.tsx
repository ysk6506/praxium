// @vitest-environment happy-dom

import { act } from "react";
import { afterEach, describe, expect, it } from "vitest";
import { renderHook } from "../test-support/render";
import { createConnectionStore, type StorageLike, TOKEN_KEY } from "./connection";
import {
  applyEntry,
  createMark,
  type EntryHistory,
  LOCAL_KEY,
  parseEntryMode,
  useEntryMode,
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

describe("[REQ-296] the page takes the token of an entry address and shows that it came in by local mode", () => {
  it("reads the token and the mode from the fragment, and nothing from any other address", () => {
    expect(parseEntryMode("#/connect?token=abc123&mode=local")).toEqual({
      token: "abc123",
      mode: "local",
    });
    expect(parseEntryMode("#/connect?token=%20abc123%20")).toEqual({
      token: "abc123",
      mode: "direct",
    });
    expect(parseEntryMode("#/connect?token=")).toBeUndefined();
    expect(parseEntryMode("#/connect")).toBeUndefined();
    expect(parseEntryMode("#/kgs?token=abc123")).toBeUndefined();
    expect(parseEntryMode("")).toBeUndefined();
  });

  it("saves the token, marks the local mode and replaces the address with the KG list", () => {
    const storage = new MemoryStorage();
    const store = createConnectionStore(storage);
    const marks = createMark(storage, LOCAL_KEY);
    const history = recordingHistory();
    const location = {
      hash: "#/connect?token=secret-token-0001&mode=local",
      pathname: "/",
      search: "",
    };
    expect(applyEntry(location, history, store, marks)).toBe(true);
    expect(storage.items.get(TOKEN_KEY)).toBe("secret-token-0001");
    expect(storage.items.get(LOCAL_KEY)).toBe("secret-token-0001");
    expect(marks.is(store.getToken())).toBe(true);
    expect(history.urls).toEqual(["/#/kgs"]);
    expect(history.urls.join()).not.toContain("secret-token-0001");
  });

  it("takes a token without the local mode as a direct entry, and leaves other addresses alone", () => {
    const storage = new MemoryStorage();
    const store = createConnectionStore(storage);
    const marks = createMark(storage, LOCAL_KEY);
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

  it("is local mode only while the saved token is the one that came in by the local-mode address", () => {
    const marks = createMark(new MemoryStorage(), LOCAL_KEY);
    marks.mark("local-token-000001");
    expect(marks.is("local-token-000001")).toBe(true);
    expect(marks.is("another-token-0001")).toBe(false);
    expect(marks.is("")).toBe(false);
  });

  it("keeps the mark in memory when the browser gives no storage", () => {
    const store = createConnectionStore(failing);
    const marks = createMark(failing, LOCAL_KEY);
    applyEntry(
      { hash: "#/connect?token=memory-token-0003&mode=local", pathname: "/", search: "" },
      recordingHistory(),
      store,
      marks,
    );
    expect(marks.is(store.getToken())).toBe(true);
  });

  it("tells a component about the local mode and stops when the token is replaced or deleted", async () => {
    const storage = new MemoryStorage();
    const store = createConnectionStore(storage);
    const marks = createMark(storage, LOCAL_KEY);
    applyEntry(
      { hash: "#/connect?token=hook-token-0004&mode=local", pathname: "/", search: "" },
      recordingHistory(),
      store,
      marks,
    );
    const hook = await renderHook(() => useEntryMode(store, marks));
    expect(hook.result.current).toBe("local");
    await act(async () => store.setToken("typed-token-0005"));
    expect(hook.result.current).toBe("direct");
    await hook.unmount();
  });
});
