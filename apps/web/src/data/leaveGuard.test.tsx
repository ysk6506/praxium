// @vitest-environment happy-dom

import { act } from "react";
import { afterEach, beforeEach, describe, expect, it } from "vitest";
import { renderHook } from "../test-support/render";
import { useLeaveGuard } from "./leaveGuard";

/** Go to another address the way a link does: set the hash, and let the browser tell the page. */
async function goTo(hash: string): Promise<void> {
  await act(async () => {
    location.hash = hash;
    await new Promise((resolve) => setTimeout(resolve, 0));
  });
}

const flush = (): Promise<void> => new Promise((resolve) => setTimeout(resolve, 0));

// The test document tells the page about every change of the hash, a browser only about a person's; so each test
// starts after the events of its own setup have gone by.
beforeEach(async () => {
  history.replaceState(null, "", `${location.pathname}#/kg/main/types`);
  await flush();
});

afterEach(async () => {
  history.replaceState(null, "", location.pathname);
  await flush();
});

function unloading(): { prevented: boolean } {
  const event = new Event("beforeunload", { cancelable: true });
  window.dispatchEvent(event);
  return { prevented: event.defaultPrevented };
}

describe("[REQ-409] leaving with changes that are not saved is asked about", () => {
  it("asks the browser before the tab closes only while there are changes", async () => {
    let dirty = false;
    const hook = await renderHook(() => useLeaveGuard(dirty));
    expect(unloading().prevented).toBe(false);
    dirty = true;
    await hook.rerender();
    expect(unloading().prevented).toBe(true);
    dirty = false;
    await hook.rerender();
    expect(unloading().prevented).toBe(false);
    dirty = true;
    await hook.rerender();
    await hook.unmount();
    expect(unloading().prevented).toBe(false);
  });

  it("puts the address back when the person goes to another screen, and waits for the choice", async () => {
    const hook = await renderHook(() => useLeaveGuard(true));
    await goTo("#/kg/main/impact");
    expect(location.hash).toBe("#/kg/main/types");
    expect(hook.result.current.pending?.target).toBe("#/kg/main/impact");
    await hook.unmount();
  });

  it("goes where the person was going when they choose to leave", async () => {
    const hook = await renderHook(() => useLeaveGuard(true));
    await goTo("#/kg/main/impact");
    await act(async () => hook.result.current.pending?.leave());
    expect(location.hash).toBe("#/kg/main/impact");
    expect(hook.result.current.pending).toBeUndefined();
    await hook.unmount();
  });

  it("stays on the screen when the person chooses to stay", async () => {
    const hook = await renderHook(() => useLeaveGuard(true));
    await goTo("#/kg/main/impact");
    await act(async () => hook.result.current.pending?.stay());
    expect(location.hash).toBe("#/kg/main/types");
    expect(hook.result.current.pending).toBeUndefined();
    await goTo("#/settings");
    expect(hook.result.current.pending?.target).toBe("#/settings");
    await hook.unmount();
  });

  it("does not ask for a change of the query of the same screen, nor when nothing is changed", async () => {
    let dirty = true;
    const hook = await renderHook(() => useLeaveGuard(dirty));
    await goTo("#/kg/main/types?sort=name");
    expect(hook.result.current.pending).toBeUndefined();
    expect(location.hash).toBe("#/kg/main/types?sort=name");
    dirty = false;
    await hook.rerender();
    await goTo("#/kg/main/impact");
    expect(hook.result.current.pending).toBeUndefined();
    expect(location.hash).toBe("#/kg/main/impact");
    await hook.unmount();
  });

  it("forgets a question when the changes are saved meanwhile", async () => {
    let dirty = true;
    const hook = await renderHook(() => useLeaveGuard(dirty));
    await goTo("#/settings");
    expect(hook.result.current.pending).toBeDefined();
    dirty = false;
    await hook.rerender();
    expect(hook.result.current.pending).toBeUndefined();
    await hook.unmount();
  });
});
