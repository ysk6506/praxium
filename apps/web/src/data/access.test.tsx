// @vitest-environment happy-dom

import { act } from "react";
import { describe, expect, it } from "vitest";
import { ApiError } from "../api";
import { renderHook } from "../test-support/render";
import { createConnectionStore, type StorageLike } from "./connection";
import {
  applyEntry,
  createMark,
  DEMO_KEY,
  type EntryHistory,
  forgetToken,
  LOCAL_KEY,
  parseEntryMode,
  saveTypedToken,
  useEntryMode,
  watchEntry,
} from "./entry";
import { describeError, errorDetails, errorFacts, withAccessMode, withNames } from "./errors";

class MemoryStorage implements StorageLike {
  readonly items = new Map<string, string>();
  getItem = (key: string) => this.items.get(key) ?? null;
  setItem = (key: string, value: string) => void this.items.set(key, value);
  removeItem = (key: string) => void this.items.delete(key);
}

function setup() {
  const storage = new MemoryStorage();
  const urls: string[] = [];
  const history: EntryHistory = { replaceState: (_s, _u, url) => void urls.push(url) };
  return {
    storage,
    urls,
    history,
    store: createConnectionStore(storage),
    demo: createMark(storage, DEMO_KEY),
    local: createMark(storage, LOCAL_KEY),
  };
}

describe("[REQ-296] the page knows how the person came in: demo, local mode or a typed token", () => {
  it("reads the mode of an entry address", () => {
    expect(parseEntryMode("#/connect?token=a1&demo=1")).toEqual({ token: "a1", mode: "demo" });
    expect(parseEntryMode("#/connect?token=a1&mode=local")).toEqual({ token: "a1", mode: "local" });
    expect(parseEntryMode("#/connect?token=a1&mode=other")).toEqual({
      token: "a1",
      mode: "direct",
    });
    expect(parseEntryMode("#/connect?mode=local")).toBeUndefined();
  });

  it("marks a local-mode entry and lands on the KG list, and a later demo entry replaces the mark", () => {
    const s = setup();
    const at = { pathname: "/", search: "" };
    applyEntry(
      { ...at, hash: "#/connect?token=local-1&mode=local" },
      s.history,
      s.store,
      s.demo,
      s.local,
    );
    expect([s.local.is("local-1"), s.demo.is("local-1"), s.urls]).toEqual([
      true,
      false,
      ["/#/kgs"],
    ]);
    applyEntry(
      { ...at, hash: "#/connect?token=demo-2&demo=1" },
      s.history,
      s.store,
      s.demo,
      s.local,
    );
    expect([s.local.is("local-1"), s.demo.is("demo-2"), s.urls[1]]).toEqual([
      false,
      true,
      "/#/kg/main/types",
    ]);
    expect(s.storage.items.has(LOCAL_KEY)).toBe(false);
  });

  it("takes a new address pasted into the open page, replacing the address before the token is announced", () => {
    const s = setup();
    const seen: string[] = [];
    s.store.subscribe(() => seen.push(`${s.store.getToken()}@${s.urls.length}`));
    const listeners: (() => void)[] = [];
    const location = { hash: "#/kgs", pathname: "/", search: "" };
    const off = watchEntry(
      {
        location,
        history: s.history,
        addEventListener: (_t, l) => void listeners.push(l),
        removeEventListener: (_t, l) => void listeners.splice(listeners.indexOf(l), 1),
      },
      s.store,
      s.demo,
      s.local,
    );
    location.hash = "#/connect?token=fresh-3&demo=1";
    for (const l of listeners) l();
    expect(seen).toEqual(["fresh-3@1"]);
    expect(s.demo.is("fresh-3")).toBe(true);
    location.hash = "#/kgs";
    for (const l of listeners) l();
    expect(s.urls).toHaveLength(1);
    off();
    expect(listeners).toHaveLength(0);
  });

  it("tells the components the mode, and forgets it with the token or when another token is typed", async () => {
    const s = setup();
    const at = { pathname: "/", search: "" };
    const hook = await renderHook(() => useEntryMode(s.store, s.demo, s.local));
    expect(hook.result.current).toBe("none");
    await act(async () => {
      applyEntry(
        { ...at, hash: "#/connect?token=m-1&mode=local" },
        s.history,
        s.store,
        s.demo,
        s.local,
      );
    });
    expect(hook.result.current).toBe("local");
    await act(async () => saveTypedToken("typed-2", s.store, s.demo, s.local));
    expect(hook.result.current).toBe("direct");
    expect(s.storage.items.has(LOCAL_KEY)).toBe(false);
    await act(async () => {
      applyEntry(
        { ...at, hash: "#/connect?token=d-3&demo=1" },
        s.history,
        s.store,
        s.demo,
        s.local,
      );
    });
    expect(hook.result.current).toBe("demo");
    await act(async () => forgetToken(s.store, s.demo, s.local));
    expect(hook.result.current).toBe("none");
    expect([...s.storage.items.keys()]).toEqual([]);
    await hook.unmount();
  });
});

