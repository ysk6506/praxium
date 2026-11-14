// @vitest-environment happy-dom

import { act } from "react";
import { afterEach, describe, expect, it } from "vitest";
import { render } from "../test-support/render";
import { ButtonLink, Table, TBody, Td, Tr } from "./index";

afterEach(() => {
  document.body.innerHTML = "";
});

describe("[REQ-299] links that look like buttons, and the fade on the side that was scrolled past", () => {
  it("renders a link in each variant", async () => {
    const view = await render(
      <>
        <ButtonLink href="#/kgs">KG 목록으로</ButtonLink>
        <ButtonLink href="#/settings" variant="primary">
          접속 설정
        </ButtonLink>
      </>,
    );
    const links = view.container.querySelectorAll("a");
    expect([links[0]?.getAttribute("href"), links[1]?.className]).toEqual([
      "#/kgs",
      expect.stringContaining("bg-accent"),
    ]);
    await view.unmount();
  });

  it("fades the start of the table once it is scrolled, and the end when it is scrolled to the end", async () => {
    Object.defineProperty(HTMLElement.prototype, "scrollWidth", {
      configurable: true,
      get: () => 900,
    });
    Object.defineProperty(HTMLElement.prototype, "clientWidth", {
      configurable: true,
      get: () => 300,
    });
    const view = await render(
      <Table aria-label="t">
        <TBody>
          <Tr>
            <Td>x</Td>
          </Tr>
        </TBody>
      </Table>,
    );
    const frame = view.container.querySelector(".overflow-x-auto") as HTMLElement;
    frame.scrollLeft = 600;
    await act(async () => frame.dispatchEvent(new Event("scroll", { bubbles: true })));
    expect(view.container.querySelectorAll("[aria-hidden]")).toHaveLength(1);
    expect(view.container.querySelector("[aria-hidden]")?.className).toContain("left-0");
    frame.scrollLeft = 100;
    await act(async () => frame.dispatchEvent(new Event("scroll", { bubbles: true })));
    expect(view.container.querySelectorAll("[aria-hidden]")).toHaveLength(2);
    await view.unmount();
  });
});
