import {
  type CSSProperties,
  type KeyboardEvent,
  memo,
  type RefObject,
  useEffect,
  useMemo,
  useRef,
  useState,
} from "react";
import { cx } from "./cx";
import { Glyph, type GlyphShape, type GlyphTone } from "./Glyph";
import { isNavKey, targetOf } from "./LaneGraphKeys";

export const LANE_WIDTH = 170;
/** A node is as high as a row, and a row is 24 px: the least a target may be (WCAG 2.5.8). */
export const ROW_HEIGHT = 24;
const HEADER_HEIGHT = 30;
const GLYPH_RADIUS = 6;
const LABEL_LENGTH = 16;

export interface GraphLane {
  readonly name: string;
  /** The step at which the spread first reaches the column: its head is lit from then on. */
  readonly firstStep: number;
}

export interface GraphNode {
  readonly id: string;
  /** What is written beside the figure. */
  readonly label: string;
  /** What a screen reader hears: the name, the type, the layer, the grade, the step. */
  readonly description: string;
  readonly lane: number;
  readonly row: number;
  /** The step of the spread at which the node appears. */
  readonly step: number;
  readonly parent: string | null;
  readonly shape: GlyphShape;
  readonly tone: GlyphTone;
  readonly dashed: boolean;
  /** For a group: how many nodes it stands for. */
  readonly count?: number;
}

export interface GraphEdge {
  readonly from: string;
  readonly to: string;
  readonly step: number;
  /** The link rests on an assumption or on nothing known: drawn dashed. */
  readonly dashed: boolean;
  /** The failure passed through a redundancy group on this link: marked with a small diamond. */
  readonly group: boolean;
}

export interface LaneGraphProps {
  readonly name: string;
  /** What the picture holds, in one sentence for a screen reader. */
  readonly summary: string;
  readonly lanes: readonly GraphLane[];
  readonly nodes: readonly GraphNode[];
  readonly edges: readonly GraphEdge[];
  /** The step the spread has reached: what is of a later step is not shown. */
  readonly step: number;
  /** How long a step fades in; with reduced motion there is no fade whatever this is. */
  readonly stepMs?: number;
  readonly playing?: boolean;
  readonly selected: string | undefined;
  readonly onSelect: (id: string | undefined) => void;
  /** Moves the focus to the list, which holds the same answer. */
  readonly onSkip?: () => void;
}

/** An SVG has no `<fieldset>` or `<button>`; the roles tell a screen reader what a `<g>` is. */
const GROUP = { role: "group" } as const;
const BUTTON = { role: "button" } as const;
/** The lines are decoration; spread, because the lint rule takes any `<g>` with this for a focusable element. */
const HIDDEN = { "aria-hidden": true } as const;

const short = (text: string): string =>
  text.length > LABEL_LENGTH ? `${text.slice(0, LABEL_LENGTH - 1)}…` : text;

const cx0 = (node: { lane: number }): number => node.lane * LANE_WIDTH + LANE_WIDTH / 2 - 52;
const cy0 = (node: { row: number }): number =>
  HEADER_HEIGHT + node.row * ROW_HEIGHT + ROW_HEIGHT / 2;

interface Spot {
  readonly x: number;
  readonly y: number;
}

const round = (n: number): number => Math.round(n * 10) / 10;

/** Where a line leaves its parent (past the label) and where it meets its child (before the figure). */
const OUT = 112;
const IN = 9;

/** The elbow of a line: the x of its upright part, and the two x it runs between. */
function course(from: Spot, to: Spot): { out: number; mid: number; into: number } {
  if (to.x > from.x + OUT) {
    const out = from.x + OUT;
    const into = to.x - IN;
    return { out, mid: (out + into) / 2, into };
  }
  // The child is in the same column: the line goes up and down the left of the figures.
  return { out: from.x - IN, mid: from.x - IN - 3, into: to.x - IN };
}

