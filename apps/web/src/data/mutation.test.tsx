// @vitest-environment happy-dom

import { act } from "react";
import { afterEach, describe, expect, it, vi } from "vitest";
import type { Requester } from "../api";
import { ApiError, createUser, disableUser, listKGs, listLayers } from "../api";
import { fakeApi, problem, type Reply } from "../test-support/fake-api";
import { renderHook, settle } from "../test-support/render";
import { connection } from "./connection";
import { describeError } from "./errors";
import { invalidate } from "./invalidation";
import { useAsync } from "./useAsync";
import { isIdempotencyKey, type MutationOptions, useMutation } from "./useMutation";

afterEach(() => {
  connection.clear();
});

// `useAsync` needs a load with a stable identity, as `useCallback` gives in a screen.
const loadKgs = (client: Requester) => listKGs(client);
const loadLayers = (client: Requester) => listLayers(client);

const created = { status: 201, body: { data: { id: "u1" } } };
type Variables = { name: string };

/** A hook that creates a user; keys come from a counter, so the tests can tell which submission a key belongs to. */
function creating(extra: Partial<MutationOptions<Variables, unknown>> = {}) {
  let count = 0;
  return renderHook(() =>
    useMutation<Variables, unknown>({
      run: (client, variables, context) =>
        createUser(client, {
          headers: { "Idempotency-Key": context.idempotencyKey },
          body: { name: variables.name, roles: [] },
        }),
      newKey: () => `key-${String(++count).padStart(4, "0")}`,
      ...extra,
    }),
  );
}

const keysOf = (asked: { headers: Readonly<Record<string, string>> }[]): (string | undefined)[] =>
  asked.map((a) => a.headers["idempotency-key"]);

describe("[REQ-408] a write is idle, pending, then done or failed", () => {
  it("goes through pending to success and sends the key, the token and the body", async () => {
    connection.setToken("tok");
    const api = fakeApi({ "POST /v1/users": created });
    const hook = await creating();
    expect(hook.result.current.state.status).toBe("idle");
    let done: Promise<boolean> | undefined;
    await act(async () => {
      done = hook.result.current.submit({ name: "ann" });
    });
    expect(await done).toBe(true);
    await settle();
    expect(hook.result.current.state).toEqual({
      status: "success",
      result: { data: { id: "u1" } },
    });
    expect(api.asked).toHaveLength(1);
    expect(api.asked[0]?.body).toEqual({ name: "ann", roles: [] });
    expect(api.asked[0]?.authorization).toBe("Bearer tok");
    expect(keysOf(api.asked)).toEqual(["key-0001"]);
    await hook.unmount();
    api.restore();
  });

  it("shows pending while the call is out, and says so with isPending", async () => {
    let release: (() => void) | undefined;
    const waiting = new Promise<void>((resolve) => {
      release = resolve;
    });
    const hook = await renderHook(() =>
      useMutation<number, string>({
        run: async (_client, n) => {
          await waiting;
          return `done ${n}`;
        },
        newKey: () => "key-pending-1",
      }),
    );
    let done: Promise<boolean> | undefined;
    await act(async () => {
      done = hook.result.current.submit(1);
    });
    expect(hook.result.current.state.status).toBe("pending");
    expect(hook.result.current.isPending).toBe(true);
    await act(async () => {
      release?.();
      await done;
    });
    expect(hook.result.current.isPending).toBe(false);
    expect(hook.result.current.state).toEqual({ status: "success", result: "done 1" });
    await act(async () => hook.result.current.reset());
    expect(hook.result.current.state.status).toBe("idle");
    await hook.unmount();
  });
});

describe("[REQ-408] a submission in flight is not sent again", () => {
  it("sends one request for a double click and answers both calls alike", async () => {
    const api = fakeApi({ "POST /v1/users": created });
    const hook = await creating();
    const answers: boolean[] = [];
    await act(async () => {
      const first = hook.result.current.submit({ name: "ann" });
      const second = hook.result.current.submit({ name: "ann" });
      answers.push(...(await Promise.all([first, second])));
    });
    expect(answers).toEqual([true, true]);
    expect(api.asked).toHaveLength(1);
    await hook.unmount();
    api.restore();
  });

  it("does not send another one with other variables while one is in flight", async () => {
    const api = fakeApi({ "POST /v1/users": created });
    const hook = await creating();
    await act(async () => {
      const first = hook.result.current.submit({ name: "ann" });
      const second = hook.result.current.submit({ name: "bob" });
      await Promise.all([first, second]);
    });
    expect(api.asked.map((a) => (a.body as { name: string }).name)).toEqual(["ann"]);
    await hook.unmount();
    api.restore();
  });
});

