// @vitest-environment happy-dom

import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { App } from "./App";
import { connection } from "./data";

(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;

/** A small server in the place of `fetch`: it answers by what it is asked and sees the signal of each request. */
interface Call {
  readonly path: string;
  readonly body: Record<string, unknown>;
  readonly signal: AbortSignal | undefined;
}
type Handler = (call: Call) => unknown | Promise<unknown>;

const calls: Call[] = [];
let handler: Handler = () => ({ status: 404 });

function json(body: unknown, status = 200): Response {
  const type = status >= 400 ? "application/problem+json" : "application/json";
  return new Response(JSON.stringify(body), { status, headers: { "Content-Type": type } });
}

function install(): void {
  vi.stubGlobal("fetch", (input: RequestInfo | URL, init?: RequestInit): Promise<Response> => {
    const path = new URL(String(input), "file:///fake").pathname;
    const call: Call = {
      path,
      body: typeof init?.body === "string" ? JSON.parse(init.body) : {},
      signal: init?.signal ?? undefined,
    };
    calls.push(call);
    return new Promise((resolve, reject) => {
      call.signal?.addEventListener("abort", () =>
        reject(new DOMException("aborted", "AbortError")),
      );
      Promise.resolve(handler(call)).then((answer) => {
        const reply = answer as { status?: number; body?: unknown };
        resolve(json(reply.body ?? reply, reply.status ?? 200));
      }, reject);
    });
  });
}

let root: Root | undefined;
let host: HTMLElement;

async function open(hash: string): Promise<void> {
  globalThis.location.hash = hash;
  host = document.createElement("div");
  document.body.appendChild(host);
  root = createRoot(host);
  await act(async () => root?.render(<App />));
  await settle();
}

async function settle(times = 8): Promise<void> {
  for (let i = 0; i < times; i++)
    await act(async () => new Promise((resolve) => setTimeout(resolve, 0)));
}

async function click(element: Element | null | undefined): Promise<void> {
  if (!element) throw new Error("nothing to click");
  await act(async () => {
    element.dispatchEvent(new MouseEvent("click", { bubbles: true, cancelable: true }));
  });
  await settle();
}

const text = (): string => host.textContent ?? "";
const byText = (selector: string, label: string): HTMLElement | undefined =>
  [...host.querySelectorAll<HTMLElement>(selector)].find((e) => e.textContent?.includes(label));

const hex = (n: number): string => n.toString(16).padStart(32, "0");
const ID = (n: number): string => `obj_${hex(n)}`;
const START = ID(1);
const ref = (n: number, type = "PhysicalServer") => ({
  object_id: ID(n),
  object_type: type,
  display_name: `${type.toLowerCase()}-${n}`,
});

function item(n: number, result: string, extra: Record<string, unknown> = {}) {
  const self = ref(n);
  return {
    ...self,
    result,
    hops: 1,
    also: [],
    lost_channels: ["power"],
    via: START,
    assumption_based: false,
    weakest_grade: "fact",
    path: [
      ref(1, "RackPdu"),
      { ...self, link: { grade: "fact", link_type: "SUPPLIED_BY", meaning: "supplied_by" } },
    ],
    ...extra,
  };
}

const COMPUTATION = {
  complete: true,
  items_over_cap: false,
  max_hops: 0,
  max_items: 2000,
  rounds: 2,
  stale_inputs: 0,
  stopped_by: null,
  time_budget_seconds: 30,
  weak_links: 0,
};

const META = {
  trace_id: "t",
  warnings: [] as string[],
  model_limits: ["Time is not modelled"],
  hidden: { has_hidden: false, hidden_passed_count: 0, hidden_reached_count: 0 },
  result_complete: {
    total: 0,
    returned: 0,
    truncated: false,
    reason: null,
    next_cursor: null as string | null,
  },
};

const section = (counts: Record<string, number>, items?: unknown[]) => ({
  counts_by_result: counts,
  counts_by_type: [],
  counts_by_parent: [],
  groups_touched: [],
  unfilled_channel_objects: 0,
  ...(items && { items }),
});

function downstream(counts: Record<string, number>, items?: unknown[], computation = {}) {
  const s = section(counts, items);
  return {
    body: {
      data: {
        direction: "downstream",
        seeds: [ref(1, "RackPdu")],
        computation: { ...COMPUTATION, ...computation },
        facts_only: s,
        with_assumptions: s,
      },
      meta: META,
    },
  };
}

/** The server: the summary, then the items; `answer` can make either one different. */
function serve(options: {
  counts: Record<string, number>;
  items?: unknown[];
  computation?: Record<string, unknown>;
}): void {
  const items = options.items ?? [];
  handler = (call) => {
    if (call.path === "/v1/object-types" || call.path === "/v1/link-types")
      return { body: { data: [], meta: META } };
    return call.body.shape === "summary"
      ? downstream(options.counts, undefined, options.computation)
      : downstream(options.counts, items, options.computation);
  };
}

const sample = (n: number) =>
  Array.from({ length: n }, (_, i) => item(10 + i, i % 3 === 0 ? "degraded" : "down"));

beforeEach(() => {
  calls.length = 0;
  install();
  connection.setToken("tok");
});

afterEach(async () => {
  await act(async () => root?.unmount());
  root = undefined;
  host?.remove();
  vi.unstubAllGlobals();
  connection.clear();
  globalThis.location.hash = "";
});

const IMPACT = `#/kg/main/impact?dead=${START}`;

describe("[REQ-375] [REQ-376] the screen asks the question and says what it is", () => {
  it("shows what was assumed to have stopped, the direction and the reading, and the answer after the wait", async () => {
    serve({ counts: { down: 2, degraded: 1 }, items: sample(3) });
    const pending = open(IMPACT);
    await pending;
    expect(text()).toContain("멈췄다고 가정한 객체");
    expect(byText("a", "rackpdu-1")?.getAttribute("href")).toBe(`#/kg/main/objects/${START}`);
    expect(text()).toContain("아래 방향");
    expect(text()).toContain("사실만");
    expect(host.querySelector("svg[role=group]")).not.toBeNull();
    const asked = calls.filter((c) => c.path.endsWith("queries:impact")).map((c) => c.body);
    expect(asked[0]).toMatchObject({ shape: "summary", mode: "both", max_items: 2000 });
    expect(asked[1]).toMatchObject({ shape: "items", mode: "facts_only", dead: [START] });
  });

  it("reads the reading from the address: it asks the items of that reading and says the assumptions are included", async () => {
    serve({ counts: { down: 2 }, items: sample(2) });
    await open(`${IMPACT}&reading=assumed`);
    const modes = calls.filter((c) => c.body.shape === "items").map((c) => c.body.mode);
    expect(modes).toEqual(["with_assumptions"]);
    expect(host.querySelector('[aria-label="읽는 방식"]')?.textContent).toContain("가정 포함");
    expect(text()).toContain("가정을 포함해 읽은 결과입니다");
  });

  it("says there is nothing to ask when the address names no object", async () => {
    serve({ counts: {} });
    await open("#/kg/main/impact");
    expect(text()).toContain("무엇이 멈췄다고 가정할지 정하지 않았습니다");
    expect(calls.filter((c) => c.path.endsWith("queries:impact"))).toHaveLength(0);
  });
});

describe("[REQ-377] the counts are the server's, and a count narrows the list", () => {
  it("shows the counts of the whole answer, not of the items received, with the shape and the word of each grade", async () => {
    serve({
      counts: { down: 681, degraded: 614 },
      items: sample(4),
      computation: { items_over_cap: true },
    });
    await open(IMPACT);
    const legend = host.querySelector('[aria-label="등급별 개수"]');
    expect(legend?.textContent).toContain("중단");
    expect(legend?.textContent).toContain("681");
    expect(legend?.textContent).toContain("614");
    expect(legend?.querySelectorAll("svg").length).toBe(2);
    expect(text()).toContain("서버가 센 전체 1295개");
    expect(text()).toContain("받은 항목은 4개입니다");
  });

  it("narrows the list to one grade when it is pressed, and widens it when it is pressed again", async () => {
    serve({ counts: { down: 2, degraded: 1 }, items: sample(3) });
    await open(`${IMPACT}&view=list`);
    expect(host.querySelectorAll("section[aria-label$='개']").length).toBe(2);
    const button = byText('[aria-label="등급별 개수"] button', "저하");
    await click(button);
    expect(globalThis.location.hash).toContain("grade=degraded");
    expect(button?.getAttribute("aria-pressed")).toBe("true");
    expect(host.querySelectorAll("section[aria-label$='개']").length).toBe(1);
    await click(button);
    expect(globalThis.location.hash).not.toContain("grade=");
    expect(host.querySelectorAll("section[aria-label$='개']").length).toBe(2);
  });
});

describe("[REQ-378] the graph is the first tab, the list is the other, and a node tells about itself", () => {
  it("opens on the graph, switches to the list in the address, and comes back to the same tab from the address", async () => {
    serve({ counts: { down: 2, degraded: 1 }, items: sample(3) });
    await open(IMPACT);
    const tabs = [...host.querySelectorAll('[role="tab"]')].map((t) => t.textContent);
    expect(tabs).toEqual(["그래프", "목록"]);
    expect(host.querySelector('[role="tab"][aria-selected="true"]')?.textContent).toBe("그래프");
    await click(byText('[role="tab"]', "목록"));
    expect(globalThis.location.hash).toContain("view=list");
    expect(host.querySelector("table")).not.toBeNull();
    expect(host.querySelector("svg[role=group]")).toBeNull();

    // A reload: the same address makes the same screen.
    const hash = globalThis.location.hash;
    await act(async () => root?.unmount());
    host.remove();
    await open(hash);
    expect(host.querySelector('[role="tab"][aria-selected="true"]')?.textContent).toBe("목록");
  });

  it("sorts the list by a column head and says so", async () => {
    serve({ counts: { down: 3 }, items: sample(3).map((i) => ({ ...i, result: "down" })) });
    await open(`${IMPACT}&view=list`);
    await click(byText("th button", "객체"));
    expect(globalThis.location.hash).toContain("sort=name");
    expect(host.querySelector("th[aria-sort='ascending']")?.textContent).toContain("객체");
  });

  it("tells about the node that is selected: its type, grade, what it lost, the way from the start and a link to it", async () => {
    serve({ counts: { down: 2, degraded: 1 }, items: sample(3) });
    await open(IMPACT);
    const node = host.querySelector<SVGElement>(
      `svg [role="button"][aria-label^="physicalserver-10"]`,
    );
    expect(node).not.toBeNull();
    await click(node);
    const detail = host.querySelector('[aria-label="선택한 객체"]');
    expect(detail?.textContent).toContain("PhysicalServer");
    expect(detail?.textContent).toContain("전원");
    expect(detail?.textContent).toContain("rackpdu-1");
    expect(detail?.querySelector("a")?.getAttribute("href")).toBe(`#/kg/main/objects/${ID(10)}`);
    expect(globalThis.location.hash).toContain(`sel=${ID(10)}`);
  });

  it("opens a result of more than 300 objects as groups, and a group lists its members, 100 at most", async () => {
    serve({ counts: { down: 400 }, items: sample(400).map((i) => ({ ...i, result: "down" })) });
    await open(IMPACT);
    expect(text()).toContain("타입별 묶음");
    const group = host.querySelector<SVGElement>(
      `svg [role="button"][aria-label^="PhysicalServer 400개"]`,
    );
    expect(group).not.toBeNull();
    await click(group);
    const detail = host.querySelector('[aria-label="선택한 묶음"]');
    expect(detail?.textContent).toContain("PhysicalServer 400개");
    expect(detail?.querySelectorAll("li").length).toBeLessThanOrEqual(100 + 1);
    expect(detail?.querySelectorAll("li").length).toBeGreaterThan(50);
    expect(detail?.textContent).toContain("나머지 300개");
  });
});

describe("[REQ-379] a result cut short says why and offers the way to ask again", () => {
  it("tells the item limit and asks again with a higher one", async () => {
    serve({
      counts: { down: 6000 },
      items: sample(3),
      computation: { items_over_cap: true, max_items: 2000 },
    });
    await open(IMPACT);
    expect(text()).toContain("항목 수 한도를 넘어 일부만 받았습니다");
    await click(byText("button", "한도를 5000개로 올려 다시 묻기"));
    expect(globalThis.location.hash).toContain("max=5000");
    const last = calls.filter((c) => c.body.shape === "summary").at(-1);
    expect(last?.body.max_items).toBe(5000);
  });

  it("tells the time limit, and the hop limit with a button that asks without it", async () => {
    serve({
      counts: { down: 3 },
      items: sample(3),
      computation: { complete: false, stopped_by: "time_budget" },
    });
    await open(IMPACT);
    expect(text()).toContain("시간 한도에서 계산이 멈췄습니다");
    expect(text()).not.toContain("항목 수 한도를 넘어");
    await act(async () => root?.unmount());
    host.remove();

    serve({
      counts: { down: 3 },
      items: sample(3),
      computation: { complete: false, stopped_by: "max_hops", max_hops: 2 },
    });
    await open(`${IMPACT}&hops=2`);
    expect(text()).toContain("단계 한도에서 계산이 멈췄습니다");
    await click(byText("button", "단계 한도 없이 다시 묻기"));
    expect(globalThis.location.hash).not.toContain("hops=");
  });

  it("says how many objects have no supplier for a channel they need, what the assumptions were, and what the calculation does not see", async () => {
    const answer = downstream({ down: 1 });
    handler = (call) => {
      if (call.path === "/v1/object-types" || call.path === "/v1/link-types")
        return { body: { data: [], meta: META } };
      const body = JSON.parse(JSON.stringify(answer.body));
      body.data.facts_only.unfilled_channel_objects = 711;
      body.data.computation.weak_links = 5;
      if (call.body.shape === "items") body.data.facts_only.items = sample(1);
      body.meta.warnings = ["capacity_group_unknown"];
      return { body };
    };
    await open(IMPACT);
    expect(text()).toContain("필요한 채널에 공급 링크가 없는 객체 711개");
    expect(text()).toContain("사실만으로 읽은 결과입니다");
    expect(text()).toContain("용량으로 이중화를 맺은 그룹");
    expect(host.querySelector("details")?.textContent).toContain("Time is not modelled");
  });
});

describe("[REQ-380] an answer with nothing, an error, and the single points of failure upstream", () => {
  it("says that nothing is affected and that this does not mean it is safe", async () => {
    serve({ counts: {} });
    await open(IMPACT);
    expect(text()).toContain("영향받는 객체가 없습니다");
    expect(text()).toContain("안전하다는 뜻은 아닙니다");
    expect(calls.filter((c) => c.body.shape === "items")).toHaveLength(0);
  });

  it("keeps what the calculation does not see in view when nothing is affected", async () => {
    serve({ counts: {} });
    await open(IMPACT);
    expect(host.querySelector("details")?.textContent).toContain("Time is not modelled");
  });

  it("shows the error of the server in the words of the other screens, with what was not found", async () => {
    handler = (call) =>
      call.path.endsWith("queries:impact")
        ? {
            status: 404,
            body: {
              type: "x",
              title: "Not found",
              status: 404,
              code: "not_found.resource",
              trace_id: "tr",
            },
          }
        : { body: { data: [], meta: META } };
    await open(IMPACT);
    expect(host.querySelector('[role="alert"]')).not.toBeNull();
    expect(text()).toContain("찾을 수 없습니다");
  });

  it("marks the single points of failure of what an object depends on, with their number from the server", async () => {
    const up = (items?: unknown[]) => ({
      body: {
        data: {
          direction: "upstream",
          target: ref(1),
          computation: COMPUTATION,
          upstream: {
            total: 2,
            single_points_of_failure: 1,
            counts_by_type: [],
            counts_by_effect: { down: 1, none: 1 },
            ...(items && { items }),
          },
        },
        meta: META,
      },
    });
    const dependency = (n: number, effect: string, single: boolean) => ({
      ...ref(n, "RackPdu"),
      hops: 1,
      effects: { facts_only: effect, with_assumptions: effect },
      single_point_of_failure: single,
      assumption_based: false,
      path: [
        { ...ref(n, "RackPdu") },
        { ...ref(1), link: { grade: "fact", link_type: "SUPPLIED_BY", meaning: "supplied_by" } },
      ],
    });
    handler = (call) =>
      call.path.endsWith("queries:impact")
        ? up(
            call.body.shape === "items"
              ? [dependency(2, "down", true), dependency(3, "none", false)]
              : undefined,
          )
        : { body: { data: [], meta: META } };
    await open(`#/kg/main/impact?target=${START}&view=list`);
    expect(text()).toContain("위 방향");
    expect(text()).toContain("단일 장애점 1개");
    expect(host.querySelector("table")?.textContent).toContain("단일 장애점");
    expect(text()).toContain("영향 없음");
  });
});

describe("[REQ-375] the request stops when the screen is left, the question changes, or the person says so", () => {
  const impactCalls = () => calls.filter((c) => c.path.endsWith("queries:impact"));

  it("aborts a request that is running when the screen is left", async () => {
    handler = (call) =>
      call.path.endsWith("queries:impact")
        ? new Promise(() => undefined)
        : { body: { data: [], meta: META } };
    await open(IMPACT);
    expect(text()).toContain("영향 범위를 계산하는 중");
    const running = impactCalls()[0]?.signal;
    expect(running?.aborted).toBe(false);
    await act(async () => {
      globalThis.location.hash = "#/kgs";
    });
    await settle();
    expect(running?.aborted).toBe(true);
  });

  it("aborts the question before when the question changes", async () => {
    handler = (call) =>
      call.path.endsWith("queries:impact")
        ? new Promise(() => undefined)
        : { body: { data: [], meta: META } };
    await open(IMPACT);
    const first = impactCalls()[0]?.signal;
    await act(async () => {
      globalThis.location.hash = `#/kg/main/impact?dead=${ID(2)}`;
    });
    await settle();
    expect(first?.aborted).toBe(true);
    expect(impactCalls().at(-1)?.body.dead).toEqual([ID(2)]);
  });

  it("stops receiving items when the person presses stop, draws what came, and says so", async () => {
    let page = 0;
    handler = (call) => {
      if (!call.path.endsWith("queries:impact")) return { body: { data: [], meta: META } };
      if (call.body.shape === "summary") return downstream({ down: 5 });
      if (page++ > 0) return new Promise(() => undefined);
      const answer = downstream({ down: 5 }, sample(2));
      answer.body.meta = {
        ...META,
        result_complete: { ...META.result_complete, next_cursor: "more" },
      };
      return answer;
    };
    await open(IMPACT);
    expect(text()).toContain("항목을 받는 중 2 / 5");
    const waiting = impactCalls().at(-1)?.signal;
    await click(byText("button", "불러오기 중지"));
    expect(waiting?.aborted).toBe(true);
    expect(text()).toContain("불러오기를 중지했습니다");
    expect(text()).toContain("5개 중 2개");
    expect(host.querySelector("svg[role=group]")).not.toBeNull();
    await click(byText("button", "다시 불러오기"));
    expect(impactCalls().filter((c) => c.body.shape === "items").length).toBeGreaterThan(2);
  });
});

describe("[REQ-374] the object screen opens the impact screen three ways", () => {
  const object = (type: string) => ({
    body: {
      data: { object: ref(7, type), valid: { from_change_seq: 1, to_change_seq: null } },
      meta: {
        ...META,
        as_of: { kg: "kg", change_seq: 1, generation: 1 },
        freshness: { state: "fresh" },
      },
    },
  });
  const model = (path: string) => {
    if (path === "/v1/object-types")
      return {
        body: {
          data: ["Room", "RackPdu"].map((name) => ({
            id: `ot.${name}`,
            name,
            layer: "facility",
            parent: null,
            traits: [],
          })),
          meta: META,
        },
      };
    if (path === "/v1/link-types")
      return { body: { data: [{ id: "lt.in", meaning: "contained_in" }], meta: META } };
    return {
      body: {
        data: [{ from: { kind: "type", ref: "ot.x" }, to: { kind: "type", ref: "ot.Room" } }],
        meta: META,
      },
    };
  };

  it("offers what stops with it and what it depends on, and what stops with everything inside it only for a type that holds others", async () => {
    handler = (call) => (call.path.endsWith("queries:get") ? object("RackPdu") : model(call.path));
    await open(`#/kg/main/objects/${ID(7)}`);
    const hrefs = [...host.querySelectorAll("header a")].map((a) => [
      a.textContent,
      a.getAttribute("href"),
    ]);
    expect(hrefs).toContainEqual(["이 객체가 멈추면", `#/kg/main/impact?dead=${ID(7)}`]);
    expect(hrefs).toContainEqual(["이 객체가 의존하는 것", `#/kg/main/impact?target=${ID(7)}`]);
    expect(hrefs.map((h) => h[0])).not.toContain("안에 든 것까지 멈추면");
    await act(async () => root?.unmount());
    host.remove();

    handler = (call) => (call.path.endsWith("queries:get") ? object("Room") : model(call.path));
    await open(`#/kg/main/objects/${ID(7)}`);
    const room = [...host.querySelectorAll("header a")].map((a) => [
      a.textContent,
      a.getAttribute("href"),
    ]);
    expect(room).toContainEqual(["안에 든 것까지 멈추면", `#/kg/main/impact?site=${ID(7)}`]);
  });
});

describe("[REQ-381] the words of the screen", () => {
  it("calls the loss of redundancy through an object it depends on 간접, and asks for an access token on the connection screen", async () => {
    serve({ counts: { exposed_inherited: 1 }, items: [item(10, "exposed_inherited")] });
    await open(IMPACT);
    expect(host.querySelector('[aria-label="등급별 개수"]')?.textContent).toContain(
      "이중화 상실(간접)",
    );
    expect(text()).not.toContain("상속");
    await act(async () => root?.unmount());
    host.remove();

    await open("#/settings");
    expect(text()).toContain("접근 토큰");
    expect(text()).not.toContain("개발용 토큰");
    for (const where of ["데모", "로컬 모드", "개발 서버", "운영 서버"])
      expect(text()).toContain(where);
    expect(text()).toContain("praxium admin token create");
  });
});
