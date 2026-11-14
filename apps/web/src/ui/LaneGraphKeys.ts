/**
 * Where an arrow key takes the focus in the graph (WAI-ARIA: one tab stop, then the arrow keys). Right goes to the
 * first child (what is affected next), left to the parent (the cause), up and down to the neighbor in the same column,
 * Home and End to the first and the last of the column. Only nodes that are shown count: a node the spread has not
 * reached yet is not on the screen, so the keys do not go there.
 */

export interface KeyNode {
  readonly id: string;
  readonly lane: number;
  readonly row: number;
  readonly parent: string | null;
}

export type NavKey = "ArrowRight" | "ArrowLeft" | "ArrowDown" | "ArrowUp" | "Home" | "End";

export const isNavKey = (key: string): key is NavKey =>
  key === "ArrowRight" ||
  key === "ArrowLeft" ||
  key === "ArrowDown" ||
  key === "ArrowUp" ||
  key === "Home" ||
  key === "End";

/** The node the key leads to from `from`, or nothing when the key leads nowhere. `shown` lists the nodes in the order of the tree. */
export function targetOf(key: NavKey, from: string, shown: readonly KeyNode[]): string | undefined {
  const here = shown.find((n) => n.id === from);
  if (here === undefined) return undefined;
  if (key === "ArrowRight") return shown.find((n) => n.parent === from)?.id;
  if (key === "ArrowLeft") {
    return here.parent === null ? undefined : shown.find((n) => n.id === here.parent)?.id;
  }
  const column = shown.filter((n) => n.lane === here.lane).sort((a, b) => a.row - b.row);
  const at = column.findIndex((n) => n.id === from);
  if (key === "Home") return column[0]?.id;
  if (key === "End") return column[column.length - 1]?.id;
  return column[key === "ArrowDown" ? at + 1 : at - 1]?.id;
}
