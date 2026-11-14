/** What exists: the KGs, and the object types of the model with how many objects of each a KG holds. */
import { useCallback } from "react";
import {
  askFind,
  getObjectType,
  type KG,
  listKGs,
  listObjectTypes,
  type ObjectType,
  type ObjectTypeSummary,
} from "../api";
import { type AsyncResult, useAsync } from "./useAsync";

/** The largest page the API gives (the contract's limit), enough for every KG and every type of the model. */
export const MAX_PAGE = 1000;

export function useKgs(): AsyncResult<readonly KG[]> {
  const load = useCallback(
    async (client: Parameters<typeof listKGs>[0]) =>
      (await listKGs(client, { query: { limit: MAX_PAGE } })).data,
    [],
  );
  return useAsync(load);
}

export interface TypeEntry {
  readonly name: string;
  readonly layer: string;
  readonly tier: string;
  readonly description: string;
  readonly parent: string | null;
  /** How many objects of exactly this type the KG holds. */
  readonly count: number;
}

/** The model's types joined with the KG's counts, the types with objects first within each layer. */
export function joinTypes(
  types: readonly ObjectTypeSummary[],
  counts: readonly { object_type: string; count: number }[],
): TypeEntry[] {
  const byName = new Map(counts.map((c) => [c.object_type, c.count]));
  return types
    .map((t) => ({
      name: t.name,
      layer: t.layer,
      tier: t.tier,
      description: t.description,
      parent: t.parent,
      count: byName.get(t.name) ?? 0,
    }))
    .sort(
      (a, b) =>
        a.layer.localeCompare(b.layer, "en") ||
        b.count - a.count ||
        a.name.localeCompare(b.name, "en"),
    );
}

export function useTypeCatalog(kg: string): AsyncResult<readonly TypeEntry[]> {
  const load = useCallback(
    async (client: Parameters<typeof listKGs>[0]) => {
      const [types, found] = await Promise.all([
        listObjectTypes(client, { query: { limit: MAX_PAGE } }),
        askFind(client, { path: { kg }, body: { shape: "summary" } }),
      ]);
      return joinTypes(types.data, found.data.counts_by_type);
    },
    [kg],
  );
  return useAsync(load);
}

export function useObjectTypeDetail(typeName: string): AsyncResult<ObjectType> {
  const load = useCallback(
    async (client: Parameters<typeof listKGs>[0]) =>
      (
        await getObjectType(client, {
          path: { type_id: typeName },
          query: { include: "effective_properties" },
        })
      ).data,
    [typeName],
  );
  return useAsync(load);
}
