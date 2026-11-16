import { describe, expect, it } from "vitest";
import { fakeApi } from "../test-support/fake-api";
import { createClient, type Requester, type RequestSpec, requestHeaders, withEtag } from "./client";
import { createUser, disableUser, getKG, listKGs } from "./generated/operations";
import { generateOperations } from "./generator/operations";
import type { OpenApiDocument } from "./generator/spec";

const contract: OpenApiDocument = {
  paths: {
    "/v1/things/{id}": {
      patch: {
        operationId: "changeThing",
        parameters: [
          { name: "id", in: "path", required: true, schema: { type: "string" } },
          { $ref: "#/components/parameters/IfMatch" },
          { $ref: "#/components/parameters/Key" },
        ],
        requestBody: {
          required: true,
          content: { "application/json": { schema: { type: "object" } } },
        },
        responses: {},
      },
      get: {
        operationId: "readThing",
        parameters: [{ $ref: "#/components/parameters/IfNoneMatch" }],
        responses: {},
      },
    },
  },
  components: {
    parameters: {
      IfMatch: { name: "If-Match", in: "header", required: true, schema: { type: "string" } },
      Key: { name: "Idempotency-Key", in: "header", schema: { type: "string" } },
      IfNoneMatch: { name: "If-None-Match", in: "header", schema: { type: "string" } },
    },
  },
};

describe("[REQ-407] the generator makes the header parameters of the contract arguments", () => {
  const text = generateOperations(contract);

  it("writes the headers by their names, optional unless the contract requires one", () => {
    expect(text).toContain('headers: { "If-Match": string; "Idempotency-Key"?: string; };');
    expect(text).toContain('headers?: { "If-None-Match"?: string; };');
  });

  it("passes the headers on to the request", () => {
    expect(text).toContain("headers: args.headers");
  });

  it("makes the arguments required when a header is required, and optional when nothing is", () => {
    expect(text).toContain("args: ChangeThingArgs)");
    expect(text).toContain("args: ReadThingArgs = {}");
    const onlyOptional = generateOperations({
      paths: {
        "/v1/x": {
          get: {
            operationId: "listX",
            parameters: [{ $ref: "#/components/parameters/IfNoneMatch" }],
            responses: {},
          },
        },
      },
      components: contract.components,
    });
    expect(onlyOptional).toContain("args: ListXArgs = {}");
  });
});

describe("[REQ-407] the generated functions send the headers they are given", () => {
  const recording = (): { client: Requester; seen: RequestSpec[] } => {
    const seen: RequestSpec[] = [];
    return {
      seen,
      client: {
        async request<T>(spec: RequestSpec): Promise<T> {
          seen.push(spec);
          return undefined as T;
        },
      },
    };
  };

  it("hands the headers of the arguments to the request", async () => {
    const { client, seen } = recording();
    await createUser(client, {
      headers: { "Idempotency-Key": "px-key-0001" },
      body: { name: "a", roles: [] },
    });
    await disableUser(client, {
      path: { user_id: "u1" },
      headers: { "Idempotency-Key": "px-key-0002" },
    });
    await listKGs(client, { headers: { "If-None-Match": '"v1"' } });
    expect(seen.map((s) => s.headers)).toEqual([
      { "Idempotency-Key": "px-key-0001" },
      { "Idempotency-Key": "px-key-0002" },
      { "If-None-Match": '"v1"' },
    ]);
  });

  it("still takes no arguments for a read that has none", async () => {
    const { client, seen } = recording();
    await listKGs(client);
    expect(seen[0]?.headers).toBeUndefined();
  });

  it("sends them as headers of the HTTP request, next to the token and the JSON body", async () => {
    const api = fakeApi({ "POST /v1/users": { status: 201, body: { data: {} } } });
    const client = createClient({ token: "tok" });
    await createUser(client, {
      headers: { "Idempotency-Key": "px-key-0003" },
      body: { name: "n", roles: [] },
    });
    api.restore();
    expect(api.asked).toHaveLength(1);
    expect(api.asked[0]?.headers["idempotency-key"]).toBe("px-key-0003");
    expect(api.asked[0]?.headers.authorization).toBe("Bearer tok");
    expect(api.asked[0]?.headers["content-type"]).toBe("application/json");
    expect(api.asked[0]?.body).toEqual({ name: "n", roles: [] });
  });
});

describe("[REQ-407] the client puts the headers on the request without letting them take its own", () => {
  it("leaves out a header with no value", () => {
    const headers = requestHeaders(
      { method: "PATCH", path: "/x", headers: { "If-Match": undefined, "Idempotency-Key": "k" } },
      undefined,
    );
    expect(headers).toEqual({ Accept: "application/json", "Idempotency-Key": "k" });
  });

  it("does not let a header argument replace the token, the media types or their case", () => {
    const headers = requestHeaders(
      {
        method: "POST",
        path: "/x",
        body: {},
        headers: {
          authorization: "Bearer other",
          "content-type": "text/plain",
          ACCEPT: "text/html",
          "If-Match": '"v2"',
        },
      },
      "tok",
    );
    expect(headers).toEqual({
      "If-Match": '"v2"',
      Accept: "application/json",
      Authorization: "Bearer tok",
      "Content-Type": "application/json",
    });
  });
});

describe("[REQ-407] the ETag of an answer comes back beside its body", () => {
  it("returns the ETag the answer carried and leaves the body as it was", async () => {
    const api = fakeApi({
      "GET /v1/kgs/main": { body: { data: { name: "main" } }, headers: { ETag: '"rev-17"' } },
    });
    const result = await withEtag(createClient(), (client) =>
      getKG(client, { path: { kg: "main" } }),
    );
    api.restore();
    expect(result.etag).toBe('"rev-17"');
    expect(result.data).toEqual({ data: { name: "main" } });
  });

  it("returns no ETag when the answer has none, and when the answer is a 304", async () => {
    const api = fakeApi({
      "GET /v1/kgs/main": { body: { data: {} } },
      "GET /v1/kgs/other": { status: 304, headers: { ETag: '"same"' } },
    });
    const none = await withEtag(createClient(), (c) => getKG(c, { path: { kg: "main" } }));
    expect(none.etag).toBeUndefined();
    const notModified = await withEtag(createClient(), (c) =>
      getKG(c, { path: { kg: "other" }, headers: { "If-None-Match": '"same"' } }),
    );
    api.restore();
    expect(notModified.data).toBeUndefined();
    expect(notModified.etag).toBe('"same"');
    expect(api.asked[1]?.headers["if-none-match"]).toBe('"same"');
  });

  it("keeps the failure of the call, and tells an outer listener too", async () => {
    const api = fakeApi({});
    await expect(
      withEtag(createClient(), (c) => getKG(c, { path: { kg: "nope" } })),
    ).rejects.toMatchObject({ status: 404 });
    api.restore();
    const heard: (string | undefined)[] = [];
    const api2 = fakeApi({ "GET /v1/kgs/a": { body: {}, headers: { ETag: '"e"' } } });
    await withEtag(createClient(), (c) =>
      c.request({
        method: "GET",
        path: "/v1/kgs/a",
        onResponse: (meta) => heard.push(meta.etag),
      }),
    );
    api2.restore();
    expect(heard).toEqual(['"e"']);
  });
});
