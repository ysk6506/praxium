import { describe, expect, it } from "vitest";
import type { ImpactData } from "../api";
import { type ImpactDataInput, impactSource } from "./impact-input";
import { nodePicture, VIA_HIDDEN } from "./impact-picture";
import { impactRows } from "./impact-rows";
import { buildImpactTree } from "./impact-tree";

/** The answer of the engine, as generated from the contract, must be accepted by the converter as it is. */
describe("impactSource and the generated types", () => {
  it("accepts ImpactData of the contract without conversion", () => {
    const data: ImpactData = {
      computation: {
        complete: true,
        items_over_cap: false,
        max_hops: 8,
        max_items: 2000,
        rounds: 1,
        stale_inputs: 0,
        stopped_by: null,
        time_budget_seconds: 5,
        weak_links: 0,
      },
      direction: "downstream",
      seeds: [{ object_id: "obj_a", object_type: "PowerFeed", display_name: "a" }],
      facts_only: {
        counts_by_parent: [],
        counts_by_result: {},
        counts_by_type: [],
        groups_touched: [],
        unfilled_channel_objects: 0,
        items: [
          {
            also: [],
            assumption_based: false,
            display_name: "b",
            hops: 1,
            lost_channels: [],
            object_id: "obj_b",
            object_type: "NetworkSwitch",
            path: [
              { object_id: "obj_a", object_type: "PowerFeed", display_name: "a" },
              { object_id: "obj_b", object_type: "NetworkSwitch", display_name: "b" },
            ],
            result: "down",
            via: "obj_a",
            weakest_grade: "fact",
          },
        ],
      },
    };
    const input: ImpactDataInput = data;
    const source = impactSource(input, "facts_only");
    expect(source.records).toHaveLength(1);
  });

  it("[REQ-354] carries the weakest evidence, the hidden path and the other paths, and works without them", () => {
    const root = { object_id: "obj_a", object_type: "PowerFeed", display_name: "a" };
    const item = (id: string, extra: object) => ({
      assumption_based: false,
      display_name: id,
      hops: 1,
      object_id: id,
      object_type: "NetworkSwitch",
      path: [root, { object_id: id, object_type: "NetworkSwitch", display_name: id }],
      result: "down",
      via: "obj_a",
      ...extra,
    });
    const input: ImpactDataInput = {
      direction: "downstream",
      seeds: [root],
      facts_only: {
        items: [
          item("obj_plain", {}),
          item("obj_assumed", { assumption_based: true }),
          item("obj_full", {
            weakest_grade: "unknown",
            via_hidden: true,
            also: ["obj_x"],
            lost_channels: ["network"],
            delay_class: "delayed",
          }),
        ],
      },
    };
    const source = impactSource(input, "facts_only");
    const [plain, , full] = source.records;
    expect(plain).toMatchObject({ weakest: undefined, viaHidden: false, also: [], lost: [] });
    expect(full).toMatchObject({
      weakest: "unknown",
      viaHidden: true,
      also: ["obj_x"],
      delayed: true,
    });
    const tree = buildImpactTree(source, () => "network");
    const rows = impactRows(tree);
    expect(rows.find((r) => r.id === "obj_full")).toMatchObject({
      weakest: "unknown",
      viaHidden: true,
      also: ["obj_x"],
    });
    const words = new Map(nodePicture(tree).nodes.map((n) => [n.id, n.description]));
    expect(words.get("obj_plain")).not.toContain("근거");
    expect(words.get("obj_assumed")).toContain("가정에 근거");
    expect(words.get("obj_full")).toContain("근거 알 수 없음");
    expect(words.get("obj_full")).toContain(VIA_HIDDEN);
  });
});
