// @vitest-environment happy-dom
import { act } from "react";
import { afterEach, describe, expect, it } from "vitest";
import { connection } from "../data";
import { render, settle } from "../test-support/render";
import { SettingsScreen } from "./SettingsScreen";

afterEach(() => {
  connection.clear();
  document.body.innerHTML = "";
});

const box = (root: ParentNode): HTMLInputElement =>
  root.querySelector('input[type="password"]') as HTMLInputElement;

async function typeInto(element: HTMLInputElement, text: string): Promise<void> {
  await act(async () => {
    Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, "value")?.set?.call(element, text);
    element.dispatchEvent(new Event("input", { bubbles: true }));
  });
}

async function submit(root: ParentNode): Promise<void> {
  await act(async () => {
    (root.querySelector("form") as HTMLFormElement).dispatchEvent(
      new Event("submit", { bubbles: true, cancelable: true }),
    );
  });
  await settle();
}

describe("the connection settings form", () => {
  it("has a submit button that is not turned off before the person submits", async () => {
    const view = await render(<SettingsScreen />);
    const button = [...view.container.querySelectorAll("button")].find(
      (b) => b.textContent === "저장하고 접속",
    );
    expect(button?.hasAttribute("disabled")).toBe(false);
    await view.unmount();
  });

  it("says in words that the token is missing, and puts focus in the box", async () => {
    const view = await render(<SettingsScreen />);
    await submit(view.container);
    expect(box(view.container).getAttribute("aria-invalid")).toBe("true");
    expect(view.container.querySelector('[role="alert"]')?.textContent).toContain("입력하십시오");
    expect(document.activeElement).toBe(box(view.container));
    expect(connection.getToken()).toBe("");
    await typeInto(box(view.container), "a");
    expect(box(view.container).getAttribute("aria-invalid")).not.toBe("true");
    await view.unmount();
  });

  it("keeps the typed token in the browser as before and goes to the list", async () => {
    const view = await render(<SettingsScreen />);
    await typeInto(box(view.container), "  typed-token  ");
    await submit(view.container);
    expect(connection.getToken()).toBe("typed-token");
    expect(location.hash).toBe("#/kgs");
    expect(box(view.container).value).toBe("");
    await view.unmount();
  });
});
