import { describe, expect, test } from "vitest";
import { API_CONFIG, healthyContract } from "../test-support/api-fixture";
import { checkServer, readServed, type ServedRoute } from "./api-server";

const doc = healthyContract();
const SERVED: ServedRoute[] = [
  { method: "GET", path: "/v1/things", roles: ["read"] },
  { method: "GET", path: "/v1/health/live", roles: ["anonymous"] },
  { method: "PATCH", path: "/v1/things/{thing_id}", roles: ["read"] },
  { method: "POST", path: "/v1/things/{thing_id}:approve", roles: ["read"] },
  { method: "POST", path: "/v1/kgs/{kg}/questions/{question_name}", roles: ["read"] },
];

const codes = (routes: ServedRoute[]): string[] =>
  checkServer(API_CONFIG, doc, () => ({ routes })).map((v) => v.code);

describe("the server serves exactly what the contract says [REQ-143]", () => {
  test("equal routes and roles pass", () => {
    expect(codes(SERVED)).toEqual([]);
  });

  test("a route only the contract has, only the server has, or with other roles fails", () => {
    expect(codes(SERVED.slice(1))).toEqual(["API050"]);
    expect(codes([...SERVED, { method: "GET", path: "/v1/extra", roles: ["read"] }])).toEqual([
      "API050",
    ]);
    expect(
      codes([{ method: "GET", path: "/v1/things", roles: ["admin"] }, ...SERVED.slice(1)]),
    ).toEqual(["API052"]);
    const [shown] = checkServer(API_CONFIG, doc, () => ({ routes: SERVED.slice(1) }));
    expect(shown?.message).toContain("GET /v1/things");
  });

  test("a server whose routes cannot be listed fails", () => {
    const [found] = checkServer(API_CONFIG, doc, () => ({ error: "go: no such file" }));
    expect(found?.code).toBe("API051");
    expect(found?.message).toContain("no such file");
  });

  test("the output of the routes command is read", () => {
    const text = JSON.stringify({
      routes: [{ method: "GET", path: "/v1/x", roles: ["read"] }, {}],
    });
    expect(readServed(text)).toEqual([
      { method: "GET", path: "/v1/x", roles: ["read"] },
      { method: "", path: "", roles: [] },
    ]);
    expect(readServed("{}")).toBeNull();
    expect(readServed("nope")).toBeNull();
  });
});
