/** A fake API for tests: answers by `METHOD path`, records what was asked. Installed as the global `fetch`. */
import { vi } from "vitest";

export interface Asked {
  readonly method: string;
  readonly path: string;
  readonly search: string;
  readonly body: unknown;
  readonly authorization: string | null;
}

export interface Reply {
  status?: number;
  body?: unknown;
  problem?: boolean;
}
export type Answer = Reply | (() => Reply);

export function fakeApi(routes: Record<string, Answer>): { asked: Asked[]; restore: () => void } {
  const asked: Asked[] = [];
  vi.stubGlobal(
    "fetch",
    async (input: RequestInfo | URL, init?: RequestInit): Promise<Response> => {
      const url = new URL(String(input), "http://fake");
      const method = init?.method ?? "GET";
      const headers = new Headers(init?.headers);
      asked.push({
        method,
        path: url.pathname,
        search: decodeURIComponent(url.search),
        body: typeof init?.body === "string" ? JSON.parse(init.body) : undefined,
        authorization: headers.get("Authorization"),
      });
      let answer = routes[`${method} ${url.pathname}`];
      if (typeof answer === "function") answer = answer();
      if (answer === undefined) {
        return new Response(
          JSON.stringify({
            code: "not_found.resource",
            title: "Not found",
            status: 404,
            type: "x",
            trace_id: "tr-t",
          }),
          {
            status: 404,
            headers: { "Content-Type": "application/problem+json" },
          },
        );
      }
      const type = answer.problem ? "application/problem+json" : "application/json";
      return new Response(JSON.stringify(answer.body), {
        status: answer.status ?? 200,
        headers: { "Content-Type": type },
      });
    },
  );
  return { asked, restore: () => vi.unstubAllGlobals() };
}

export const problem = (status: number, code: string, title = "Problem"): Reply => ({
  status,
  problem: true,
  body: { type: `/v1/problems/${code}`, title, status, code, trace_id: "tr-test" },
});
