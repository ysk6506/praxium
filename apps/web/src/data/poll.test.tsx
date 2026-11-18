// @vitest-environment happy-dom

import { act } from "react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { Requester } from "../api";
import { ApiError, getKG } from "../api";
import { fakeApi, problem } from "../test-support/fake-api";
import { renderHook } from "../test-support/render";
import { currentEtag, describeError } from "./errors";
import { useAsync } from "./useAsync";

let hidden = false;
const loadRun = (client: Requester) => getKG(client, { path: { kg: "run" } });

function tabIs(state: "hidden" | "visible"): void {
  hidden = state === "hidden";
  document.dispatchEvent(new Event("visibilitychange"));
}

beforeEach(() => {
  hidden = false;
  vi.useFakeTimers();
  Object.defineProperty(document, "visibilityState", {
    configurable: true,
    get: () => (hidden ? "hidden" : "visible"),
  });
});

afterEach(() => {
  vi.useRealTimers();
  Reflect.deleteProperty(document, "visibilityState");
});

/** Let the timers run for `ms` and the answers of the fake API settle. */
async function wait(ms: number): Promise<void> {
  // In steps: a render that sets the next timer has to commit between two of them.
  for (let done = 0; done === 0 || done < ms; done += 50) {
    await act(async () => {
      await vi.advanceTimersByTimeAsync(Math.min(50, ms - done));
    });
  }
}

/** The run says `running` for `times` answers and `done` after. */
function runThatEnds(times: number) {
  let asked = 0;
  return fakeApi({
    "GET /v1/kgs/run": () => ({ body: { data: { state: ++asked > times ? "done" : "running" } } }),
  });
}

type RunAnswer = { data: { state: string } };
const finished = (answer: unknown): boolean => (answer as RunAnswer).data.state === "done";

describe("[REQ-408] a read can be read again without showing loading", () => {
  it("refresh keeps ready while it reads, reload shows loading", async () => {
    let n = 0;
    const api = fakeApi({ "GET /v1/kgs/run": () => ({ body: { data: { n: ++n } } }) });
    const seen: string[] = [];
    const hook = await renderHook(() => {
      const result = useAsync(loadRun);
      seen.push(result.state.status);
      return result;
    });
    await wait(0);
    expect(hook.result.current.state).toEqual({ status: "ready", data: { data: { n: 1 } } });
    seen.length = 0;
    await act(async () => hook.result.current.refresh());
    await wait(0);
    expect(seen).not.toContain("loading");
    expect(hook.result.current.state).toEqual({ status: "ready", data: { data: { n: 2 } } });
    seen.length = 0;
    await act(async () => hook.result.current.reload());
    await wait(0);
    expect(seen).toContain("loading");
    expect(hook.result.current.state).toEqual({ status: "ready", data: { data: { n: 3 } } });
    await hook.unmount();
    api.restore();
  });

  it("shows loading when a refresh is for another question than the one on the screen", async () => {
    const api = fakeApi({
      "GET /v1/kgs/run": { body: { data: { state: "a" } } },
      "GET /v1/kgs/other": { body: { data: { state: "b" } } },
    });
    let kg = "run";
    const hook = await renderHook(() => {
      const load = useCallbackFor(kg);
      return useAsync(load);
    });
    await wait(0);
    kg = "other";
    await act(async () => hook.result.current.refresh());
    await hook.rerender();
    await wait(0);
    expect(hook.result.current.state).toEqual({ status: "ready", data: { data: { state: "b" } } });
    await hook.unmount();
    api.restore();
  });
});

const loads = new Map<string, (client: Requester) => ReturnType<typeof getKG>>();
function useCallbackFor(kg: string) {
  let load = loads.get(kg);
  if (!load) {
    load = (client) => getKG(client, { path: { kg } });
    loads.set(kg, load);
  }
  return load;
}

