import { describe, expect, it } from "vitest";
import {
  DEFAULT_IMPACT_VIEW,
  EMPTY_QUESTION,
  type ImpactView,
  impactAddress,
  impactViewParams,
  isAnswerable,
  itemsRequest,
  MAX_ADDRESS_IDS,
  readImpactAddress,
  readImpactView,
  summaryRequest,
} from "./impact-view";

const obj = (n: number): string => `obj_${n.toString(16).padStart(32, "0")}`;
const lnk = (n: number): string => `lnk_${n.toString(16).padStart(32, "0")}`;

const read = (query: string): ImpactView => readImpactView(new URLSearchParams(query));

describe("[REQ-353] the state of the impact screen is in the address", () => {
  it("writes nothing for the default view, so the plain address is the default screen", () => {
    expect(impactViewParams(DEFAULT_IMPACT_VIEW)).toEqual(
      Object.fromEntries(
        Object.keys(impactViewParams(DEFAULT_IMPACT_VIEW)).map((k) => [k, undefined]),
      ),
    );
    expect(impactAddress("main", DEFAULT_IMPACT_VIEW)).toBe("#/kg/main/impact");
    expect(read("")).toEqual(DEFAULT_IMPACT_VIEW);
  });

  it("reads back what it wrote, for views that use every part of the address", () => {
    const views: ImpactView[] = [
      {
        question: { dead: [obj(1), obj(2)], site: [obj(3)], cut: [lnk(4)], target: undefined },
        reading: "with_assumptions",
        hops: 4,
        max: 5000,
        view: "list",
        focus: "/PowerFeed/NetworkSwitch",
        selected: obj(9),
        step: 0,
        all: true,
        sort: { key: "hops", direction: "descending" },
        page: 3,
        grade: "exposed_direct",
      },
      { ...DEFAULT_IMPACT_VIEW, question: { ...EMPTY_QUESTION, target: obj(7) }, step: 2 },
      { ...DEFAULT_IMPACT_VIEW, max: 500, sort: { key: "name", direction: "ascending" } },
    ];
    for (const view of views) {
      const address = impactAddress("my kg/1", view);
      expect(readImpactAddress(address)).toEqual({ kg: "my kg/1", view });
      expect(impactAddress("my kg/1", readImpactAddress(address)?.view ?? view)).toBe(address);
    }
  });

  it("reads a value that does not fit as the default", () => {
    const view = read(
      `dead=${obj(1)},not-an-id,${obj(1)},${obj(2).toUpperCase()}&cut=${obj(5)},${lnk(6)}&site=x` +
        "&reading=wish&hops=0&max=123&view=cards&step=-1&all=yes&sort=color&dir=desc&page=abc&grade=awful&sel=zz&focus=",
    );
    expect(view).toEqual({
      ...DEFAULT_IMPACT_VIEW,
      question: { dead: [obj(1)], site: [], cut: [lnk(6)], target: undefined },
    });
    expect(read("hops=12x&step=1.5&page=0&max=2000").hops).toBeUndefined();
    expect(read("page=0").page).toBe(1);
    expect(read(`focus=${"x".repeat(501)}`).focus).toBeUndefined();
  });

  it("takes the target over the objects that failed, and keeps at most 20 of each list", () => {
    const view = read(`dead=${obj(1)}&target=${obj(2)}&site=${obj(3)}`);
    expect(view.question).toEqual({ ...EMPTY_QUESTION, target: obj(2) });
    expect(
      impactViewParams({
        ...DEFAULT_IMPACT_VIEW,
        question: { dead: [obj(1)], site: [], cut: [], target: obj(2) },
      }).dead,
    ).toBeUndefined();
    const many = Array.from({ length: 30 }, (_, i) => obj(i + 1));
    expect(read(`dead=${many.join(",")}`).question.dead).toHaveLength(MAX_ADDRESS_IDS);
    expect(
      impactViewParams({
        ...DEFAULT_IMPACT_VIEW,
        question: { ...EMPTY_QUESTION, dead: many },
      }).dead?.split(","),
    ).toHaveLength(MAX_ADDRESS_IDS);
  });

  it("knows the address of this screen and no other", () => {
    expect(readImpactAddress("#/kg/main/impact?view=list")?.view.view).toBe("list");
    expect(readImpactAddress("#/kg/main/types")).toBeUndefined();
    expect(readImpactAddress("#/kg/main/impact/extra")).toBeUndefined();
    expect(readImpactAddress("#/kgs")).toBeUndefined();
    expect(readImpactAddress("#/kg/%E0%A4%A/impact")).toBeUndefined();
  });

  it("says whether the question names a start", () => {
    expect(isAnswerable(EMPTY_QUESTION)).toBe(false);
    expect(isAnswerable({ ...EMPTY_QUESTION, dead: [obj(1)] })).toBe(true);
    expect(isAnswerable({ ...EMPTY_QUESTION, site: [obj(1)] })).toBe(true);
    expect(isAnswerable({ ...EMPTY_QUESTION, cut: [lnk(1)] })).toBe(true);
    expect(isAnswerable({ ...EMPTY_QUESTION, target: obj(1) })).toBe(true);
  });
});

describe("[REQ-353] the view makes the two requests of the screen", () => {
  const view: ImpactView = {
    ...DEFAULT_IMPACT_VIEW,
    question: { dead: [obj(1)], site: [obj(2)], cut: [lnk(3)], target: undefined },
    hops: 3,
  };

  it("first asks for the counts of both readings, then for the items of the reading chosen in pages of 1000", () => {
    expect(summaryRequest(view)).toEqual({
      direction: "downstream",
      dead: [obj(1)],
      dead_links: [lnk(3)],
      site_failure: [obj(2)],
      mode: "both",
      shape: "summary",
      max_hops: 3,
      max_items: 2000,
    });
    expect(itemsRequest({ ...view, reading: "with_assumptions" })).toEqual({
      direction: "downstream",
      dead: [obj(1)],
      dead_links: [lnk(3)],
      site_failure: [obj(2)],
      mode: "with_assumptions",
      shape: "items",
      max_hops: 3,
      max_items: 2000,
      limit: 1000,
    });
    expect(itemsRequest(view, "c_next").cursor).toBe("c_next");
    expect(itemsRequest({ ...view, max: 500 }).limit).toBe(500);
  });

  it("asks upstream with the target alone, and leaves the limit of hops out when there is none", () => {
    const up = { ...DEFAULT_IMPACT_VIEW, question: { ...EMPTY_QUESTION, target: obj(8) } };
    expect(summaryRequest(up)).toEqual({
      direction: "upstream",
      target_object_id: obj(8),
      mode: "both",
      shape: "summary",
      max_items: 2000,
    });
    expect(itemsRequest(up)).not.toHaveProperty("max_hops");
  });
});
