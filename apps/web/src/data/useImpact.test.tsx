// @vitest-environment happy-dom

import { act } from "react";
import { afterEach, describe, expect, it } from "vitest";
import type { ImpactData } from "../api";
import { type Asked, fakeApi, problem } from "../test-support/fake-api";
import { smallImpact } from "../test-support/impact-world";
import { renderHook, settle } from "../test-support/render";
import { connection } from "./connection";
import { impactSource } from "./impact-input";
import {
  DEFAULT_IMPACT_VIEW,
  EMPTY_QUESTION,
  type ImpactView,
  impactAddress,
  itemsRequest,
  readImpactAddress,
} from "./impact-view";
import { toRequest, useImpact, withItems } from "./useImpact";

const A = `obj_${"a".repeat(32)}`;
const view = (over: Partial<ImpactView> = {}): ImpactView => ({
  ...DEFAULT_IMPACT_VIEW,
  question: { ...EMPTY_QUESTION, dead: [A] },
  ...over,
});

const PATH = "/v1/kgs/main/queries:impact";
const sample = smallImpact() as unknown as ImpactData;
const factsItems = sample.facts_only?.items ?? [];

const meta = (next: string | null, returned = 0) => ({
  trace_id: "t",
  warnings: [],
  model_limits: [],
  hidden: { has_hidden: false, hidden_passed_count: 0, hidden_reached_count: 0 },
  result_complete: { total: 0, returned, truncated: false, reason: null, next_cursor: next },
});

/** The summary answer: the counts of both readings and no items. */
const summary = () => ({
  body: {
    data: {
      ...sample,
      facts_only: { ...sample.facts_only, items: undefined },
      with_assumptions: { ...sample.with_assumptions, items: undefined },
    },
    meta: meta(null),
  },
});

/** The items in pages of `size`: the answer to the n-th request for items is the n-th page. */
function pages(size: number) {
  let n = 0;
  return () => {
    const part = factsItems.slice(n * size, (n + 1) * size);
    n++;
    const more = n * size < factsItems.length;
    return {
      body: {
        data: { ...sample, facts_only: { ...sample.facts_only, items: part } },
        meta: meta(more ? `cursor-${n}` : null, part.length),
      },
    };
  };
}

const bodies = (asked: readonly Asked[]) =>
  asked.filter((a) => a.path === PATH).map((a) => a.body as Record<string, unknown>);

afterEach(() => connection.clear());

