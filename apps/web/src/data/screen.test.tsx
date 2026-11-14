// @vitest-environment happy-dom

import { act } from "react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { render } from "../test-support/render";
import { replaceHash, resolveRoute, useHash } from "./router";
import {
  fallbackTitle,
  ScreenReportsProvider,
  screenKey,
  titleOf,
  useScreen,
  useScreenTitle,
} from "./screen";

const moved = vi.hoisted(() => ({ forward: true }));
vi.mock("./scroll", async (original) => ({
  ...(await original<typeof import("./scroll")>()),
  lastChangeWasForward: () => moved.forward,
}));

function Named() {
  useScreenTitle(titleOf("서버-1", "PhysicalServer"));
  return null;
}

function Page() {
  const hash = useHash();
  const route = resolveRoute(hash, true);
  const screen = useScreen(route);
  return (
    <ScreenReportsProvider value={screen.report}>
      <output data-testid="said">{screen.announcement}</output>
      <button type="button" data-other>
        다른 곳
      </button>
      <main tabIndex={-1}>
        <h1 tabIndex={-1}>제목</h1>
        {route.page === "object" ? <Named /> : null}
      </main>
    </ScreenReportsProvider>
  );
}

const go = async (hash: string, forward: boolean) => {
  moved.forward = forward;
  await act(async () => replaceHash(hash));
};

afterEach(() => {
  document.body.innerHTML = "";
  document.title = "";
  moved.forward = true;
});

describe("[REQ-315] each screen names the page, and a move moves the focus", () => {
  it("writes the title of the page from the route, and the name an object screen reports", async () => {
    replaceHash("#/kgs");
    const view = await render(<Page />);
    expect(document.title).toBe("KG 목록 · 프락시움");
    await go("#/kg/main/objects/o1", true);
    expect(document.title).toBe("서버-1 · PhysicalServer · 프락시움");
    await go("#/kg/main/types/PhysicalServer", true);
    expect(document.title).toBe("PhysicalServer 객체 · main · 프락시움");
    await view.unmount();
  });

  it("moves the focus to the heading after a move the person made, and not on the first load", async () => {
    replaceHash("#/kgs");
    const view = await render(<Page />);
    expect(document.activeElement).toBe(document.body);
    await go("#/kg/main/types", true);
    expect(document.activeElement).toBe(document.querySelector("h1"));
    expect(document.querySelector("output")?.textContent).toBe("");
    await view.unmount();
  });

  it("leaves the focus alone for a change of the query and for back, and says where the person arrived", async () => {
    replaceHash("#/kg/main/types");
    const view = await render(<Page />);
    const other = document.querySelector("[data-other]") as HTMLElement;
    other.focus();
    await go("#/kg/main/types?q=rack", true);
    expect(document.activeElement).toBe(other);
    await go("#/kgs", false);
    expect(document.activeElement).toBe(other);
    expect(document.querySelector("output")?.textContent).toBe("KG 목록 · 프락시움");
    await view.unmount();
  });

  it("names every route, and two routes that differ in the query only are one screen", () => {
    expect(fallbackTitle({ page: "settings" })).toBe("접속 설정 · 프락시움");
    expect(fallbackTitle({ page: "notFound", path: "/x" })).toBe(
      "페이지를 찾을 수 없습니다 · 프락시움",
    );
    expect(fallbackTitle({ page: "types", kg: "main" })).toBe("객체 타입 · main · 프락시움");
    expect(fallbackTitle({ page: "object", kg: "main", id: "o1", tab: "attributes" })).toBe(
      "객체 · main · 프락시움",
    );
    expect(screenKey({ page: "object", kg: "main", id: "o1", tab: "neighbors" })).toBe(
      screenKey({ page: "object", kg: "main", id: "o1", tab: "attributes" }),
    );
  });
});
