// @vitest-environment happy-dom
import { describe, expect, it } from "vitest";
import { createClient, createUser, getKG, withEtag } from "../api";
import { fakeApi } from "../test-support/fake-api";

describe("[REQ-407] headers on the wire", () => {
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
