// @vitest-environment happy-dom
import { act } from "react";
import { describe, expect, it, vi } from "vitest";
import { click, render } from "../test-support/render";
import type { GraphEdge, GraphLane, GraphNode, LaneGraphProps } from "./index";
import { GradeLegend, GradeList, GradeMark, LaneGraph, SpreadControls } from "./index";

const lanes: GraphLane[] = [
  { name: "설비", firstStep: 0 },
  { name: "하드웨어", firstStep: 1 },
  { name: "서비스", firstStep: 2 },
];

const node = (
  id: string,
  lane: number,
  row: number,
  step: number,
  parent: string | null,
  extra: Partial<GraphNode> = {},
): GraphNode => ({
  id,
  label: id,
  description: `${id}, 타입, 층, 중단, ${step}단계`,
  lane,
  row,
  step,
  parent,
  shape: "square-solid",
  tone: "danger",
  dashed: false,
  ...extra,
});

const nodes: GraphNode[] = [
  node("feed", 0, 0, 0, null, { shape: "double-circle", tone: "accent" }),
  node("srv-a", 1, 0, 1, "feed"),
  node("srv-b", 1, 1, 1, "feed", { shape: "triangle-solid", tone: "warn" }),
  node("svc-1", 2, 0, 2, "srv-a", { shape: "circle-dashed", tone: "muted", dashed: true }),
];

const edges: GraphEdge[] = [
  { from: "feed", to: "srv-a", step: 1, dashed: false, group: false },
  { from: "feed", to: "srv-b", step: 1, dashed: true, group: true },
  { from: "srv-a", to: "svc-1", step: 2, dashed: false, group: false },
];

const graph = (over: Partial<LaneGraphProps> = {}): LaneGraphProps => ({
  name: "영향 범위 그래프",
  summary: "영향받는 객체 3개, 열 3개, 단계 2까지",
  lanes,
  nodes,
  edges,
  step: 2,
  selected: undefined,
  onSelect: () => undefined,
  ...over,
});

const key = async (el: Element, name: string) =>
  act(async () => {
    el.dispatchEvent(new KeyboardEvent("keydown", { key: name, bubbles: true, cancelable: true }));
  });

const buttons = (root: HTMLElement) => [...root.querySelectorAll<SVGElement>("[data-node]")];
const byId = (root: HTMLElement, id: string) =>
  buttons(root).find((b) => b.dataset.node === id) as SVGElement;

