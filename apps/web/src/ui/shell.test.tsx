// @vitest-environment happy-dom

import { afterEach, describe, expect, it, vi } from "vitest";
import { click, render } from "../test-support/render";
import {
  AppShell,
  Badge,
  Coded,
  ErrorState,
  Pagination,
  Section,
  SortTh,
  Table,
  THead,
  Tr,
} from "./index";

afterEach(() => {
  document.body.innerHTML = "";
});

describe("[REQ-299] the frame can be skipped and folded, and the parts say more", () => {
  it("has a skip link that moves the focus to the page and a menu that folds and opens", async () => {
    const view = await render(
      <AppShell brand="프락시움" nav={<a href="#/kgs">KG 목록</a>} place="a">
        <p>본문</p>
      </AppShell>,
    );
    const buttons = [...view.container.querySelectorAll("button")];
    expect(buttons[0]?.textContent).toBe("본문으로 건너뛰기");
    await click(buttons[0] as Element);
    expect(document.activeElement?.tagName).toBe("MAIN");
    const menu = buttons.find((b) => b.getAttribute("aria-expanded") !== null) as HTMLElement;
    expect(menu.getAttribute("aria-expanded")).toBe("false");
    expect(
      view.container.querySelector(`#${menu.getAttribute("aria-controls")}`)?.className,
    ).toContain("hidden");
    await click(menu);
    expect(menu.getAttribute("aria-expanded")).toBe("true");
    expect(menu.textContent).toBe("메뉴 닫기");
    await view.rerender(
      <AppShell brand="프락시움" nav={<a href="#/kgs">KG 목록</a>} place="b">
        <p>본문</p>
      </AppShell>,
    );
    expect(menu.getAttribute("aria-expanded")).toBe("false");
    await view.unmount();
  });

  it("sorts from a column header, writes the code in a tooltip and notes what a section shows", async () => {
    const onSort = vi.fn();
    const view = await render(
      <>
        <Table aria-label="t">
          <THead>
            <Tr>
              <SortTh sort="ascending" onSort={onSort}>
                타입
              </SortTh>
              <SortTh sort={undefined} onSort={onSort}>
                수
              </SortTh>
            </Tr>
          </THead>
        </Table>
        <Badge title="cross_cutting">공통</Badge>
        <Coded code="fresh">최신</Coded>
        <Section title="읽은 시점" note="변경 번호는 …">
          x
        </Section>
      </>,
    );
    const heads = view.container.querySelectorAll("th");
    expect([heads[0]?.getAttribute("aria-sort"), heads[1]?.getAttribute("aria-sort")]).toEqual([
      "ascending",
      "none",
    ]);
    await click(heads[0]?.querySelector("button") as Element);
    expect(onSort).toHaveBeenCalledTimes(1);
    expect(view.container.querySelector("span[title='cross_cutting']")?.textContent).toBe("공통");
    expect(view.container.querySelector("abbr")?.getAttribute("title")).toBe("fresh");
    expect(view.container.textContent).toContain("변경 번호는 …");
    await view.unmount();
  });

  it("shows the cause of an error at once and folds the details", async () => {
    const view = await render(
      <ErrorState
        title="요청이 올바르지 않습니다"
        message="m"
        facts={[["필드", "object_type"]]}
        details={[["코드", "invalid_input.field"]]}
      />,
    );
    expect(view.container.querySelector("details")?.textContent).toContain("invalid_input.field");
    expect(view.container.querySelector("details summary")?.textContent).toBe("자세히");
    expect(view.container.querySelector("details")?.hasAttribute("open")).toBe(false);
    expect(view.container.textContent).toContain("object_type");
    await view.unmount();
  });

  it("offers the first page when the page is not the first", async () => {
    const onFirst = vi.fn();
    const view = await render(
      <Pagination
        page={3}
        hasPrevious={false}
        hasNext
        onPrevious={() => undefined}
        onNext={() => undefined}
        pageSize={25}
        pageSizes={[25]}
        onPageSize={() => undefined}
        summary="51–75 / 90"
        onFirst={onFirst}
      />,
    );
    const first = [...view.container.querySelectorAll("button")].find(
      (b) => b.textContent === "처음",
    );
    await click(first as Element);
    expect(onFirst).toHaveBeenCalledTimes(1);
    await view.rerender(
      <Pagination
        page={1}
        hasPrevious={false}
        hasNext
        onPrevious={() => undefined}
        onNext={() => undefined}
        pageSize={25}
        pageSizes={[25]}
        onPageSize={() => undefined}
        summary="1–25 / 90"
        onFirst={onFirst}
      />,
    );
    expect(
      [...view.container.querySelectorAll("button")].some((b) => b.textContent === "처음"),
    ).toBe(false);
    await view.unmount();
  });
});
