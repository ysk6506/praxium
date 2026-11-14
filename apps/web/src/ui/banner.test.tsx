// @vitest-environment happy-dom

import { afterEach, describe, expect, it } from "vitest";
import { render } from "../test-support/render";
import { AppShell } from "./index";

afterEach(() => {
  document.body.innerHTML = "";
});

describe("[REQ-281] the frame shows a banner above everything when it is given one", () => {
  it("puts the banner in a status element and leaves it out when there is none", async () => {
    const withBanner = await render(
      <AppShell brand="프락시움" nav={null} banner="데모 모드">
        <p>본문</p>
      </AppShell>,
    );
    const status = withBanner.container.querySelector("output");
    expect(status?.textContent).toContain("데모 모드");
    expect(withBanner.container.textContent).toContain("본문");
    await withBanner.unmount();
    const without = await render(
      <AppShell brand="프락시움" nav={null}>
        <p>본문</p>
      </AppShell>,
    );
    expect(without.container.querySelector("output")).toBeNull();
    await without.unmount();
  });
});
