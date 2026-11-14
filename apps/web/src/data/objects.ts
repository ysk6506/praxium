/** Objects: a page of one type, the attributes of each row, one object in full, and its neighbors. */
import { useCallback, useEffect, useState } from "react";
import {
  askFind,
  askGet,
  askNeighbors,
  createClient,
  type FindItem,
  type GetData,
  type NeighborItem,
  type NeighborsData,
  type QueryMeta,
  type Requester,
  type ResultComplete,
} from "../api";
import { mapLimit } from "./concurrency";
import { useToken } from "./connection";
import { describeError, type ErrorView } from "./errors";
import { type AsyncResult, useAsync } from "./useAsync";

export interface ObjectPage {
  readonly items: readonly FindItem[];
  readonly page: ResultComplete;
}

export function useObjectPage(
  kg: string,
  type: string,
  limit: number,
  cursor: string | undefined,
): AsyncResult<ObjectPage> {
  const load = useCallback(
    async (client: Requester): Promise<ObjectPage> => {
      const answer = await askFind(client, {
        path: { kg },
        body: { object_type: type, shape: "items", limit, cursor, include_subtypes: false },
      });
      return { items: answer.data.items ?? [], page: answer.meta.result_complete };
    },
    [kg, type, limit, cursor],
  );
  return useAsync(load);
}

export type RowDetail =
  | { readonly status: "ready"; readonly data: GetData }
  | { readonly status: "error"; readonly error: ErrorView };

/** How many rows are read at once: enough to fill a page quickly, few enough not to crowd the server. */
export const ROW_READ_PARALLELISM = 6;

/**
 * The attributes and identifiers of the rows of a page. The find query lists names only, so each row is read with
 * `get`; the details come in one by one and the table fills as they do.
 */
export function useRowDetails(
  kg: string,
  items: readonly FindItem[],
): ReadonlyMap<string, RowDetail> {
  const token = useToken();
  const [details, setDetails] = useState<ReadonlyMap<string, RowDetail>>(new Map());
  useEffect(() => {
    setDetails(new Map());
    if (items.length === 0) return;
    const controller = new AbortController();
    const client = createClient({ token, signal: controller.signal });
    const put = (id: string, detail: RowDetail): void => {
      if (!controller.signal.aborted) setDetails((previous) => new Map(previous).set(id, detail));
    };
    void mapLimit(items, ROW_READ_PARALLELISM, async (item) => {
      try {
        const answer = await askGet(client, {
          path: { kg },
          body: {
            object_id: item.object_id,
            include: ["attributes", "identifiers"],
            shape: "items",
          },
        });
        put(item.object_id, { status: "ready", data: answer.data });
      } catch (error) {
        if (!controller.signal.aborted)
          put(item.object_id, { status: "error", error: describeError(error) });
      }
    });
    return () => controller.abort();
  }, [kg, items, token]);
  return details;
}

export interface ObjectDetail {
  readonly data: GetData;
  readonly meta: QueryMeta;
}

export function useObject(kg: string, id: string): AsyncResult<ObjectDetail> {
  const load = useCallback(
    async (client: Requester): Promise<ObjectDetail> => {
      const answer = await askGet(client, {
        path: { kg },
        body: {
          object_id: id,
          include: ["attributes", "identifiers", "link_counts", "evidence", "provenance"],
          shape: "items",
        },
      });
      return { data: answer.data, meta: answer.meta };
    },
    [kg, id],
  );
  return useAsync(load);
}

export interface NeighborResult {
  readonly data: NeighborsData;
  readonly page: ResultComplete;
}

export const NEIGHBOR_STEPS = [100, 300, 1000] as const;

export function useNeighbors(kg: string, id: string, limit: number): AsyncResult<NeighborResult> {
  const load = useCallback(
    async (client: Requester): Promise<NeighborResult> => {
      const answer = await askNeighbors(client, {
        path: { kg },
        body: { object_id: id, direction: "both", shape: "items", limit },
      });
      return { data: answer.data, page: answer.meta.result_complete };
    },
    [kg, id, limit],
  );
  return useAsync(load);
}

export interface NeighborGroupView {
  readonly linkType: string;
  readonly direction: string;
  readonly items: readonly NeighborItem[];
}

/** The neighbor items grouped by link type and direction, in the order the API listed them. */
export function groupNeighbors(data: NeighborsData): NeighborGroupView[] {
  const groups = new Map<string, { linkType: string; direction: string; items: NeighborItem[] }>();
  for (const item of data.items ?? []) {
    const key = `${item.link_type}\u0000${item.direction}`;
    const group = groups.get(key) ?? {
      linkType: item.link_type,
      direction: item.direction,
      items: [],
    };
    group.items.push(item);
    groups.set(key, group);
  }
  return [...groups.values()];
}
