import { describe, expect, it } from "vitest";
import { ApiError, buildUrl, CLIENT_CODES, createClient, serializeQuery } from "./client";

interface Seen {
  url: string;
  init: RequestInit;
}

function fakeFetch(respond: () => Response | Promise<Response>): {
  fetch: typeof fetch;
  seen: Seen[];
} {
  const seen: Seen[] = [];
  const impl = async (input: RequestInfo | URL, init?: RequestInit): Promise<Response> => {
    seen.push({ url: String(input), init: init ?? {} });
    return respond();
  };
  return { fetch: impl as typeof fetch, seen };
}

const json = (body: unknown, status = 200, type = "application/json"): Response =>
  new Response(JSON.stringify(body), { status, headers: { "Content-Type": type } });

describe("[REQ-278] the client builds the request the contract describes", () => {
  it("fills and escapes path parameters", () => {
    const url = buildUrl("", {
      method: "GET",
      path: "/v1/kgs/{kg}/objects/{id}",
      pathParams: { kg: "main", id: "a/b c" },
    });
    expect(url).toBe("/v1/kgs/main/objects/a%2Fb%20c");
  });

  it("joins arrays with commas and writes objects in the deepObject style, leaving out what is not set", () => {
    expect(
      serializeQuery({
        limit: 5,
        cursor: undefined,
        shape: null,
        include: ["a", "b"],
        filter: { name: { equals: "x y" }, role: { in: ["p", "q"] } },
        flag: true,
        none: [],
      }),
    ).toBe("limit=5&include=a,b&filter[name][equals]=x%20y&filter[role][in]=p,q&flag=true");
    expect(serializeQuery(undefined)).toBe("");
  });

  it("appends the query to a base URL without doubling the slash", () => {
    expect(buildUrl("http://host/", { method: "GET", path: "/v1/kgs", query: { limit: 2 } })).toBe(
      "http://host/v1/kgs?limit=2",
    );
  });

  it("sends the token as a bearer header and a body as JSON", async () => {
    const { fetch, seen } = fakeFetch(() => json({ ok: true }));
    const client = createClient({ token: "t0ken", fetch });
    const answer = await client.request<{ ok: boolean }>({
      method: "POST",
      path: "/v1/x",
      body: { a: 1 },
    });
    expect(answer).toEqual({ ok: true });
    const headers = seen[0]?.init.headers as Record<string, string>;
    expect(headers.Authorization).toBe("Bearer t0ken");
    expect(headers.Accept).toBe("application/json");
    expect(headers["Content-Type"]).toBe("application/json");
    expect(seen[0]?.init.body).toBe('{"a":1}');
  });

  it("sends no credentials without a token", async () => {
    const { fetch, seen } = fakeFetch(() => json({}));
    await createClient({ fetch }).request({ method: "GET", path: "/v1/x" });
    expect((seen[0]?.init.headers as Record<string, string>).Authorization).toBeUndefined();
  });

  it("answers undefined to a 204 and a 304", async () => {
    for (const status of [204, 304]) {
      const { fetch } = fakeFetch(() => new Response(null, { status }));
      expect(
        await createClient({ fetch }).request({ method: "GET", path: "/v1/x" }),
      ).toBeUndefined();
    }
  });
});

describe("[REQ-278] the client turns every failure into an ApiError", () => {
  it("reads a problem answer", async () => {
    const problem = {
      type: "/v1/problems/auth.token_missing",
      title: "Unauthorized",
      status: 401,
      code: "auth.token_missing",
      trace_id: "tr-1",
      params: { why: "none" },
      errors: [{ pointer: "/a", code: "invalid_input.field", params: {} }],
    };
    const { fetch } = fakeFetch(() => json(problem, 401, "application/problem+json"));
    const error = await createClient({ fetch })
      .request({ method: "GET", path: "/v1/x" })
      .catch((e: unknown) => e);
    expect(error).toBeInstanceOf(ApiError);
    const api = error as ApiError;
    expect([api.status, api.code, api.title, api.traceId]).toEqual([
      401,
      "auth.token_missing",
      "Unauthorized",
      "tr-1",
    ]);
    expect(api.params).toEqual({ why: "none" });
    expect(api.errors).toHaveLength(1);
  });

  it("names an answer that is not a problem", async () => {
    const { fetch } = fakeFetch(
      () => new Response("<html>bad gateway</html>", { status: 502, statusText: "Bad Gateway" }),
    );
    const error = (await createClient({ fetch })
      .request({ method: "GET", path: "/v1/x" })
      .catch((e: unknown) => e)) as ApiError;
    expect([error.status, error.code, error.title]).toEqual([
      502,
      CLIENT_CODES.badResponse,
      "Bad Gateway",
    ]);
  });

  it("names a success whose body is not JSON", async () => {
    const { fetch } = fakeFetch(() => new Response("plain", { status: 200 }));
    const error = (await createClient({ fetch })
      .request({ method: "GET", path: "/v1/x" })
      .catch((e: unknown) => e)) as ApiError;
    expect(error.code).toBe(CLIENT_CODES.badResponse);
  });

  it("names a request that never reached the API", async () => {
    const { fetch } = fakeFetch(() => {
      throw new TypeError("failed to fetch");
    });
    const error = (await createClient({ fetch })
      .request({ method: "GET", path: "/v1/x" })
      .catch((e: unknown) => e)) as ApiError;
    expect([error.status, error.code]).toEqual([0, CLIENT_CODES.network]);
  });

  it("lets an abort through as it is", async () => {
    const { fetch } = fakeFetch(() => {
      throw new DOMException("aborted", "AbortError");
    });
    const error = await createClient({ fetch })
      .request({ method: "GET", path: "/v1/x" })
      .catch((e: unknown) => e);
    expect(error).toBeInstanceOf(DOMException);
  });
});