/** A bent line from a parent to a child: out of the parent, across to the middle, along to the child's row, in. */
const elbow = (from: Spot, to: Spot): string => {
  const c = course(from, to);
  return `M${round(c.out)} ${round(from.y)}H${round(c.mid)}V${round(to.y)}H${round(c.into)}`;
};

/** A small diamond on the upright part of a line: the failure passed through a redundancy group on this link. */
const diamond = (from: Spot, to: Spot): string => {
  const c = course(from, to);
  const mx = round(c.mid);
  const my = round((from.y + to.y) / 2);
  return `M${mx} ${my - 4}l4 4l-4 4l-4 -4z`;
};

interface Strokes {
  readonly solid: string;
  readonly dashed: string;
  readonly marks: string;
}

/** The lines of one step as three paths whatever their number: those resting on facts, those that do not, the marks. */
function strokesOf(edges: readonly GraphEdge[], spots: ReadonlyMap<string, Spot>): Strokes {
  const solid: string[] = [];
  const dashed: string[] = [];
  const marks: string[] = [];
  for (const e of edges) {
    const from = spots.get(e.from);
    const to = spots.get(e.to);
    if (from === undefined || to === undefined) continue;
    (e.dashed ? dashed : solid).push(elbow(from, to));
    if (e.group) marks.push(diamond(from, to));
  }
  return { solid: solid.join(""), dashed: dashed.join(""), marks: marks.join("") };
}

/**
 * The lines of a step, behind its nodes. A line is decoration: what led to what is told by the name of a node and by
 * the list, so the lines are hidden from a screen reader. They are in the group of the step they lead to, so they
 * appear with it, and there is a path for a kind of line, not an element for each line.
 */
function EdgePaths({
  edges,
  spots,
}: {
  readonly edges: readonly GraphEdge[];
  readonly spots: ReadonlyMap<string, Spot>;
}) {
  const { solid, dashed, marks } = strokesOf(edges, spots);
  const line = "fill-none stroke-subtle stroke-[1.5]";
  return (
    <g {...HIDDEN} data-edges="">
      {solid === "" ? null : <path d={solid} className={line} />}
      {dashed === "" ? null : <path d={dashed} strokeDasharray="4 3" className={line} />}
      {marks === "" ? null : <path d={marks} className="fill-surface stroke-subtle stroke-[1.5]" />}
    </g>
  );
}

interface Handlers {
  readonly select: (id: string) => void;
  readonly key: (id: string, event: KeyboardEvent<SVGGElement>) => void;
  readonly focus: (id: string) => void;
}

interface LayerBodyProps {
  readonly nodes: readonly GraphNode[];
  readonly edges: readonly GraphEdge[];
  readonly spots: ReadonlyMap<string, Spot>;
  /** The selected node, when it is in this layer. */
  readonly selected: string | undefined;
  /** The node that is the tab stop, when it is in this layer. */
  readonly tabStop: string | undefined;
  readonly handlers: Handlers;
}

/**
 * What a step draws. It does not depend on the step that is shown, so while the spread plays it is not drawn again:
 * only the `<g>` around it changes (its opacity), which is what keeps a play cheap in a picture of thousands of nodes.
 */
const LayerBody = memo(function LayerBody({
  nodes,
  edges,
  spots,
  selected,
  tabStop,
  handlers,
}: LayerBodyProps) {
  return (
    <>
      <EdgePaths edges={edges} spots={spots} />
      {nodes.map((n) => {
        const spot = spots.get(n.id) ?? { x: 0, y: 0 };
        /** The props that make a `<g>` a button: spread, because the lint rules do not know an SVG group may be one. */
        const button = {
          ...BUTTON,
          "aria-label": n.description,
          "aria-pressed": selected === n.id,
          tabIndex: tabStop === n.id ? 0 : -1,
          onClick: () => handlers.select(n.id),
          onKeyDown: (event: KeyboardEvent<SVGGElement>) => handlers.key(n.id, event),
          onFocus: () => handlers.focus(n.id),
        };
        return (
          <g key={n.id} data-node={n.id} {...button} className="cursor-pointer">
            <rect
              x={spot.x - 14}
              y={spot.y - ROW_HEIGHT / 2}
              width={LANE_WIDTH - 12}
              height={ROW_HEIGHT}
              className={selected === n.id ? "fill-accent-soft stroke-accent" : "fill-transparent"}
            />
            <Glyph
              shape={n.shape}
              tone={n.tone}
              dashed={n.dashed}
              x={spot.x}
              y={spot.y}
              r={GLYPH_RADIUS}
            />
            <text x={spot.x + 12} y={spot.y + 4} className="fill-fg text-xs">
              {short(n.label)}
              {n.count === undefined ? "" : ` ×${n.count}`}
            </text>
          </g>
        );
      })}
    </>
  );
});

