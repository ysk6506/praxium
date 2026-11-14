// @vitest-environment happy-dom
import { act } from "react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { impactOfSize, sampleLayerOf, smallImpact } from "../test-support/impact-world";
import { renderHook } from "../test-support/render";
import { buildOverview } from "./impact-group";
import { impactSource } from "./impact-input";
import { nodePicture, overviewPicture } from "./impact-picture";
import { buildImpactTree } from "./impact-tree";
import { useReducedMotion, useSpread } from "./useSpread";

const treeOf = (data = smallImpact()) =>
  buildImpactTree(impactSource(data, "with_assumptions"), sampleLayerOf);

describe("[REQ-354] [REQ-355] the tree becomes the picture the graph is given", () => {
  it("gives each node a position, a step, a shape and a full name for a screen reader", () => {
    const tree = treeOf();
    const picture = nodePicture(tree);
    expect(picture.nodes).toHaveLength(tree.order.length);
    expect(picture.summary).toBe(
      `영향받는 객체 ${picture.objects}개, 열 ${picture.lanes.length}개, 단계 ${picture.last}까지`,
    );
    for (const node of picture.nodes) {
      expect(node.description).toContain(`${tree.nodes.get(node.id)?.hops}단계`);
      expect(node.lane).toBeGreaterThanOrEqual(0);
      expect(node.lane).toBeLessThan(picture.lanes.length);
    }
    const root = picture.nodes[0];
    expect(root).toMatchObject({ shape: "double-circle", tone: "accent" });
    expect(root?.description).toContain("시작");
    const item = picture.nodes.find((n) => n.description.includes("중단"));
    expect(item?.description.split(", ")).toHaveLength(6);
    expect(item?.description).toMatch(/근거 (사실|가정|알 수 없음)/);
    expect(picture.lanes.map((l) => l.name)[0]).toBe("설비");
  });

  it("marks a hidden object without a name, an assumption and a group on the link", () => {
    const picture = nodePicture(treeOf());
    const hidden = picture.nodes.find((n) => n.id.startsWith("hidden:"));
    expect(hidden).toMatchObject({ label: "숨김 객체", dashed: true });
    expect(hidden?.description).not.toContain("hidden:");
    expect(picture.nodes.some((n) => n.description.includes("근거 가정"))).toBe(true);
    expect(picture.edges.some((e) => e.dashed)).toBe(true);
    expect(picture.edges.some((e) => e.group)).toBe(true);
  });

  it("puts a layer the model does not list in a column named 기타", () => {
    const tree = buildImpactTree(
      impactSource({ ...impactOfSize(300), direction: "downstream" }, "with_assumptions"),
      (type) => (type === "Pod" ? "mystery" : sampleLayerOf(type)),
    );
    const picture = nodePicture(tree);
    expect(picture.lanes.at(-1)?.name).toBe("기타");
  });

  it("draws an overview with a node for each group and the number of objects it stands for", () => {
    const tree = treeOf(impactOfSize(2000));
    const overview = buildOverview(tree);
    const picture = overviewPicture(tree, overview);
    expect(picture.nodes).toHaveLength(overview.order.length);
    expect(picture.nodes.reduce((n, g) => n + (g.count ?? 0), 0)).toBe(tree.nodes.size);
    expect(picture.objects).toBe(nodePicture(tree).objects);
    expect(picture.summary).toContain("타입별 묶음");
    expect(picture.last).toBe(nodePicture(tree).last);
    for (const node of picture.nodes) expect(node.description).toContain(`${node.count}개`);
    expect(picture.edges).toHaveLength(
      picture.nodes.length - picture.nodes.filter((n) => n.parent === null).length,
    );
    expect(picture.nodes.length).toBeLessThan(150);
  });
});

describe("[REQ-352] [REQ-356] the clock of the spread", () => {
  afterEach(() => vi.unstubAllGlobals());

  const frames = () => {
    const queue: ((time: number) => void)[] = [];
    vi.stubGlobal("requestAnimationFrame", (cb: (time: number) => void) => queue.push(cb));
    vi.stubGlobal("cancelAnimationFrame", () => undefined);
    return async (time: number) => {
      const cb = queue.pop();
      await act(async () => cb?.(time));
    };
  };

  it("plays on the frames the browser hands in and stops at the last step", async () => {
    const frame = frames();
    const hook = await renderHook(() => useSpread(4));
    expect(hook.result.current.state).toMatchObject({ step: 0, playing: true });
    const beat = hook.result.current.state.plan.beatMs;
    await frame(1000);
    await frame(1000 + beat);
    expect(hook.result.current.state.step).toBe(1);
    await frame(1000 + beat * 20);
    expect(hook.result.current.state).toMatchObject({ step: 4, playing: false });
    await hook.unmount();
  });

  it("is moved by the controls and follows an answer whose last step changes", async () => {
    frames();
    let last = 4;
    const hook = await renderHook(() => useSpread(last));
    await act(async () => hook.result.current.pause());
    expect(hook.result.current.state.playing).toBe(false);
    await act(async () => hook.result.current.seek(2));
    expect(hook.result.current.state.step).toBe(2);
    await act(async () => hook.result.current.replay());
    expect(hook.result.current.state).toMatchObject({ step: 0, playing: true });
    await act(async () => hook.result.current.play());
    last = 1;
    await hook.rerender();
    expect(hook.result.current.state.plan.last).toBe(1);
    await hook.unmount();
  });

  it("opens at the end, and does not play, when the system asks for less motion", async () => {
    const listeners: (() => void)[] = [];
    vi.stubGlobal("matchMedia", () => ({
      matches: true,
      addEventListener: (_: string, l: () => void) => listeners.push(l),
      removeEventListener: () => undefined,
    }));
    const hook = await renderHook(() => ({ spread: useSpread(5), reduced: useReducedMotion() }));
    expect(hook.result.current.reduced).toBe(true);
    expect(hook.result.current.spread.state).toMatchObject({
      step: 5,
      playing: false,
      reducedMotion: true,
    });
    expect(listeners.length).toBeGreaterThan(0);
    await hook.unmount();
  });

  it("allows motion when the browser cannot be asked", async () => {
    vi.stubGlobal("matchMedia", undefined);
    const hook = await renderHook(() => useReducedMotion());
    expect(hook.result.current).toBe(false);
    await hook.unmount();
  });
});