describe("[REQ-354] a grade is drawn as a shape, a word and a color", () => {
  it("writes the word and the count beside a figure that a screen reader does not read", async () => {
    const view = await render(
      <GradeMark
        shape="triangle-solid"
        tone="warn"
        name="이중화 상실(직접)"
        count={4}
        title="exposed_direct"
      />,
    );
    expect(view.container.textContent).toContain("이중화 상실(직접)");
    expect(view.container.textContent).toContain("4");
    expect(view.container.querySelector("svg")?.getAttribute("aria-hidden")).toBe("true");
    expect(view.container.querySelector("polygon")?.getAttribute("class")).toContain("fill-warn");
    await view.unmount();
  });

  it("draws every shape differently, with a color token and never a color of its own", async () => {
    const shapes = [
      "double-circle",
      "square-solid",
      "square-dot",
      "circle-dashed",
      "triangle-solid",
      "triangle-open",
      "diamond",
      "square-bar",
      "circle-open",
    ] as const;
    const markup = new Set<string>();
    for (const shape of shapes) {
      const view = await render(
        <GradeMark shape={shape} tone="danger" dashed={shape === "circle-dashed"} name={shape} />,
      );
      const svg = view.container.querySelector("svg");
      markup.add(svg?.innerHTML ?? "");
      expect(svg?.innerHTML).not.toMatch(/#[0-9a-f]{3,6}|rgb\(|oklch\(/i);
      await view.unmount();
    }
    expect(markup.size).toBe(shapes.length);
  });

  it("draws a line of the link by what it rests on: dashed for an assumption, a diamond for a group on the way", async () => {
    const view = await render(<LaneGraph {...graph()} />);
    const lines = [...view.container.querySelectorAll("line")];
    expect(lines).toHaveLength(3);
    expect(lines.filter((l) => l.getAttribute("stroke-dasharray")).length).toBe(1);
    expect(lines.every((l) => l.getAttribute("class")?.includes("stroke-subtle"))).toBe(true);
    expect(view.container.querySelectorAll("g[data-step] > polygon")).toHaveLength(1);
    await view.unmount();
  });

  it("lists the grades with their counts and narrows to one when it is pressed", async () => {
    const onToggle = vi.fn();
    const view = await render(
      <GradeLegend
        items={[
          { key: "down", mark: { shape: "square-solid", tone: "danger", name: "중단" }, count: 12 },
          { key: "degraded", mark: { shape: "diamond", tone: "warn", name: "저하" }, count: 3 },
        ]}
        active="down"
        onToggle={onToggle}
      />,
    );
    const [down, degraded] = [...view.container.querySelectorAll("button")];
    expect(down?.getAttribute("aria-pressed")).toBe("true");
    expect(degraded?.getAttribute("aria-pressed")).toBe("false");
    expect(down?.textContent).toContain("중단");
    expect(down?.textContent).toContain("12");
    await click(degraded as Element);
    expect(onToggle).toHaveBeenCalledWith("degraded");
    await view.unmount();
  });
});

describe("[REQ-355] the graph is reached and moved through by keyboard, with a name for each node", () => {
  it("is a named region that scrolls by keyboard, holding a group with a summary", async () => {
    const view = await render(<LaneGraph {...graph()} />);
    const region = view.container.querySelector("section");
    expect(region?.getAttribute("aria-label")).toBe("영향 범위 그래프");
    expect(region?.getAttribute("tabindex")).toBe("0");
    const svg = view.container.querySelector("svg");
    expect(svg?.getAttribute("role")).toBe("group");
    expect(svg?.getAttribute("aria-label")).toBe("영향받는 객체 3개, 열 3개, 단계 2까지");
    await view.unmount();
  });

  it("gives each node a button role and a full name, and one tab stop among them all", async () => {
    const view = await render(<LaneGraph {...graph()} />);
    const all = buttons(view.container);
    expect(all).toHaveLength(4);
    for (const b of all) {
      expect(b.getAttribute("role")).toBe("button");
      expect(b.getAttribute("aria-label")).toContain("단계");
      expect(b.getAttribute("aria-pressed")).toBe("false");
    }
    expect(all.filter((b) => b.getAttribute("tabindex") === "0")).toHaveLength(1);
    expect(byId(view.container, "feed").getAttribute("tabindex")).toBe("0");
    await view.unmount();
  });

  it("moves with the arrow keys: right to a child, left to the cause, up and down in the column, Home and End", async () => {
    const view = await render(<LaneGraph {...graph()} />);
    const at = () => (document.activeElement as SVGElement | null)?.dataset.node;
    await act(async () => byId(view.container, "feed").focus());
    await key(byId(view.container, "feed"), "ArrowRight");
    expect(at()).toBe("srv-a");
    expect(byId(view.container, "srv-a").getAttribute("tabindex")).toBe("0");
    expect(buttons(view.container).filter((b) => b.getAttribute("tabindex") === "0")).toHaveLength(
      1,
    );
    await key(byId(view.container, "srv-a"), "ArrowDown");
    expect(at()).toBe("srv-b");
    await key(byId(view.container, "srv-b"), "ArrowUp");
    expect(at()).toBe("srv-a");
    await key(byId(view.container, "srv-a"), "End");
    expect(at()).toBe("srv-b");
    await key(byId(view.container, "srv-b"), "Home");
    expect(at()).toBe("srv-a");
    await key(byId(view.container, "srv-a"), "ArrowRight");
    expect(at()).toBe("svc-1");
    await key(byId(view.container, "svc-1"), "ArrowLeft");
    expect(at()).toBe("srv-a");
    await key(byId(view.container, "srv-a"), "ArrowLeft");
    await key(byId(view.container, "feed"), "ArrowLeft");
    expect(at()).toBe("feed");
    await key(byId(view.container, "feed"), "ArrowUp");
    expect(at()).toBe("feed");
    await view.unmount();
  });

  it("selects with Enter or Space and clears with Escape, and tells the choice in a polite status", async () => {
    const onSelect = vi.fn();
    const view = await render(<LaneGraph {...graph({ onSelect })} />);
    await key(byId(view.container, "feed"), "Enter");
    expect(onSelect).toHaveBeenLastCalledWith("feed");
    await key(byId(view.container, "feed"), " ");
    expect(onSelect).toHaveBeenCalledTimes(2);
    await key(byId(view.container, "feed"), "Escape");
    expect(onSelect).toHaveBeenLastCalledWith(undefined);
    await key(byId(view.container, "feed"), "q");
    expect(onSelect).toHaveBeenCalledTimes(3);
    await view.rerender(<LaneGraph {...graph({ selected: "srv-b", onSelect })} />);
    expect(byId(view.container, "srv-b").getAttribute("aria-pressed")).toBe("true");
    expect(view.container.querySelector("output")?.textContent).toContain(
      "srv-b, 타입, 층, 중단, 1단계, 선택됨",
    );
    await key(byId(view.container, "srv-b"), "Enter");
    expect(onSelect).toHaveBeenLastCalledWith(undefined);
    await click(byId(view.container, "srv-a"));
    expect(onSelect).toHaveBeenLastCalledWith("srv-a");
    await view.rerender(<LaneGraph {...graph({ selected: "srv-b", onSelect, playing: true })} />);
    expect(view.container.querySelector("output")?.textContent).toBe("");
    await view.unmount();
  });

  it("makes the selected node the tab stop, and leaves a skip button to the list when the screen gives one", async () => {
    const onSkip = vi.fn();
    const view = await render(<LaneGraph {...graph({ selected: "svc-1", onSkip })} />);
    expect(byId(view.container, "svc-1").getAttribute("tabindex")).toBe("0");
    const skip = view.container.querySelector("button");
    expect(skip?.textContent).toBe("목록으로 건너뛰기");
    await click(skip as Element);
    expect(onSkip).toHaveBeenCalled();
    await view.unmount();
  });
});

describe("[REQ-354] [REQ-356] the step decides what is shown, and with reduced motion nothing fades", () => {
  it("shows the layers up to the step and hides the later ones from the keyboard and the screen reader", async () => {
    const view = await render(<LaneGraph {...graph({ step: 1 })} />);
    const layers = [...view.container.querySelectorAll<SVGGElement>("g[data-step]")];
    expect(layers.map((g) => g.dataset.step)).toEqual(["0", "1", "2"]);
    expect(layers.map((g) => g.getAttribute("class")?.includes("opacity-100"))).toEqual([
      true,
      true,
      false,
    ]);
    expect(layers[2]?.getAttribute("class")).toContain("opacity-0");
    expect(layers[2]?.getAttribute("aria-hidden")).toBe("true");
    expect(layers[1]?.getAttribute("aria-hidden")).toBeNull();
    expect(byId(view.container, "svc-1").getAttribute("tabindex")).toBe("-1");
    const lit = [...view.container.querySelectorAll("[data-lane] text")].map((t) =>
      t.getAttribute("class")?.includes("font-semibold"),
    );
    expect(lit).toEqual([true, true, false]);
    await view.unmount();
  });

  it("fades only where motion is welcome: the transition is a motion-safe class, so reduced motion has none", async () => {
    const view = await render(<LaneGraph {...graph({ stepMs: 400 })} />);
    for (const g of view.container.querySelectorAll<SVGGElement>("g[data-step]")) {
      expect(g.getAttribute("class")).toContain("motion-safe:transition-");
      expect(g.getAttribute("class")).not.toMatch(/(^|\s)transition(-|\s|$)/);
      expect(g.getAttribute("style")).toContain("--step-ms: 400ms");
      expect(g.getAttribute("style")).not.toMatch(/transition/);
      expect(g.getAttribute("class")).toContain("motion-safe:duration-");
    }
    await view.unmount();
  });

  it("draws about as many elements as there are nodes and edges, never one for each frame", async () => {
    const view = await render(<LaneGraph {...graph()} />);
    expect(view.container.querySelectorAll("*").length).toBeLessThan(80);
    expect(view.container.querySelectorAll("g[data-step]").length).toBeLessThanOrEqual(3);
    await view.unmount();
  });

  it("skips to a spread with nothing in it", async () => {
    const view = await render(
      <LaneGraph {...graph({ nodes: [], edges: [], lanes: [], step: 0 })} />,
    );
    expect(view.container.querySelectorAll("[data-node]")).toHaveLength(0);
    await view.unmount();
  });
});

describe("[REQ-356] the controls of the spread work whatever the motion", () => {
  it("plays or pauses, replays and moves by the slider, and reads the step out", async () => {
    const calls = { play: vi.fn(), pause: vi.fn(), replay: vi.fn(), seek: vi.fn() };
    const props = {
      step: 2,
      last: 7,
      caption: "단계 2 / 7: 새로 영향받음 중단 3",
      onPlay: calls.play,
      onPause: calls.pause,
      onReplay: calls.replay,
      onSeek: calls.seek,
    };
    const view = await render(<SpreadControls {...props} playing={false} />);
    const [play, replay] = [...view.container.querySelectorAll("button")];
    expect(play?.textContent).toBe("재생");
    await click(play as Element);
    await click(replay as Element);
    expect(calls.play).toHaveBeenCalled();
    expect(calls.replay).toHaveBeenCalled();
    const slider = view.container.querySelector<HTMLInputElement>("input[type=range]");
    expect(slider?.min).toBe("0");
    expect(slider?.max).toBe("7");
    expect(slider?.value).toBe("2");
    expect(slider?.getAttribute("aria-valuetext")).toBe("단계 2 / 7: 새로 영향받음 중단 3");
    expect(view.container.querySelector("label")?.getAttribute("for")).toBe(slider?.id);
    await act(async () => {
      if (slider) {
        const set = Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, "value")?.set;
        set?.call(slider, "5");
        slider.dispatchEvent(new Event("input", { bubbles: true }));
      }
    });
    expect(calls.seek).toHaveBeenCalledWith(5);
    await view.rerender(<SpreadControls {...props} playing />);
    const pause = view.container.querySelector("button");
    expect(pause?.textContent).toBe("멈춤");
    await click(pause as Element);
    expect(calls.pause).toHaveBeenCalled();
    await view.unmount();
  });
});