describe("[REQ-408] a read repeats at an interval until its answer says it is done", () => {
  it("reads again after each interval and stops when the answer is done", async () => {
    const api = runThatEnds(2);
    const hook = await renderHook(() =>
      useAsync(loadRun, true, { poll: { intervalMs: 1000, until: finished } }),
    );
    await wait(0);
    expect(api.asked).toHaveLength(1);
    await wait(999);
    expect(api.asked).toHaveLength(1);
    await wait(1);
    expect(api.asked).toHaveLength(2);
    await wait(1000);
    expect(api.asked).toHaveLength(3);
    const last = hook.result.current.state;
    expect(last.status === "ready" && finished(last.data)).toBe(true);
    await wait(10_000);
    expect(api.asked).toHaveLength(3);
    await hook.unmount();
    api.restore();
  });

  it("never shows loading between the reads", async () => {
    const api = runThatEnds(3);
    const seen: string[] = [];
    const hook = await renderHook(() => {
      const result = useAsync(loadRun, true, { poll: { intervalMs: 500, until: finished } });
      seen.push(result.state.status);
      return result;
    });
    await wait(0);
    seen.length = 0;
    await wait(2000);
    expect(seen).not.toContain("loading");
    expect(api.asked).toHaveLength(4);
    await hook.unmount();
    api.restore();
  });

  it("keeps reading while there is no `until`", async () => {
    const api = runThatEnds(100);
    const hook = await renderHook(() => useAsync(loadRun, true, { poll: { intervalMs: 1000 } }));
    await wait(5000);
    expect(api.asked.length).toBeGreaterThanOrEqual(5);
    await hook.unmount();
    api.restore();
  });

  it("does not read while the tab is hidden, and reads once when it shows again", async () => {
    const api = runThatEnds(100);
    const hook = await renderHook(() =>
      useAsync(loadRun, true, { poll: { intervalMs: 1000, until: finished } }),
    );
    await wait(0);
    expect(api.asked).toHaveLength(1);
    tabIs("hidden");
    await wait(10_000);
    expect(api.asked).toHaveLength(1);
    await act(async () => tabIs("visible"));
    await wait(0);
    expect(api.asked).toHaveLength(2);
    await wait(1000);
    expect(api.asked).toHaveLength(3);
    await hook.unmount();
    api.restore();
  });

  it("does not read when the tab shows again but no wait was cut short", async () => {
    const api = runThatEnds(100);
    const hook = await renderHook(() =>
      useAsync(loadRun, true, { poll: { intervalMs: 1000, until: finished } }),
    );
    await wait(0);
    tabIs("hidden");
    await act(async () => tabIs("visible"));
    await wait(0);
    expect(api.asked).toHaveLength(1);
    await hook.unmount();
    api.restore();
  });

  it("stops after a failed read, and when the screen leaves or the read is turned off", async () => {
    let n = 0;
    const api = fakeApi({
      "GET /v1/kgs/run": () =>
        ++n === 2 ? problem(503, "server.unavailable") : { body: { data: {} } },
    });
    const hook = await renderHook(() => useAsync(loadRun, true, { poll: { intervalMs: 1000 } }));
    await wait(0);
    await wait(1000);
    expect(hook.result.current.state.status).toBe("error");
    await wait(10_000);
    expect(api.asked).toHaveLength(2);
    await hook.unmount();
    api.restore();

    const api2 = runThatEnds(100);
    let on = true;
    const toggled = await renderHook(() => useAsync(loadRun, on, { poll: { intervalMs: 1000 } }));
    await wait(0);
    on = false;
    await toggled.rerender();
    await wait(5000);
    expect(api2.asked).toHaveLength(1);
    await toggled.unmount();
    await wait(5000);
    expect(api2.asked).toHaveLength(1);
    api2.restore();
  });
});

describe("[REQ-408] the version the server holds now comes out of a refused change", () => {
  it("finds the current ETag of a 412, and nothing in other errors", () => {
    const refused = describeError(
      new ApiError({
        status: 412,
        code: "conflict.version_mismatch",
        title: "t",
        params: { current_etag: '"rev-9"' },
      }),
    );
    expect(currentEtag(refused)).toBe('"rev-9"');
    expect(
      currentEtag(
        describeError(new ApiError({ status: 412, code: "conflict.version_mismatch", title: "t" })),
      ),
    ).toBeUndefined();
    expect(
      currentEtag(
        describeError(
          new ApiError({
            status: 409,
            code: "conflict.state_not_allowed",
            title: "t",
            params: { current_etag: "x" },
          }),
        ),
      ),
    ).toBeUndefined();
  });
});
