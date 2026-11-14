/**
 * The reading of an impact answer (ADR-0054). The question is sent twice over: first as a summary of both readings (the
 * counts the server made of the whole answer, which come in a few milliseconds), then the items of the reading chosen,
 * a page at a time, so the screen has something to show while the rest arrives. Every request carries an abort signal:
 * leaving the screen, changing the question and pressing "stop" all end the one that is running. A reading that is
 * changed asks for the items again but not for the summary.
 */
import { useCallback, useEffect, useRef, useState } from "react";
import {
  askImpact,
  createClient,
  type ImpactAnswer,
  type ImpactData,
  type ImpactItem,
  type ImpactRequest,
  type UpstreamItem,
} from "../api";
import { useToken } from "./connection";
import { describeError, type ErrorView } from "./errors";
import { totalCount } from "./impact-counts";
import type { ImpactDataInput, ImpactReading } from "./impact-input";
import {
  type ImpactRequestBody,
  type ImpactView,
  itemsRequest,
  summaryRequest,
} from "./impact-view";

export type ReceivedItem = ImpactItem | UpstreamItem;

export interface ImpactItems {
  /** `idle` before there is a summary, or when the summary says there is nothing to receive. */
  readonly status: "idle" | "receiving" | "done" | "stopped" | "error";
  readonly items: readonly ReceivedItem[];
  /** How many items the answer is expected to have (the server's count, up to the limit asked for). */
  readonly expected: number;
  readonly error: ErrorView | undefined;
}

export interface ImpactLoad {
  /** The summary answer; nothing while it is read, or when it failed. */
  readonly summary: ImpactAnswer | undefined;
  readonly summaryError: ErrorView | undefined;
  readonly items: ImpactItems;
  /** Ends the receiving of items; what has come stays. */
  readonly stop: () => void;
  /** Asks again, the summary and the items. */
  readonly reload: () => void;
  /** Asks the items again only. */
  readonly receiveAgain: () => void;
}

const IDLE: ImpactItems = { status: "idle", items: [], expected: 0, error: undefined };

const isAbort = (error: unknown): boolean =>
  error instanceof DOMException && error.name === "AbortError";

/** The request of the contract from the body the screen builds: the contract's arrays are not read-only. */
export function toRequest(body: ImpactRequestBody): ImpactRequest {
  const { dead, dead_links, site_failure, ...rest } = body;
  return {
    ...rest,
    ...(dead && { dead: [...dead] }),
    ...(dead_links && { dead_links: [...dead_links] }),
    ...(site_failure && { site_failure: [...site_failure] }),
  };
}

/** How many items the answer has for the reading, by the counts of its summary, up to the limit asked for. */
export const expectedItems = (data: ImpactData, reading: ImpactReading, max: number): number =>
  Math.min(totalCount(data, reading), max);

/** The answer with the items received in the place of the section of the reading (the summary has no items). */
export function withItems(
  data: ImpactData,
  reading: ImpactReading,
  items: readonly ReceivedItem[],
): ImpactDataInput {
  if (data.direction === "upstream") {
    const section = data.upstream;
    return {
      ...data,
      upstream: { ...section, items: items as readonly UpstreamItem[] },
    } as ImpactDataInput;
  }
  const section = data[reading];
  return { ...data, [reading]: { ...section, items: items as readonly ImpactItem[] } };
}

function pageOf(data: ImpactData, reading: ImpactReading): readonly ReceivedItem[] {
  return (data.direction === "upstream" ? data.upstream?.items : data[reading]?.items) ?? [];
}

type SummaryState =
  | { readonly status: "loading" }
  | { readonly status: "ready"; readonly key: string; readonly answer: ImpactAnswer }
  | { readonly status: "error"; readonly key: string; readonly error: ErrorView };

export function useImpact(kg: string, view: ImpactView): ImpactLoad {
  const token = useToken();
  const [summaryRound, setSummaryRound] = useState(0);
  const [itemsRound, setItemsRound] = useState(0);
  const [summary, setSummary] = useState<SummaryState>({ status: "loading" });
  const [items, setItems] = useState<ImpactItems>(IDLE);
  const stopper = useRef<() => void>(() => undefined);
  const summaryKey = JSON.stringify([summaryRequest(view), summaryRound]);
  const itemsKey = JSON.stringify([itemsRequest(view), itemsRound]);
  const current = "key" in summary && summary.key === summaryKey;
  const answer = current && summary.status === "ready" ? summary.answer : undefined;
  const { reading, max } = view;

  useEffect(() => {
    const controller = new AbortController();
    setSummary({ status: "loading" });
    const [body] = JSON.parse(summaryKey) as [ImpactRequestBody, number];
    askImpact(createClient({ token, signal: controller.signal }), {
      path: { kg },
      body: toRequest(body),
    })
      .then((next) => {
        if (!controller.signal.aborted)
          setSummary({ status: "ready", key: summaryKey, answer: next });
      })
      .catch((error: unknown) => {
        if (controller.signal.aborted || isAbort(error)) return;
        setSummary({ status: "error", key: summaryKey, error: describeError(error) });
      });
    return () => controller.abort();
  }, [kg, token, summaryKey]);

  // `itemsKey` holds the question and the reading: it changes exactly when the items have to be asked again.
  useEffect(() => {
    if (answer === undefined) {
      setItems(IDLE);
      return;
    }
    const expected = expectedItems(answer.data, reading, max);
    if (expected === 0) {
      setItems({ ...IDLE, status: "done" });
      return;
    }
    const controller = new AbortController();
    let stoppedByPerson = false;
    stopper.current = () => {
      stoppedByPerson = true;
      controller.abort();
    };
    const [first] = JSON.parse(itemsKey) as [ImpactRequestBody, number];
    const client = createClient({ token, signal: controller.signal });
    const got: ReceivedItem[] = [];
    const show = (status: ImpactItems["status"], error?: ErrorView): void =>
      setItems({ status, items: [...got], expected, error });
    setItems({ status: "receiving", items: [], expected, error: undefined });
    void (async () => {
      try {
        let cursor: string | undefined;
        for (;;) {
          const body = toRequest({ ...first, ...(cursor !== undefined && { cursor }) });
          const page = await askImpact(client, { path: { kg }, body });
          const fresh = pageOf(page.data, reading);
          got.push(...fresh);
          cursor = page.meta.result_complete.next_cursor ?? undefined;
          if (fresh.length === 0 || cursor === undefined || got.length >= expected) break;
          show("receiving");
        }
        show("done");
      } catch (error: unknown) {
        if (isAbort(error) && stoppedByPerson) show("stopped");
        else if (!controller.signal.aborted) show("error", describeError(error));
      }
    })();
    return () => controller.abort();
  }, [answer, itemsKey, kg, token, reading, max]);

  return {
    summary: answer,
    summaryError: current && summary.status === "error" ? summary.error : undefined,
    items,
    stop: useCallback(() => stopper.current(), []),
    reload: useCallback(() => setSummaryRound((n) => n + 1), []),
    receiveAgain: useCallback(() => setItemsRound((n) => n + 1), []),
  };
}