describe("[REQ-357] the list is a table for each grade, sortable and paged", () => {
  const columns = [
    { key: "name", label: "객체", sort: { direction: "ascending" as const } },
    { key: "type", label: "타입", sort: { direction: undefined } },
    { key: "via", label: "원인" },
  ];
  const sections = [
    {
      key: "down",
      mark: { shape: "square-solid" as const, tone: "danger" as const, name: "중단" },
      total: 61,
      rows: [{ id: "a", cells: ["srv-1", "PhysicalServer", "feed-1"] }],
    },
    {
      key: "degraded",
      mark: { shape: "diamond" as const, tone: "warn" as const, name: "저하" },
      total: 2,
      rows: [{ id: "b", cells: ["vm-1", "VirtualMachine", "srv-1"] }],
    },
  ];

  it("has a heading and a named table for each grade, with the sortable columns as buttons and aria-sort", async () => {
    const onSort = vi.fn();
    const onPage = vi.fn();
    const view = await render(
      <GradeList
        sections={sections}
        columns={columns}
        onSort={onSort}
        page={2}
        pages={3}
        pageSize={50}
        summary="51–61 / 63"
        onPage={onPage}
        empty={<p>없음</p>}
      />,
    );
    const headings = [...view.container.querySelectorAll("h3")].map((h) => h.textContent);
    expect(headings[0]).toContain("중단");
    expect(headings[0]).toContain("61");
    expect(view.container.querySelectorAll("table")).toHaveLength(2);
    expect(view.container.querySelector("table")?.getAttribute("aria-label")).toBe("중단 객체");
    const heads = [...view.container.querySelectorAll("th")].slice(0, 3);
    expect(heads.map((h) => h.getAttribute("aria-sort"))).toEqual(["ascending", "none", null]);
    expect(heads[2]?.querySelector("button")).toBeNull();
    await click(heads[1]?.querySelector("button") as Element);
    expect(onSort).toHaveBeenCalledWith("type");
    expect(view.container.textContent).toContain("srv-1");
    expect(view.container.textContent).toContain("51–61 / 63");
    const names = [...view.container.querySelectorAll("button")].map((b) => b.textContent);
    await click(
      [...view.container.querySelectorAll("button")].find(
        (b) => b.textContent === "다음",
      ) as Element,
    );
    expect(onPage).toHaveBeenLastCalledWith(3);
    await click(
      [...view.container.querySelectorAll("button")].find(
        (b) => b.textContent === "이전",
      ) as Element,
    );
    expect(onPage).toHaveBeenLastCalledWith(1);
    await click(
      [...view.container.querySelectorAll("button")].find(
        (b) => b.textContent === "처음",
      ) as Element,
    );
    expect(onPage).toHaveBeenLastCalledWith(1);
    expect(names).toContain("이전");
    await view.unmount();
  });

  it("says that nothing was found when there is nothing to list", async () => {
    const view = await render(
      <GradeList
        sections={[]}
        columns={columns}
        onSort={() => undefined}
        page={1}
        pages={1}
        pageSize={50}
        summary=""
        onPage={() => undefined}
        empty={<p>영향받는 객체가 없습니다</p>}
      />,
    );
    expect(view.container.textContent).toBe("영향받는 객체가 없습니다");
    await view.unmount();
  });
});
