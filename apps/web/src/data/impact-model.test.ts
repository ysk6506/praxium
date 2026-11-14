// @vitest-environment happy-dom

import { afterEach, describe, expect, it } from "vitest";
import type { Binding, ObjectTypeSummary } from "../api";
import { fakeApi } from "../test-support/fake-api";
import { renderHook, settle } from "../test-support/render";
import { connection } from "./connection";
import { buildImpactModel, useImpactModel } from "./impact-model";

const type = (
  id: string,
  parent: string | null,
  layer = "facility",
  traits: string[] = [],
): ObjectTypeSummary => ({
  aliases: [],
  description: "",
  display_names: {},
  id: `ot.${id}`,
  layer,
  name: id[0]?.toUpperCase() + id.slice(1),
  namespace: "std",
  parent: parent === null ? null : `ot.${parent}`,
  standard_ref: "",
  status: "active",
  superseded_by: null,
  tier: "core",
  traits,
  type_no: 1,
});

const into = (kind: string, ref: string | null): Binding => ({
  id: "bd",
  cardinality: "N:1",
  param_constraints: {},
  from: { kind: "type", ref: "ot.x" },
  to: { kind, ref },
});

afterEach(() => connection.clear());

describe("[REQ-374] the model says which objects hold others, and no type name is written in the screen", () => {
  const types = [
    type("room", null),
    type("serverRoom", "room"),
    type("rack", null),
    type("zone", null, "facility", ["tr.holder"]),
    type("server", null, "hardware"),
    type("looped", "looped"),
  ];

  it("takes the types at the inside end of a binding, their subtypes, and the types that have a trait at that end", () => {
    const model = buildImpactModel(types, [
      into("type", "ot.room"),
      into("type", "ot.rack"),
      into("trait", "tr.holder"),
      into("type", null),
    ]);
    expect([...model.containers].sort()).toEqual(["Rack", "Room", "ServerRoom", "Zone"]);
    expect(model.containers.has("Server")).toBe(false);
  });

  it("holds nothing when the model has no such binding, and gives the layer of a type by its name", () => {
    const model = buildImpactModel(types, []);
    expect(model.containers.size).toBe(0);
    expect(model.layerOf("Server")).toBe("hardware");
    expect(model.layerOf("Nothing")).toBe("");
  });

  it("is read from the link types of the meaning contained_in and their bindings", async () => {
    const list = (data: unknown[]) => ({
      body: {
        data,
        meta: {
          trace_id: "t",
          result_complete: {
            total: data.length,
            returned: data.length,
            truncated: false,
            reason: null,
            next_cursor: null,
          },
        },
      },
    });
    const api = fakeApi({
      "GET /v1/object-types": list(types),
      "GET /v1/link-types": list([
        { id: "lt.in", meaning: "contained_in" },
        { id: "lt.runs", meaning: "runs_on" },
      ]),
      "GET /v1/link-types/lt.in/bindings": list([into("type", "ot.room")]),
    });
    connection.setToken("t");
    const hook = await renderHook(() => useImpactModel());
    await settle();
    const data = hook.result.current.state.data;
    expect([...(data?.containers ?? [])].sort()).toEqual(["Room", "ServerRoom"]);
    expect(api.asked.map((a) => a.path)).not.toContain("/v1/link-types/lt.runs/bindings");
    await hook.unmount();
    api.restore();
  });
});