describe("[REQ-375] the screen asks the real API: the counts first, then the items a page at a time", () => {
  it("sends a summary of both readings and then the items of the reading, with the cursor of the page before", async () => {
    let call = 0;
    const next = pages(5);
    const api = fakeApi({
      [`POST ${PATH}`]: () => (call++ === 0 ? summary() : next()),
    });
    connection.setToken("t");
    const hook = await renderHook(() => useImpact("main", view()));
    await settle(8);
    const load = hook.result.current;
    expect(load.summary?.data.direction).toBe("downstream");
    expect(load.items.status).toBe("done");
    expect(load.items.items).toHaveLength(factsItems.length);
    expect(load.items.expected).toBe(factsItems.length);

    const sent = bodies(api.asked);
    expect(sent[0]).toMatchObject({ mode: "both", shape: "summary", max_items: 2000, dead: [A] });
    expect(sent[1]).toMatchObject({
      mode: "facts_only",
      shape: "items",
      max_items: 2000,
      limit: 1000,
    });
    expect(sent[1]?.cursor).toBeUndefined();
    expect(sent[2]?.cursor).toBe("cursor-1");
    expect(api.asked[0]?.authorization).toBe("Bearer t");
    await hook.unmount();
    api.restore();
  });

  it("asks the items again for another reading but not the summary, and everything again for another question", async () => {
    const api = fakeApi({
      [`POST ${PATH}`]: () => (api.asked.length === 0 ? summary() : pages(1000)()),
    });
    connection.setToken("t");
    let now = view();
    const hook = await renderHook(() => useImpact("main", now));
    await settle(8);
    expect(bodies(api.asked).map((b) => b.shape)).toEqual(["summary", "items"]);

    now = view({ reading: "with_assumptions" });
    await hook.rerender();
    await settle(8);
    expect(bodies(api.asked).map((b) => `${b.shape}:${b.mode}`)).toEqual([
      "summary:both",
      "items:facts_only",
      "items:with_assumptions",
    ]);

    now = view({ question: { ...EMPTY_QUESTION, target: A } });
    await hook.rerender();
    await settle(8);
    const last = bodies(api.asked).slice(-2);
    expect(last[0]).toMatchObject({ shape: "summary", direction: "upstream", target_object_id: A });
    await hook.unmount();
    api.restore();
  });

  it("does not ask for items when the server counted none", async () => {
    const empty = {
      body: {
        data: {
          ...sample,
          facts_only: { counts_by_result: {}, items: undefined },
          with_assumptions: { counts_by_result: {}, items: undefined },
        },
        meta: meta(null),
      },
    };
    const api = fakeApi({ [`POST ${PATH}`]: empty });
    connection.setToken("t");
    const hook = await renderHook(() => useImpact("main", view()));
    await settle(8);
    expect(hook.result.current.items).toMatchObject({ status: "done", items: [], expected: 0 });
    expect(bodies(api.asked)).toHaveLength(1);
    await hook.unmount();
    api.restore();
  });

  it("gives the error of the summary and keeps the items it already has when a later page fails", async () => {
    const failing = fakeApi({ [`POST ${PATH}`]: problem(404, "not_found.resource") });
    connection.setToken("t");
    const first = await renderHook(() => useImpact("main", view()));
    await settle(8);
    expect(first.result.current.summaryError?.kind).toBe("notFound");
    expect(first.result.current.summary).toBeUndefined();
    await first.unmount();
    failing.restore();

    let call = 0;
    const next = pages(5);
    const api = fakeApi({
      [`POST ${PATH}`]: () => {
        call++;
        if (call === 1) return summary();
        return call === 2 ? next() : problem(500, "server.internal");
      },
    });
    const hook = await renderHook(() => useImpact("main", view()));
    await settle(8);
    expect(hook.result.current.items.status).toBe("error");
    expect(hook.result.current.items.items).toHaveLength(5);
    expect(hook.result.current.items.error).toBeDefined();
    await hook.unmount();
    api.restore();
  });

  it("asks the items again on receiveAgain, and everything again on reload", async () => {
    const api = fakeApi({
      [`POST ${PATH}`]: () => (api.asked.length % 2 === 0 ? summary() : pages(1000)()),
    });
    connection.setToken("t");
    const hook = await renderHook(() => useImpact("main", view()));
    await settle(8);
    await act(async () => hook.result.current.receiveAgain());
    await settle(8);
    expect(bodies(api.asked).map((b) => b.shape)).toEqual(["summary", "items", "items"]);
    await act(async () => hook.result.current.reload());
    await settle(8);
    expect(
      bodies(api.asked)
        .map((b) => b.shape)
        .slice(3),
    ).toEqual(["summary", "items"]);
    await hook.unmount();
    api.restore();
  });
});

describe("[REQ-375] the answer is put together from the summary and the items", () => {
  it("puts the items in the section of the reading, or of the upstream answer", () => {
    const down = withItems(sample, "facts_only", factsItems.slice(0, 2));
    expect(down.facts_only?.items).toHaveLength(2);
    expect(impactSource(down, "facts_only").records).toHaveLength(2);
    const up = withItems(
      { ...sample, direction: "upstream", upstream: { total: 0 } } as unknown as ImpactData,
      "facts_only",
      [],
    );
    expect(up.upstream?.items).toEqual([]);
  });

  it("makes the request of the contract from the body of the screen without sharing its arrays", () => {
    const body = itemsRequest(view({ question: { ...EMPTY_QUESTION, site: [A], cut: ["lnk_x"] } }));
    const request = toRequest(body);
    expect(request.site_failure).toEqual([A]);
    expect(request.site_failure).not.toBe(body.site_failure);
    expect(request.dead_links).toEqual(["lnk_x"]);
    expect(request.dead).toBeUndefined();
  });
});

describe("[REQ-374] the question and the view are in the address and come back from it", () => {
  it("writes the address of the screen and reads the same view back", () => {
    const shown = view({ reading: "with_assumptions", view: "list", grade: "down", selected: A });
    const address = impactAddress("main", shown);
    expect(address.startsWith("#/kg/main/impact?")).toBe(true);
    expect(readImpactAddress(address)).toEqual({ kg: "main", view: shown });
  });
});