interface Layer {
  readonly step: number;
  readonly nodes: GraphNode[];
  readonly edges: GraphEdge[];
}

/** The nodes and edges by the step they appear at, in the order of the steps. */
function layersOf(nodes: readonly GraphNode[], edges: readonly GraphEdge[]): Layer[] {
  const layers = new Map<number, Layer>();
  const at = (step: number): Layer => {
    let layer = layers.get(step);
    if (layer === undefined) {
      layer = { step, nodes: [], edges: [] };
      layers.set(step, layer);
    }
    return layer;
  };
  for (const n of nodes) at(n.step).nodes.push(n);
  for (const e of edges) at(e.step).edges.push(e);
  return [...layers.values()].sort((a, b) => a.step - b.step);
}

interface Navigation {
  readonly handlers: Handlers;
  /** The node the tab key reaches: the one with the focus or the selection, else the first that is shown. */
  readonly tabStop: string | undefined;
}

/** One tab stop, and the arrow keys that move the focus along the tree among the nodes that are shown. */
function useNavigation(
  root: RefObject<HTMLElement | null>,
  at: ReadonlyMap<string, GraphNode>,
  nodes: readonly GraphNode[],
  step: number,
  selected: string | undefined,
  onSelect: (id: string | undefined) => void,
): Navigation {
  const [focused, setFocused] = useState<string | undefined>(undefined);
  const [moved, setMoved] = useState(0);
  const shown = nodes.filter((n) => n.step <= step);
  const visible = (id: string | undefined): boolean => {
    const n = id === undefined ? undefined : at.get(id);
    return n !== undefined && n.step <= step;
  };
  const tabStop = [focused, selected].find(visible) ?? shown[0]?.id;

  // What the handlers need to know of the moment they are called is read from here, so they can stay the same ones.
  const now = useRef({ shown, selected, onSelect });
  now.current = { shown, selected, onSelect };
  const handlers = useMemo<Handlers>(
    () => ({
      select: (id) => now.current.onSelect(now.current.selected === id ? undefined : id),
      focus: setFocused,
      key: (id, event) => {
        if (isNavKey(event.key)) {
          event.preventDefault();
          const next = targetOf(event.key, id, now.current.shown);
          if (next !== undefined) {
            setFocused(next);
            setMoved((n) => n + 1);
          }
        } else if (event.key === "Enter" || event.key === " ") {
          event.preventDefault();
          now.current.onSelect(now.current.selected === id ? undefined : id);
        } else if (event.key === "Escape") {
          now.current.onSelect(undefined);
        }
      },
    }),
    [],
  );

  // The arrow keys move the focus to a node that was not the tab stop, so the page has to do it.
  useEffect(() => {
    if (moved > 0 && focused !== undefined) {
      const found = root.current?.querySelectorAll<SVGElement>("[data-node]") ?? [];
      for (const node of found) if (node.dataset.node === focused) node.focus();
    }
  }, [moved, focused, root]);
  return { handlers, tabStop };
}

