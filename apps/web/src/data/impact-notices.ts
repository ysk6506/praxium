/**
 * What the impact screen tells a person about the answer besides the answer (ADR-0054): why a result is cut short and
 * what to do about it, what the calculation assumed, which notes the server made, what the answer cannot see. Pure
 * functions from the computation and the counts of the answer to short sentences, so the words are tested and the
 * screen only draws them.
 */

import { ITEM_LIMITS } from "./impact-view";

/** The channels an object can lose, in the words of the glossary. A channel this version does not know is shown as it came. */
const CHANNEL_NAMES: Readonly<Record<string, string>> = {
  service: "서비스",
  power: "전원",
  cooling: "냉각",
  network: "네트워크",
  storage: "스토리지",
  management: "관리",
};

export const channelName = (channel: string): string => CHANNEL_NAMES[channel] ?? channel;

/** The notes the server makes (`meta.warnings`) as sentences. A note this version does not know is shown as it came. */
const WARNING_TEXTS: Readonly<Record<string, string>> = {
  stale_observation_not_used: "오래된 관측값은 계산에 쓰지 않았습니다.",
  capacity_group_unknown:
    "용량으로 이중화를 맺은 그룹은 용량을 계산하지 않습니다. 구성원이 모두 정상일 때만 정상으로 보고, 아니면 알 수 없음으로 읽었습니다.",
};

export const warningText = (code: string): string => WARNING_TEXTS[code] ?? code;

export type NoticeTone = "warn" | "info";

/** What pressing the button of a notice asks again. */
export type NoticeAction =
  | { readonly kind: "raise"; readonly label: string; readonly max: number }
  | { readonly kind: "unlimited-hops"; readonly label: string }
  | { readonly kind: "again"; readonly label: string };

export interface ImpactNotice {
  readonly id: string;
  readonly tone: NoticeTone;
  readonly title: string;
  readonly text: string;
  readonly action?: NoticeAction;
}

/** The facts of the answer the notices are made from. */
export interface NoticeFacts {
  readonly computation: {
    readonly complete: boolean;
    readonly items_over_cap: boolean;
    readonly max_hops: number;
    readonly max_items: number;
    readonly stopped_by: string | null;
    readonly time_budget_seconds: number;
    readonly weak_links: number;
    readonly stale_inputs: number;
  };
  readonly warnings: readonly string[];
  readonly reading: "facts_only" | "with_assumptions";
  /** How many objects have no supplier for a channel they need, by the reading shown; not asked upstream. */
  readonly unfilled: number | undefined;
  /** The person stopped receiving the items. */
  readonly stopped: boolean;
  readonly received: number;
  readonly expected: number;
}

/** The next limit of items above the one asked for; nothing when the highest was asked already. */
export function nextItemLimit(current: number): number | undefined {
  return ITEM_LIMITS.find((n) => n > current);
}

function overCap(facts: NoticeFacts): ImpactNotice | undefined {
  if (!facts.computation.items_over_cap) return undefined;
  const asked = facts.computation.max_items;
  const next = nextItemLimit(asked);
  return {
    id: "items_over_cap",
    tone: "warn",
    title: "항목 수 한도를 넘어 일부만 받았습니다",
    text: `영향받는 객체가 한도(${asked}개)보다 많아 뒤쪽 항목을 받지 않았습니다. 위의 등급별 개수는 전체를 센 값이고, 그래프와 목록은 받은 ${facts.received}개로 그렸습니다.`,
    ...(next !== undefined && {
      action: { kind: "raise", label: `한도를 ${next}개로 올려 다시 묻기`, max: next },
    }),
  };
}

function timeBudget(facts: NoticeFacts): ImpactNotice | undefined {
  if (facts.computation.stopped_by !== "time_budget") return undefined;
  return {
    id: "time_budget",
    tone: "warn",
    title: "시간 한도에서 계산이 멈췄습니다",
    text: `계산 시간 한도(${facts.computation.time_budget_seconds}초)에 걸려 여기까지만 번진 결과입니다. 실제로는 더 번질 수 있어, 개수와 목록은 최소한입니다.`,
  };
}

function maxHops(facts: NoticeFacts): ImpactNotice | undefined {
  if (facts.computation.stopped_by !== "max_hops") return undefined;
  return {
    id: "max_hops",
    tone: "warn",
    title: "단계 한도에서 계산이 멈췄습니다",
    text: `질문에서 정한 단계 한도(${facts.computation.max_hops}단계)까지만 번진 결과입니다. 더 먼 객체는 들어 있지 않습니다.`,
    action: { kind: "unlimited-hops", label: "단계 한도 없이 다시 묻기" },
  };
}

function stoppedByPerson(facts: NoticeFacts): ImpactNotice | undefined {
  if (!facts.stopped) return undefined;
  return {
    id: "stopped",
    tone: "warn",
    title: "불러오기를 중지했습니다",
    text: `항목 ${facts.expected}개 중 ${facts.received}개만 받아 그렸습니다. 등급별 개수는 전체를 센 값입니다.`,
    action: { kind: "again", label: "다시 불러오기" },
  };
}

function unfilledChannels(facts: NoticeFacts): ImpactNotice | undefined {
  if (facts.unfilled === undefined || facts.unfilled === 0) return undefined;
  return {
    id: "unfilled",
    tone: "info",
    title: `필요한 채널에 공급 링크가 없는 객체 ${facts.unfilled}개`,
    text: "이 객체들은 필요로 하는 채널(전원, 냉각 등)을 채우는 공급 링크가 데이터에 없습니다. 장애로 계산하지 않았고 알 수 없음으로도 읽지 않았습니다. 이 수만큼 데이터에 빈 곳이 있다는 뜻입니다.",
  };
}

function assumptions(facts: NoticeFacts): ImpactNotice | undefined {
  const weak = facts.computation.weak_links;
  if (facts.reading === "with_assumptions") {
    return {
      id: "assumptions",
      tone: "info",
      title: "가정을 포함해 읽은 결과입니다",
      text: "근거가 가정인 링크도 사실처럼 읽었습니다. 가정이 틀리면 결과도 달라집니다.",
    };
  }
  if (weak === 0) return undefined;
  return {
    id: "assumptions",
    tone: "info",
    title: "사실만으로 읽은 결과입니다",
    text: `근거가 가정이거나 알 수 없는 링크 ${weak}개는 장애를 넘기지 않고 알 수 없음만 넘겼습니다. 가정 포함으로 바꾸면 이 링크도 사실처럼 읽습니다.`,
  };
}

function staleInputs(facts: NoticeFacts): ImpactNotice | undefined {
  if (facts.computation.stale_inputs === 0) return undefined;
  return {
    id: "stale",
    tone: "info",
    title: `오래된 관측값 ${facts.computation.stale_inputs}개를 쓰지 않았습니다`,
    text: "관측한 지 오래된 값은 계산에서 뺐습니다.",
  };
}

/** The notices of an answer in the order a person should read them: what cuts the result short comes first. */
export function impactNotices(facts: NoticeFacts): ImpactNotice[] {
  const made = [
    overCap(facts),
    timeBudget(facts),
    maxHops(facts),
    stoppedByPerson(facts),
    unfilledChannels(facts),
    assumptions(facts),
    staleInputs(facts),
  ];
  const notices = made.filter((n): n is ImpactNotice => n !== undefined);
  for (const code of facts.warnings) {
    notices.push({
      id: `warning:${code}`,
      tone: "info",
      title: "계산 메모",
      text: warningText(code),
    });
  }
  return notices;
}
