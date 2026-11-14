/**
 * What the impact screen reads from the model (ADR-0054): the layer of a type, which the layout needs to put an object
 * in a column, and which types hold other objects (a site, a building, a room, a rack), which decides whether the
 * object screen offers "안에 든 것까지 멈추면". The model says it: a type holds others when it is the "inside" end of a
 * binding of a link type whose meaning is `contained_in`. No type name is written here.
 */
import { useCallback } from "react";
import {
  type Binding,
  type LinkTypeSummary,
  listLinkTypeBindings,
  listLinkTypes,
  listObjectTypes,
  type ObjectTypeSummary,
  type Requester,
} from "../api";
import { MAX_PAGE } from "./catalog";
import { type AsyncResult, useAsync } from "./useAsync";

/** The meaning of a link type that says one object is inside another. */
export const CONTAINED_IN = "contained_in";

export interface ImpactModel {
  /** The layer of a type by its name; nothing for a type the model does not list. */
  readonly layerOf: (type: string) => string;
  /** The names of the types whose objects can hold other objects. */
  readonly containers: ReadonlySet<string>;
}

/** The ends of the bindings that hold: the ids of types, and the ids of traits that a type may have. */
interface Holders {
  readonly types: ReadonlySet<string>;
  readonly traits: ReadonlySet<string>;
}

function holdersOf(bindings: readonly Binding[]): Holders {
  const types = new Set<string>();
  const traits = new Set<string>();
  for (const binding of bindings) {
    const { kind, ref } = binding.to;
    if (ref === null) continue;
    if (kind === "type") types.add(ref);
    else if (kind === "trait") traits.add(ref);
  }
  return { types, traits };
}

/** Whether a type, or one of the types it is a kind of, is an end that holds. */
function holds(
  type: ObjectTypeSummary,
  byId: ReadonlyMap<string, ObjectTypeSummary>,
  holders: Holders,
): boolean {
  const seen = new Set<string>();
  for (let t: ObjectTypeSummary | undefined = type; t && !seen.has(t.id); ) {
    seen.add(t.id);
    if (holders.types.has(t.id) || t.traits.some((trait) => holders.traits.has(trait))) return true;
    t = t.parent === null ? undefined : byId.get(t.parent);
  }
  return false;
}

/** The model of the screen from the types, and the bindings of the link types that mean "inside". */
export function buildImpactModel(
  types: readonly ObjectTypeSummary[],
  containedBindings: readonly Binding[],
): ImpactModel {
  const byId = new Map(types.map((t) => [t.id, t]));
  const layers = new Map(types.map((t) => [t.name, t.layer]));
  const holders = holdersOf(containedBindings);
  const containers = new Set(types.filter((t) => holds(t, byId, holders)).map((t) => t.name));
  return { layerOf: (type) => layers.get(type) ?? "", containers };
}

const isContainment = (type: LinkTypeSummary): boolean => type.meaning === CONTAINED_IN;

async function loadModel(client: Requester): Promise<ImpactModel> {
  const [types, linkTypes] = await Promise.all([
    listObjectTypes(client, { query: { limit: MAX_PAGE } }),
    listLinkTypes(client, { query: { limit: MAX_PAGE } }),
  ]);
  const answers = await Promise.all(
    linkTypes.data
      .filter(isContainment)
      .map((lt) =>
        listLinkTypeBindings(client, { path: { link_type_id: lt.id }, query: { limit: MAX_PAGE } }),
      ),
  );
  return buildImpactModel(
    types.data,
    answers.flatMap((a) => a.data),
  );
}

/** The model of the impact screen, read once for the screen that asks. */
export function useImpactModel(): AsyncResult<ImpactModel> {
  const load = useCallback((client: Requester) => loadModel(client), []);
  return useAsync(load);
}