/** The head and the band of each column; the column is lit from the step the spread first reaches it. */
function LaneHeads({
  lanes,
  step,
  height,
}: {
  lanes: readonly GraphLane[];
  step: number;
  height: number;
}) {
  return (
    <>
      {lanes.map((lane, i) => {
        const lit = lane.firstStep <= step;
        return (
          <g key={lane.name} data-lane={i}>
            <rect
              x={i * LANE_WIDTH}
              y={0}
              width={LANE_WIDTH}
              height={height}
              className={lit ? "fill-accent-soft" : "fill-transparent"}
            />
            <text
              x={i * LANE_WIDTH + 8}
              y={19}
              className={cx("text-xs", lit ? "fill-fg font-semibold" : "fill-muted")}
            >
              {lane.name}
            </text>
          </g>
        );
      })}
    </>
  );
}

/**
 * The picture of an answer in lanes: a column for each layer, a node for each object, a line for each link. It shows
 * the answer from the objects that failed outward, a step at a time: a `<g>` for each step, whose opacity is switched
 * when the spread reaches it (an element-by-element animation is what makes an SVG slow; a layer is cheap). It moves
 * only when the person has not asked the system for less motion. Every node is a button with a name for a screen
 * reader; there is one tab stop, and the arrow keys move along the tree.
 */
export function LaneGraph(props: LaneGraphProps) {
  const { lanes, nodes, edges, step, selected } = props;
  const root = useRef<HTMLElement>(null);
  const layers = useMemo(() => layersOf(nodes, edges), [nodes, edges]);
  const at = useMemo(() => new Map<string, GraphNode>(nodes.map((n) => [n.id, n])), [nodes]);
  const spots = useMemo(
    () => new Map<string, Spot>(nodes.map((n) => [n.id, { x: cx0(n), y: cy0(n) }])),
    [nodes],
  );
  const { handlers, tabStop } = useNavigation(root, at, nodes, step, selected, props.onSelect);
  const height =
    HEADER_HEIGHT + nodes.reduce((most, n) => Math.max(most, n.row + 1), 0) * ROW_HEIGHT;
  const width = lanes.length * LANE_WIDTH;
  const chosen = selected === undefined ? undefined : at.get(selected);
  const tabStep = tabStop === undefined ? undefined : at.get(tabStop)?.step;
  const fade =
    props.stepMs === undefined
      ? undefined
      : ({ "--step-ms": `${props.stepMs}ms` } as CSSProperties);

  return (
    <section
      ref={root}
      aria-label={props.name}
      // The picture can be wider than the screen: it scrolls, and what scrolls is reached by the keyboard (WCAG 2.1.1).
      {...{ tabIndex: 0 }}
      className="overflow-auto rounded-lg border border-line bg-surface"
    >
      {props.onSkip ? (
        <button
          type="button"
          onClick={props.onSkip}
          className="sr-only focus:not-sr-only focus:m-2 focus:cursor-pointer focus:rounded-md focus:border focus:border-control-line focus:bg-surface focus:px-3 focus:py-1.5 focus:text-sm focus:font-medium"
        >
          목록으로 건너뛰기
        </button>
      ) : null}
      <svg
        {...GROUP}
        aria-label={props.summary}
        width={width}
        height={height}
        viewBox={`0 0 ${width} ${height}`}
        className="block"
      >
        <title>{props.summary}</title>
        <LaneHeads lanes={lanes} step={step} height={height} />
        {layers.map((layer) => (
          <g
            key={layer.step}
            data-step={layer.step}
            aria-hidden={layer.step > step ? true : undefined}
            // The length of the fade is a variable, and only the class that is inside `motion-safe` reads it: with
            // reduced motion nothing in the page sets a duration, so nothing fades.
            style={fade}
            className={cx(
              "motion-safe:transition-[opacity,visibility] motion-safe:duration-(--step-ms)",
              layer.step <= step ? "visible opacity-100" : "invisible opacity-0",
            )}
          >
            <LayerBody
              nodes={layer.nodes}
              edges={layer.edges}
              spots={spots}
              selected={layer.step === chosen?.step ? selected : undefined}
              tabStop={layer.step === tabStep ? tabStop : undefined}
              handlers={handlers}
            />
          </g>
        ))}
      </svg>
      <output className="sr-only">
        {props.playing || chosen === undefined ? "" : `${chosen.description}, 선택됨`}
      </output>
    </section>
  );
}
