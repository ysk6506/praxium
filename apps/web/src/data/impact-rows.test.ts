import { describe, expect, it } from "vitest";
import {
  impactOfSize,
  sampleLayerOf,
  smallImpact,
  syntheticImpact,
} from "../test-support/impact-world";
import {
  GRADE_STYLES,
  gradeRank,
  gradeStyle,
  IMPACT_GRADES,
  impactGradeName,
  isImpactGrade,
  START_STYLE,
  sortedGrades,
  WAYPOINT_STYLE,
} from "./impact-grades";
import { impactSource } from "./impact-input";
import { firstRows, impactRows, ROWS_STEP, sectionsByGrade, sortRows } from "./impact-rows";
import { countByGrade, countByStep, describeStep, laneFirstStep, lastStep } from "./impact-stats";
import { buildImpactTree, itemIds } from "./impact-tree";

const treeOf = (data = impactOfSize(2000)) =>
  buildImpactTree(impactSource(data, "with_assumptions"), sampleLayerOf);

describe("[REQ-354] a grade is a shape, a word and a color, never a color alone", () => {
  it("has the seven grades in the order of severity, each with its own shape and word", () => {
    expect(IMPACT_GRADES).toEqual([
      "down",
      "interrupted",
      "unknown",
      "exposed_direct",
      "exposed_inherited",
      "degraded",
      "unmanageable",
    ]);
    const styles = IMPACT_GRADES.map((g) => GRADE_STYLES[g]);
    expect(new Set(styles.map((s) => s.shape)).size).toBe(7);
    expect(new Set(styles.map((s) => s.name)).size).toBe(7);
    expect(
      new Set([...styles.map((s) => s.shape), START_STYLE.shape, WAYPOINT_STYLE.shape]).size,
    ).toBe(9);
    for (const g of IMPACT_GRADES) expect(isImpactGrade(g)).toBe(true);
    expect(isImpactGrade("none")).toBe(false);
  });

  it("calls the loss of redundancy by that name, and tells a dashed line from a solid one", () => {
    expect(impactGradeName("exposed_direct")).toBe("이중화 상실(직접)");
    expect(impactGradeName("exposed_inherited")).toBe("이중화 상실(간접)");
    expect(impactGradeName("down")).toBe("중단");
    expect(gradeStyle("unknown").dashed).toBe(true);
    expect(gradeStyle("down").dashed).toBe(false);
    for (const g of IMPACT_GRADES)
      expect(["danger", "warn", "accent", "muted"]).toContain(GRADE_STYLES[g].tone);
  });

  it("shows a result it does not know as it came, and an object that is not an item as a waypoint", () => {
    expect(gradeStyle("something_new")).toMatchObject({
      name: "something_new",
      dashed: true,
      tone: "muted",
    });
    expect(gradeStyle(undefined)).toBe(WAYPOINT_STYLE);
    expect(impactGradeName("none")).toBe("영향 없음");
    expect(gradeRank("something_new")).toBe(7);
    expect(gradeRank(undefined)).toBeGreaterThan(7);
    expect(sortedGrades(["degraded", "down", "zzz", "down", "aaa"])).toEqual([
      "down",
      "degraded",
      "aaa",
      "zzz",
    ]);
  });
});

