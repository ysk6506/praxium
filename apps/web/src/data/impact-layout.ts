/**
 * The lanes of the impact picture (ADR-0051). A layer of the model is a column, in the order "what supplies is on the
 * left"; only the columns in use are kept. A node's column is the layer of its type. Its row is the next free row of
 * its column, but never above its parent's row. Nodes are read in the depth-first order of the cause tree, so a column
 * holds its nodes in the order of the tree: the lines between columns hardly cross, and nothing has to be iterated
 * to a fixed point. The row steps are fixed, so the same input is the same picture, however it arrived.
 */

/** The layers in the order of the columns. The standard model lists them in another order (a server leans on a switch). */
export const LANE_ORDER: readonly string[] = [
  "facility",
  "network",
  "storage",
  "hardware",
  "virtualization",
  "container",
  "platform",
  "service",
  "cross_cutting",
  "system",
];

/** The column of a layer the model does not list; it comes after the known ones. */
export const OTHER_LANE = "other";

export const ROW_HEIGHT = 24;
export const LANE_WIDTH = 170;

export interface LayoutInput {
  readonly id: string;
  readonly layer: string;
  readonly parent: string | null;
}

export interface Lane {
  readonly layer: string;
  readonly index: number;
  /** The left edge of the column. */
  readonly x: number;
}

export interface Placed {
  readonly id: string;
  readonly lane: number;
  readonly row: number;
  /** The center of the node: the middle of its column, and its row. */
  readonly x: number;
  readonly y: number;
}

export interface Layout {
  readonly lanes: readonly Lane[];
  readonly placed: ReadonlyMap<string, Placed>;
  readonly rows: number;
  readonly width: number;
  readonly height: number;
}

const laneKey = (layer: string): string => (LANE_ORDER.includes(layer) ? layer : OTHER_LANE);

/** The columns used by the layers given, in order. */
export function lanesOf(layers: Iterable<string>): Lane[] {
  const used = new Set<string>();
  for (const layer of layers) used.add(laneKey(layer));
  const known = LANE_ORDER.filter((layer) => used.has(layer));
  const all = used.has(OTHER_LANE) ? [...known, OTHER_LANE] : known;
  return all.map((layer, index) => ({ layer, index, x: index * LANE_WIDTH }));
}

/** Places the nodes. `nodes` must list a parent before its children and be in the order the picture wants. */
export function layoutLanes(nodes: readonly LayoutInput[]): Layout {
  const lanes = lanesOf(nodes.map((n) => n.layer));
  const laneAt = new Map(lanes.map((lane) => [lane.layer, lane]));
  const next = new Map<string, number>();
  const placed = new Map<string, Placed>();
  let rows = 0;
  for (const node of nodes) {
    const key = laneKey(node.layer);
    const lane = laneAt.get(key);
    if (lane === undefined) continue;
    const above = node.parent === null ? 0 : (placed.get(node.parent)?.row ?? 0);
    const row = Math.max(next.get(key) ?? 0, above);
    next.set(key, row + 1);
    rows = Math.max(rows, row + 1);
    placed.set(node.id, {
      id: node.id,
      lane: lane.index,
      row,
      x: lane.x + LANE_WIDTH / 2,
      y: row * ROW_HEIGHT + ROW_HEIGHT / 2,
    });
  }
  return {
    lanes,
    placed,
    rows,
    width: lanes.length * LANE_WIDTH,
    height: rows * ROW_HEIGHT,
  };
}

export interface PlacedEdge {
  readonly from: string;
  readonly to: string;
  readonly x1: number;
  readonly y1: number;
  readonly x2: number;
  readonly y2: number;
}

/** The lines from each parent to its children. */
export function layoutEdges(nodes: readonly LayoutInput[], layout: Layout): PlacedEdge[] {
  const edges: PlacedEdge[] = [];
  for (const node of nodes) {
    if (node.parent === null) continue;
    const a = layout.placed.get(node.parent);
    const b = layout.placed.get(node.id);
    if (a && b) edges.push({ from: a.id, to: b.id, x1: a.x, y1: a.y, x2: b.x, y2: b.y });
  }
  return edges;
}