describe("[REQ-408] one submission has one idempotency key", () => {
  const down: Reply = problem(503, "server.unavailable");

  it("sends the same key when the same variables are submitted again after a failure", async () => {
    const answers: Reply[] = [down, down, created];
    const api = fakeApi({ "POST /v1/users": () => answers.shift() as Reply });
    const hook = await creating();
    const results: boolean[] = [];
    for (let i = 0; i < 3; i++) {
      await act(async () => {
        results.push(await hook.result.current.submit({ name: "ann" }));
      });
    }
    expect(results).toEqual([false, false, true]);
    expect(keysOf(api.asked)).toEqual(["key-0001", "key-0001", "key-0001"]);
    await hook.unmount();
    api.restore();
  });

  it("makes a new key for changed variables, and for the next submission after a success", async () => {
    const answers: Reply[] = [down, created, created];
    const api = fakeApi({ "POST /v1/users": () => answers.shift() as Reply });
    const hook = await creating();
    await act(async () => void (await hook.result.current.submit({ name: "ann" })));
    await act(async () => void (await hook.result.current.submit({ name: "ann2" })));
    await act(async () => void (await hook.result.current.submit({ name: "ann2" })));
    expect(keysOf(api.asked)).toEqual(["key-0001", "key-0002", "key-0003"]);
    await hook.unmount();
    api.restore();
  });

  it("drops a key the server cannot take again", async () => {
    const reused = problem(409, "conflict.idempotency_key_reused");
    const answers: Reply[] = [reused, created];
    const api = fakeApi({ "POST /v1/users": () => answers.shift() as Reply });
    const hook = await creating();
    await act(async () => void (await hook.result.current.submit({ name: "ann" })));
    await act(async () => void (await hook.result.current.submit({ name: "ann" })));
    expect(keysOf(api.asked)).toEqual(["key-0001", "key-0002"]);
    await hook.unmount();
    api.restore();
  });

  it("accepts the lengths the server accepts and nothing else", () => {
    expect(isIdempotencyKey("a".repeat(7))).toBe(false);
    expect(isIdempotencyKey("a".repeat(8))).toBe(true);
    expect(isIdempotencyKey("a".repeat(128))).toBe(true);
    expect(isIdempotencyKey("a".repeat(129))).toBe(false);
    expect(isIdempotencyKey("with space")).toBe(false);
    expect(isIdempotencyKey("한글키한글키한글키")).toBe(false);
  });

  it("fails without sending when the key is not one the server takes", async () => {
    const api = fakeApi({ "POST /v1/users": created });
    const hook = await creating({ newKey: () => "short" });
    await act(async () => void (await hook.result.current.submit({ name: "ann" })));
    expect(api.asked).toHaveLength(0);
    expect(hook.result.current.state.status).toBe("error");
    await hook.unmount();
    api.restore();
  });
});

describe("[REQ-408] a failure comes in words and with the fields the server named", () => {
  it("gives the sentences and the fields of a 422", async () => {
    const api = fakeApi({
      "POST /v1/users": {
        status: 422,
        problem: true,
        body: {
          type: "/v1/problems/invalid_input.field",
          title: "Invalid input",
          status: 422,
          code: "invalid_input.field",
          trace_id: "tr-9",
          errors: [
            { pointer: "/name", code: "invalid_input.field_required" },
            { pointer: "/roles/0", code: "invalid_input.field_format" },
          ],
        },
      },
    });
    const hook = await creating();
    await act(async () => void (await hook.result.current.submit({ name: "" })));
    const state = hook.result.current.state;
    expect(state.status).toBe("error");
    if (state.status === "error") {
      expect(state.error.kind).toBe("input");
      expect(state.error.title).toBe("요청이 올바르지 않습니다");
      expect(state.issues).toEqual([
        { pointer: "/name", code: "invalid_input.field_required" },
        { pointer: "/roles/0", code: "invalid_input.field_format" },
      ]);
    }
    await hook.unmount();
    api.restore();
  });

  it("says that the version is old on a 412 and that a version is needed on a 428", async () => {
    const api = fakeApi({
      "POST /v1/users/u1:disable": {
        status: 412,
        problem: true,
        body: {
          type: "x",
          title: "Conflict",
          status: 412,
          code: "conflict.version_mismatch",
          trace_id: "t",
          params: { current_etag: '"rev-9"' },
        },
      },
      "POST /v1/users/u2:disable": problem(428, "invalid_input.precondition_required"),
    });
    const hook = await renderHook(() =>
      useMutation<string, unknown>({
        run: (client, id, context) =>
          disableUser(client, {
            path: { user_id: id },
            headers: { "Idempotency-Key": context.idempotencyKey },
          }),
        newKey: () => "key-version-1",
      }),
    );
    await act(async () => void (await hook.result.current.submit("u1")));
    const stale = hook.result.current.state;
    expect(stale.status === "error" && stale.error.title).toBe("다른 곳에서 먼저 바뀌었습니다");
    await act(async () => void (await hook.result.current.submit("u2")));
    const missing = hook.result.current.state;
    expect(missing.status === "error" && missing.error.title).toBe("바꿀 항목의 버전이 필요합니다");
    await hook.unmount();
    api.restore();
  });

  it("says what happened when the server cannot be reached", async () => {
    const hook = await creating();
    const refused = new TypeError("failed to fetch");
    vi.stubGlobal("fetch", async () => {
      throw refused;
    });
    await act(async () => void (await hook.result.current.submit({ name: "ann" })));
    const state = hook.result.current.state;
    expect(state.status === "error" && state.error.kind).toBe("network");
    vi.unstubAllGlobals();
    await hook.unmount();
  });
});

