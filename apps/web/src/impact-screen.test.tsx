// @vitest-environment happy-dom

import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { App } from "./App";
import { connection } from "./data";

(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;

let root: Root | undefined;
let host: HTMLElement;

async function open(hash: string): Promise<void> {
  globalThis.location.hash = hash;
  host = document.createElement("div");
  document.body.appendChild(host);
  root = createRoot(host);
  await act(async () => root?.render(<App />));
  await settle();
}

async function settle(times = 8): Promise<void> {
  for (let i = 0; i < times; i++)
    await act(async () => new Promise((resolve) => setTimeout(resolve, 0)));
}

const text = (): string => host.textContent ?? "";

beforeEach(() => {
  vi.stubGlobal("fetch", () => Promise.resolve(new Response("{}", { status: 404 })));
  connection.setToken("tok");
});

afterEach(async () => {
  await act(async () => root?.unmount());
  root = undefined;
  host?.remove();
  vi.unstubAllGlobals();
  connection.clear();
  globalThis.location.hash = "";
});

describe("[REQ-381] the words of the screen", () => {
  it("asks for an access token on the connection screen and says where to get it", async () => {
    await open("#/settings");
    expect(text()).toContain("접근 토큰");
    expect(text()).not.toContain("개발용 토큰");
    for (const where of ["로컬 모드", "개발 서버", "운영 서버"]) expect(text()).toContain(where);
    expect(text()).toContain("praxium admin token create");
  });
});
