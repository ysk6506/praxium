import { describe, expect, test } from "vitest";
import {
  API_CONFIG,
  apiRepo,
  FEATURE_TABLE,
  healthyContract,
  PLANNED,
} from "../test-support/api-fixture";
import { checkRoutes, readPlanned, readTable } from "./api-routes";

const run = (files: Record<string, string> = {}) =>
  checkRoutes(apiRepo(files), API_CONFIG, healthyContract());
const codes = (files: Record<string, string> = {}): string[] =>
  [...new Set(run(files).map((v) => v.code))].sort();
const planned = (routes: object[]): string => JSON.stringify({ version: 1, routes });
const entry = (over: object = {}): object => ({
  method: "GET",
  path: "/v1/later",
  feature: "x",
  adr: "ADR-0001",
  phase: "alpha-2",
  status: "planned",
  ...over,
});

describe("the table, the contract and the planned registry agree [REQ-143]", () => {
  test("consistent files pass", () => {
    expect(run()).toEqual([]);
  });

  test("the table is read from its second column: absolute spans only", () => {
    const table = readTable(FEATURE_TABLE);
    expect(table.problems).toEqual([]);
    expect(table.routes.map((r) => `${r.method} ${r.path}`)).toEqual([
      "GET /v1/things",
      "PATCH /v1/things/{thing_id}",
      "POST /v1/things/{thing_id}:approve",
      "GET /v1/health/live",
      "POST /v1/kgs/{kg}/questions/{question_name}",
      "GET /v1/later",
      "POST /v1/later",
    ]);
    expect(
      readTable(
        "| 기능 | 길 |\n|---|---|\n| a `GET /v1/x` | prose `GET /v1/y?since=` and `meta.x` |",
      ).routes,
    ).toEqual([{ method: "GET", path: "/v1/y", line: 3 }]);
  });

  test.each([
    ["a shorthand span", "`GET /v1/things`, `.../{id}`"],
    ["a bare path", "`GET /v1/things`, `/v1/other`"],
    ["a lone action", "`GET /v1/things`, `:approve`"],
    ["a span with a method and no path", "`GET`"],
  ])("API045: %s", (_name, cell) => {
    const table = FEATURE_TABLE.replace(
      "`GET, POST /v1/later`",
      `${cell}, \`GET, POST /v1/later\``,
    );
    expect(codes({ [API_CONFIG.feature_routes]: table })).toContain("API045");
  });

  test("API040: a route of the table that is neither specified nor planned", () => {
    expect(
      codes({
        [API_CONFIG.feature_routes]: FEATURE_TABLE.replace(
          "GET, POST /v1/later",
          "GET, POST /v1/later`, `GET /v1/lost",
        ),
      }),
    ).toEqual(["API040"]);
  });

  test("API041: a specified operation that is not in the table", () => {
    expect(
      codes({ [API_CONFIG.feature_routes]: FEATURE_TABLE.replace("`GET /v1/things`, ", "") }),
    ).toEqual(["API041"]);
  });

  test("API042: a planned route that is not in the table", () => {
    expect(
      codes({
        [API_CONFIG.planned]: planned([
          entry(),
          entry({ method: "POST" }),
          entry({ path: "/v1/ghost" }),
        ]),
      }),
    ).toEqual(["API042"]);
  });

  test("API043: a route that is both specified and planned", () => {
    expect(
      codes({
        [API_CONFIG.planned]: planned([
          entry({ path: "/v1/things" }),
          entry(),
          entry({ method: "POST" }),
        ]),
      }),
    ).toEqual(["API043"]);
  });

  test.each([
    ["a repeated route", [entry(), entry(), entry({ method: "POST" })]],
    ["an unknown phase", [entry({ phase: "someday" }), entry({ method: "POST" })]],
    ["a status other than planned", [entry({ status: "done" }), entry({ method: "POST" })]],
    ["an ADR that does not exist", [entry({ adr: "ADR-9999" }), entry({ method: "POST" })]],
    ["no feature", [entry({ feature: "" }), entry({ method: "POST" })]],
    ["a bad method", [entry({ method: "FETCH" }), entry({ method: "POST" })]],
  ])("API044: %s", (_name, routes) => {
    expect(codes({ [API_CONFIG.planned]: planned(routes) })).toContain("API044");
  });

  test("API044: a missing or broken registry, API040: a missing table", () => {
    expect(codes({ [API_CONFIG.planned]: "not json" })).toEqual(["API044"]);
    expect(
      checkRoutes(apiRepo(), { ...API_CONFIG, planned: "nowhere.json" }, healthyContract())[0]
        ?.code,
    ).toBe("API044");
    expect(
      checkRoutes(apiRepo(), { ...API_CONFIG, feature_routes: "nowhere.md" }, healthyContract())[0]
        ?.code,
    ).toBe("API040");
    expect(
      checkRoutes(apiRepo(), { ...API_CONFIG, adr_dir: "nowhere" }, healthyContract()).map(
        (v) => v.code,
      ),
    ).toContain("API044");
  });

  test("the registry text is read leniently", () => {
    expect(readPlanned(PLANNED)?.length).toBe(2);
    expect(readPlanned('{"routes": [{}]}')).toEqual([
      { method: "", path: "", feature: "", adr: "", phase: "", status: "" },
    ]);
    expect(readPlanned("{}")).toEqual([]);
    expect(readPlanned("{")).toBeNull();
  });
});
