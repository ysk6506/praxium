// @vitest-environment happy-dom

import { afterEach, describe, expect, it, vi } from "vitest";
import { render } from "../test-support/render";
import { Table, TBody, Td, Tr } from "./index";

afterEach(() => {
  document.body.innerHTML = "";
  vi.unstubAllGlobals();
});

describe("[REQ-299] a table wider than its frame says so", () => {
  it("shows a fade and a line under the table only when there is more to scroll to", async () => {
    let observed: (() => void) | undefined;
    vi.stubGlobal(
      "ResizeObserver",
      class {
        constructor(callback: () => void) {
          observed = callback;
        }
        observe() {}
        disconnect() {}
      },
    );
    const sizes = { scrollWidth: 900, clientWidth: 300 };
    Object.defineProperty(HTMLElement.prototype, "scrollWidth", {
      configurable: true,
      get: () => sizes.scrollWidth,
    });
    Object.defineProperty(HTMLElement.prototype, "clientWidth", {
      configurable: true,
      get: () => sizes.clientWidth,
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
    expect(view.container.textContent).toContain("가로로 스크롤할 수 있습니다");
    expect(view.container.querySelectorAll("[aria-hidden]")).toHaveLength(1);
    const frame = view.container.querySelector(".overflow-x-auto") as HTMLElement;
    frame.scrollLeft = 200;
    frame.dispatchEvent(new Event("scroll", { bubbles: true }));
    sizes.scrollWidth = 300;
    observed?.();
    await view.rerender(
      <Table aria-label="t">
        <TBody>
          <Tr>
            <Td>x</Td>
          </Tr>
        </TBody>
      </Table>,
    );
    await view.unmount();
  });
});