const apiError = (code: string, status: number, params?: Record<string, unknown>) =>
  new ApiError({ status, code, title: "T", traceId: "tr-9", params });

describe("[REQ-296] an authentication error speaks in the words of the way in", () => {
  it("sends a demo or local-mode visitor to the new address, and leaves a typed token as it was", () => {
    const view = describeError(apiError("auth.token_invalid", 401));
    for (const mode of ["demo", "local"] as const) {
      const adapted = withAccessMode(view, mode);
      expect(adapted.hint).toBe(
        "서버를 다시 시작하면 접속 링크가 바뀝니다. 터미널에 출력된 새 주소를 여십시오.",
      );
      expect(adapted.kind).toBe("auth");
    }
    expect(withAccessMode(view, "direct")).toBe(view);
    expect(withAccessMode(view, "none")).toBe(view);
    const missing = withAccessMode(describeError(apiError("auth.token_missing", 401)), "demo");
    expect(missing.hint).toContain("새 주소");
    const other = describeError(apiError("not_found.resource", 404));
    expect(withAccessMode(other, "demo")).toBe(other);
  });
});

describe("[REQ-298] an error says what the server said about the cause", () => {
  it("shows the field and the message at once and folds the code, status and trace id", () => {
    const view = describeError(
      apiError("invalid_input.field", 422, {
        message: 'no object type "X"',
        detail: 'invalid_value: no object type "X"',
        field: "object_type",
        extra: 3,
      }),
    );
    expect(errorFacts(view)).toEqual([
      ["필드", "object_type"],
      ["서버 메시지", 'no object type "X"'],
    ]);
    expect(errorDetails(view)).toEqual([
      ["코드", "invalid_input.field"],
      ["HTTP 상태", "422"],
      ["추적 ID", "tr-9"],
      ["extra", "3"],
    ]);
    expect(errorFacts(describeError(apiError("server.internal", 500)))).toEqual([]);
    const withPointer = describeError(
      new ApiError({
        status: 422,
        code: "invalid_input.field",
        title: "T",
        errors: [{ pointer: "/limit", code: "too_big" }],
      }),
    );
    expect(errorFacts(withPointer)).toEqual([["필드", "/limit"]]);
    expect(errorDetails(withPointer)).toContainEqual(["/limit", "too_big"]);
  });

  it("names what was not found: the value the server sent, else the one the screen knows", () => {
    const notFound = (params: Record<string, unknown>) =>
      describeError(apiError("not_found.resource", 404, params));
    expect(withNames(notFound({ resource: "object", id: "obj_x" }), {}).message).toBe(
      "‘obj_x’라는 객체가 없거나, 이 토큰으로는 볼 수 없습니다.",
    );
    expect(withNames(notFound({ resource: "kg" }), { kg: "nokg" })).toMatchObject({
      title: "KG를 찾을 수 없습니다",
      message: "‘nokg’라는 KG가 없거나, 이 토큰으로는 볼 수 없습니다.",
    });
    expect(withNames(notFound({ resource: "object_type", id: "T" }), {}).title).toBe(
      "객체 타입을 찾을 수 없습니다",
    );
    const unnamed = notFound({ resource: "object" });
    expect(withNames(unnamed, {})).toBe(unnamed);
    const input = describeError(apiError("invalid_input.field", 422));
    expect(withNames(input, { kg: "x" })).toBe(input);
  });
});