describe("[REQ-357] the list has every item, in sections by result, sorted and shown in steps", () => {
  it("has a row for every item received, also in an answer that is too big to draw", () => {
    const data = impactOfSize(5000);
    const tree = treeOf(data);
    const rows = impactRows(tree);
    expect(rows).toHaveLength(data.with_assumptions?.items.length ?? -1);
    expect(rows.map((r) => r.id)).toEqual(itemIds(tree));
    const row = rows[0];
    expect(row).toMatchObject({ lost: expect.any(Array), hops: expect.any(Number) });
  });

  it("names the cause of a row, and says so when the cause is an object the caller may not read", () => {
    const tree = treeOf(smallImpact());
    const rows = impactRows(tree);
    expect(rows.some((r) => r.viaHidden)).toBe(true);
    for (const r of rows) {
      if (r.viaHidden) expect([r.via, r.viaId]).toEqual([undefined, null]);
      else if (r.viaId) expect(tree.nodes.get(r.viaId)?.name).toBe(r.via);
    }
  });

  it("makes a section for each result that has rows, the most severe first", () => {
    const rows = impactRows(treeOf());
    const sections = sectionsByGrade(rows);
    expect(sections.map((s) => s.grade)).toEqual(sortedGrades(rows.map((r) => r.grade)));
    expect(sections.reduce((n, s) => n + s.rows.length, 0)).toBe(rows.length);
    for (const s of sections) expect(s.rows.every((r) => r.grade === s.grade)).toBe(true);
    expect(sectionsByGrade([])).toEqual([]);
  });

  it("sorts by a column in either direction and falls back to a fixed order for ties", () => {
    const rows = impactRows(treeOf(syntheticImpact({ items: 200, seed: 11 })));
    for (const key of ["name", "type", "hops", "lost", "via", "basis"] as const) {
      const up = sortRows(rows, { key, direction: "ascending" });
      const down = sortRows(rows, { key, direction: "descending" });
      expect(up).toHaveLength(rows.length);
      expect(down).toHaveLength(rows.length);
      if (key === "hops") {
        expect(up.map((r) => r.hops)).toEqual([...up.map((r) => r.hops)].sort((a, b) => a - b));
        expect(down.map((r) => r.hops)).toEqual([...down.map((r) => r.hops)].sort((a, b) => b - a));
      }
      if (key === "name") expect(up.map((r) => r.name)).toEqual([...up.map((r) => r.name)].sort());
    }
    const plain = sortRows(rows, undefined);
    expect(plain.map((r) => r.id)).toEqual(
      sortRows([...rows].reverse(), undefined).map((r) => r.id),
    );
    for (let i = 1; i < plain.length; i++) {
      const a = plain[i - 1];
      const b = plain[i];
      if (a && b) expect(gradeRank(a.grade)).toBeLessThanOrEqual(gradeRank(b.grade));
    }
    const basis = sortRows(rows, { key: "basis", direction: "ascending" });
    expect(basis.findIndex((r) => r.assumed)).toBeGreaterThan(-1);
  });

  it("shows the first 50 rows of a section and 50 more each time, never past the total", () => {
    const rows = impactRows(treeOf());
    expect(ROWS_STEP).toBe(50);
    const first = firstRows(rows);
    expect(first.rows).toHaveLength(50);
    expect(first).toMatchObject({ total: rows.length, shown: 50, more: true });
    const second = firstRows(rows, 100);
    expect(second.rows).toHaveLength(100);
    expect(second.rows[99]).toBe(rows[99]);
    const all = firstRows(rows, 99999);
    expect(all).toMatchObject({ shown: rows.length, total: rows.length, more: false });
    expect(firstRows(rows, -4).shown).toBe(50);
    expect(firstRows([], 50)).toMatchObject({ rows: [], total: 0, shown: 0, more: false });
  });

  it("carries the weakest evidence, the hidden object on the way and the other paths of a row", () => {
    const rows = impactRows(treeOf(smallImpact()));
    expect(rows.some((r) => r.weakest !== undefined)).toBe(true);
    expect(rows.every((r) => Array.isArray(r.also))).toBe(true);
    expect(rows.some((r) => r.viaHidden)).toBe(true);
  });
});

describe("[REQ-352] the counts of the spread", () => {
  it("counts the items by result, the most severe first, and the nodes that appear at each step", () => {
    const tree = treeOf(smallImpact());
    const byGrade = countByGrade(tree);
    expect(byGrade.reduce((n, g) => n + g.count, 0)).toBe(itemIds(tree).length);
    expect(byGrade.map((g) => g.grade)).toEqual(sortedGrades(byGrade.map((g) => g.grade)));
    const steps = countByStep(tree);
    expect(steps).toHaveLength(lastStep(tree) + 1);
    expect(steps.reduce((n, s) => n + s.nodes, 0)).toBe(tree.nodes.size);
    expect(steps[0]?.step).toBe(0);
  });

  it("lights the head of a column at the step the spread first reaches it", () => {
    const tree = treeOf(smallImpact());
    const first = laneFirstStep(tree);
    expect(first.get("facility")).toBe(0);
    for (const node of tree.nodes.values())
      expect(first.get(node.layer) ?? 99).toBeLessThanOrEqual(node.step);
  });

  it("says in words what a step brought", () => {
    const tree = treeOf(smallImpact());
    const steps = countByStep(tree);
    const text = describeStep(steps, 1);
    expect(text.startsWith(`단계 1 / ${steps.length - 1}: `)).toBe(true);
    expect(text).toContain("새로 영향받음");
    expect(describeStep([], 0)).toBe("단계 0 / 0: 새로 영향받은 객체 없음");
    expect(describeStep(steps, 999)).toContain(`단계 ${steps.length - 1} /`);
    expect(describeStep(steps, -4)).toContain("단계 0 /");
  });

  it("takes a step later than the step of its cause when the answer says so", () => {
    const data = syntheticImpact({ items: 30, seed: 3 });
    const items = data.with_assumptions?.items ?? [];
    const late = items.map((it) => (it.hops === 1 ? { ...it, hops: 4 } : it));
    const tree = treeOf({
      ...data,
      with_assumptions: { ...data.with_assumptions, items: late },
    } as typeof data);
    for (const node of tree.nodes.values()) {
      const parent = node.parent === null ? undefined : tree.nodes.get(node.parent);
      if (parent) expect(node.step).toBeGreaterThanOrEqual(parent.step);
    }
  });
});
