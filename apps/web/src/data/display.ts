/**
 * The display names of the closed values the product itself defines (ADR-0042): the layer of a type, the evidence
 * grade and kind, the freshness, the state and role of a KG, the direction of a link. The API gives the code; the
 * screen shows the Korean name and keeps the code in a tooltip. This is the one place the names are written, with the
 * glossary's words. A value the table does not know is shown as it came.
 *
 * Not here: the names of properties (`lifecycle_state`), their values (`active`) and of types (`PhysicalServer`). They
 * are the data of the model, and the standard model has no Korean display names for them yet; they stay in English.
 */

type Names = Readonly<Record<string, string>>;

export const GRADE_NAMES: Names = {
  fact: "사실",
  assumed: "가정",
  unknown: "알 수 없음",
};

export const BASIS_NAMES: Names = {
  observed: "관측",
  declared: "기재",
  derived: "파생",
  configured: "설정",
};

export const DIRECTION_NAMES: Names = {
  out: "나가는 링크",
  in: "들어오는 링크",
  both: "양방향",
};

/** The layers of the standard model (standard-model.md), named by what they hold. */
export const LAYER_NAMES: Names = {
  facility: "설비",
  hardware: "하드웨어",
  network: "네트워크",
  storage: "스토리지",
  virtualization: "가상화",
  container: "컨테이너",
  platform: "플랫폼",
  service: "서비스",
  cross_cutting: "공통",
  system: "시스템",
};

export const FRESHNESS_NAMES: Names = {
  fresh: "최신",
  stale: "오래됨",
  no_expected_period: "갱신 주기 없음",
};

/** A KG is built in the background and cannot be read until its first load is complete. */
export const KG_STATE_NAMES: Names = {
  serving: "읽기 가능",
  building: "적재 중",
};

export const KG_ROLE_NAMES: Names = {
  production: "운영",
  candidate: "후보",
  test: "테스트",
  system: "시스템",
};

/** The name of a code; a code the table does not know is its own name. */
export const nameOf = (names: Names, code: string): string => names[code] ?? code;

export const layerName = (layer: string): string => nameOf(LAYER_NAMES, layer);
export const freshnessName = (state: string): string => nameOf(FRESHNESS_NAMES, state);
export const kgStateName = (state: string): string => nameOf(KG_STATE_NAMES, state);
export const kgRoleName = (role: string): string => nameOf(KG_ROLE_NAMES, role);
