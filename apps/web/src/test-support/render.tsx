/** Rendering for tests: React in a happy-dom document, with `act` around every change. */
import { act, type ReactElement } from "react";
import { createRoot, type Root } from "react-dom/client";

(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;

export interface Rendered {
  readonly container: HTMLElement;
  rerender(node: ReactElement): Promise<void>;
  unmount(): Promise<void>;
}

export async function render(node: ReactElement): Promise<Rendered> {
  const container = document.createElement("div");
  document.body.appendChild(container);
  const root: Root = createRoot(container);
  await act(async () => root.render(node));
  return {
    container,
    rerender: async (next) => {
      await act(async () => root.render(next));
    },
    unmount: async () => {
      await act(async () => root.unmount());
      container.remove();
    },
  };
}

export interface HookResult<T> {
  readonly result: { current: T };
  rerender(): Promise<void>;
  unmount(): Promise<void>;
}

export async function renderHook<T>(hook: () => T): Promise<HookResult<T>> {
  const result = { current: undefined as T };
  function Probe(): null {
    result.current = hook();
    return null;
  }
  const rendered = await render(<Probe />);
  return { result, rerender: () => rendered.rerender(<Probe />), unmount: rendered.unmount };
}

/** Let promises settle: the answers of the fake API arrive after a few turns. */
export async function settle(times = 5): Promise<void> {
  for (let i = 0; i < times; i++)
    await act(async () => new Promise((resolve) => setTimeout(resolve, 0)));
}

/** Click, with the change flushed. */
export async function click(element: Element): Promise<void> {
  await act(async () => {
    element.dispatchEvent(new MouseEvent("click", { bubbles: true, cancelable: true }));
  });
}
