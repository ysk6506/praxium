import { describe, expect, it } from "vitest";
import type { ImpactData } from "../api";
import { type ImpactDataInput, impactSource } from "./impact-input";

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
});