describe("[REQ-408] the reads that depend on what was written read again", () => {
  it("refreshes a read of the invalidated topic without showing loading, and leaves other reads", async () => {
    let users = 1;
    const api = fakeApi({
      "POST /v1/users": created,
      "GET /v1/kgs": () => ({ body: { data: Array.from({ length: users }, (_, i) => i) } }),
      "GET /v1/layers": { body: { data: ["layer"] } },
    });
    const seen: string[] = [];
    const hook = await renderHook(() => {
      const mutation = useMutation<Variables, unknown>({
        run: (client, v, context) =>
          createUser(client, {
            headers: { "Idempotency-Key": context.idempotencyKey },
            body: { name: v.name, roles: [] },
          }),
        newKey: () => "key-refresh-1",
        invalidates: ["users"],
      });
      const readUsers = useAsync(loadKgs, true, { refreshOn: ["users"] });
      const readLayers = useAsync(loadLayers, true, { refreshOn: ["layers"] });
      seen.push(readUsers.state.status);
      return { mutation, readUsers, readLayers };
    });
    await settle();
    expect(api.asked.map((a) => a.path)).toEqual(["/v1/kgs", "/v1/layers"]);
    users = 2;
    seen.length = 0;
    await act(async () => void (await hook.result.current.mutation.submit({ name: "ann" })));
    await settle();
    expect(api.asked.map((a) => `${a.method} ${a.path}`)).toEqual([
      "GET /v1/kgs",
      "GET /v1/layers",
      "POST /v1/users",
      "GET /v1/kgs",
    ]);
    expect(seen).not.toContain("loading");
    const read = hook.result.current.readUsers.state;
    expect(read.status === "ready" && read.data).toEqual({ data: [0, 1] });
    await hook.unmount();
    api.restore();
  });

  it("does not refresh when the write failed, and tells onSuccess only after a success", async () => {
    const api = fakeApi({
      "POST /v1/users": problem(500, "server.internal"),
      "GET /v1/kgs": { body: { data: [] } },
    });
    const succeeded: string[] = [];
    const hook = await renderHook(() => {
      const mutation = useMutation<Variables, unknown>({
        run: (client, v, context) =>
          createUser(client, {
            headers: { "Idempotency-Key": context.idempotencyKey },
            body: { name: v.name, roles: [] },
          }),
        newKey: () => "key-refresh-2",
        invalidates: ["users"],
        onSuccess: (_result, v) => succeeded.push(v.name),
      });
      useAsync(loadKgs, true, { refreshOn: ["users"] });
      return mutation;
    });
    await settle();
    await act(async () => void (await hook.result.current.submit({ name: "ann" })));
    await settle();
    expect(api.asked.filter((a) => a.path === "/v1/kgs")).toHaveLength(1);
    expect(succeeded).toEqual([]);
    await hook.unmount();
    api.restore();
  });

  it("stops listening when the read is gone", async () => {
    const api = fakeApi({ "GET /v1/kgs": { body: { data: [] } } });
    const hook = await renderHook(() => useAsync(loadKgs, true, { refreshOn: ["users"] }));
    await settle();
    await hook.unmount();
    await act(async () => invalidate("users"));
    await settle();
    expect(api.asked).toHaveLength(1);
    api.restore();
  });
});

describe("[REQ-408] errors of a write in the words of the screen", () => {
  const view = (status: number, code: string, params?: Record<string, unknown>) =>
    describeError(new ApiError({ status, code, title: "t", params }));

  it("words 428, 412 and 409 in Korean, each its own", () => {
    const needed = view(428, "invalid_input.precondition_required");
    const old = view(412, "conflict.version_mismatch", { current_etag: '"rev-3"' });
    const running = view(409, "conflict.request_in_progress");
    const reused = view(409, "conflict.idempotency_key_reused", { reason: "another_body" });
    const shown = view(409, "conflict.idempotency_key_reused", { reason: "secret_shown_once" });
    const progress = view(409, "conflict.idempotency_key_reused", { reason: "in_progress" });
    for (const v of [needed, old, running, reused, shown, progress]) {
      expect(v.title).toMatch(/[가-힣]/);
      expect(v.message).toMatch(/[가-힣]/);
      expect(v.hint).toMatch(/[가-힣]/);
    }
    expect([needed.kind, old.kind, running.kind, reused.kind]).toEqual([
      "input",
      "conflict",
      "conflict",
      "conflict",
    ]);
    expect(new Set([needed, old, running, reused, shown].map((v) => v.title)).size).toBe(5);
    expect(progress.title).toBe(running.title);
    expect(shown.message).toContain("한 번만");
  });

  it("falls back to the group for a conflict the table does not name", () => {
    const other = view(409, "conflict.state_not_allowed");
    expect(other).toMatchObject({ kind: "conflict", title: "충돌했습니다" });
  });
});
