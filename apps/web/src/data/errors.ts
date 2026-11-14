/** Turns what went wrong into words a person can act on. The API sends codes; this is where they become Korean. */
import { ApiError, CLIENT_CODES } from "../api";

export type ErrorKind =
  | "auth"
  | "permission"
  | "notFound"
  | "network"
  | "input"
  | "server"
  | "other";

export interface ErrorView {
  readonly kind: ErrorKind;
  readonly title: string;
  readonly message: string;
  /** What to do next, when there is something to do. */
  readonly hint?: string;
  readonly code?: string;
  readonly status?: number;
  readonly traceId?: string;
  /** What the server said about the cause: the fields of the answer that name the field, the message, the target. */
  readonly params?: Readonly<Record<string, unknown>>;
  readonly fieldErrors?: readonly { readonly pointer: string; readonly code: string }[];
}

const BY_CODE: Readonly<Record<string, Pick<ErrorView, "kind" | "title" | "message" | "hint">>> = {
  "auth.token_missing": {
    kind: "auth",
    title: "토큰이 없습니다",
    message: "요청에 토큰이 실리지 않았습니다.",
    hint: "접속 설정에서 개발용 토큰을 입력하십시오.",
  },
  "auth.token_invalid": {
    kind: "auth",
    title: "토큰이 올바르지 않습니다",
    message: "서버가 이 토큰을 받아들이지 않았습니다.",
    hint: "접속 설정에서 서버를 시작할 때 지정한 토큰을 다시 입력하십시오.",
  },
  "auth.token_expired": {
    kind: "auth",
    title: "토큰이 만료되었습니다",
    message: "토큰의 유효 기간이 지났습니다.",
    hint: "접속 설정에서 새 토큰을 입력하십시오.",
  },
  "not_found.resource": {
    kind: "notFound",
    title: "찾을 수 없습니다",
    message: "요청한 항목이 없거나 이 토큰으로는 볼 수 없습니다.",
  },
  "permission.role_missing": {
    kind: "permission",
    title: "권한이 없습니다",
    message: "이 토큰의 역할로는 이 데이터를 읽을 수 없습니다.",
    hint: "읽기(read) 역할이 있는 토큰으로 접속하십시오.",
  },
  "invalid_input.cursor_invalid": {
    kind: "input",
    title: "페이지 위치가 올바르지 않습니다",
    message: "다음 페이지를 가리키는 값이 더는 사용할 수 없습니다.",
    hint: "첫 페이지부터 다시 보십시오.",
  },
  "stale.projection_not_ready": {
    kind: "server",
    title: "아직 읽을 수 없습니다",
    message: "서버가 이 데이터를 아직 준비하지 못했습니다.",
    hint: "잠시 후에 다시 시도하십시오.",
  },
};

const BY_GROUP: Readonly<Record<string, Pick<ErrorView, "kind" | "title" | "message">>> = {
  auth: {
    kind: "auth",
    title: "인증에 실패했습니다",
    message: "서버가 이 요청의 인증을 거절했습니다.",
  },
  permission: {
    kind: "permission",
    title: "권한이 없습니다",
    message: "이 요청을 할 권한이 없습니다.",
  },
  not_found: { kind: "notFound", title: "찾을 수 없습니다", message: "요청한 항목이 없습니다." },
  invalid_input: {
    kind: "input",
    title: "요청이 올바르지 않습니다",
    message: "서버가 요청의 값을 받아들이지 않았습니다.",
  },
  conflict: {
    kind: "other",
    title: "충돌했습니다",
    message: "데이터가 바뀌어 요청을 처리하지 못했습니다.",
  },
  stale: {
    kind: "server",
    title: "데이터가 아직 준비되지 않았습니다",
    message: "요청한 시점의 데이터를 읽을 수 없습니다.",
  },
  rate_limited: {
    kind: "server",
    title: "요청이 너무 많습니다",
    message: "잠시 후에 다시 시도하십시오.",
  },
  server: {
    kind: "server",
    title: "서버에 문제가 있습니다",
    message: "서버가 요청을 처리하지 못했습니다.",
  },
};

/** An error of any kind as something to show. An abort is not an error and never reaches here. */
export function describeError(error: unknown): ErrorView {
  if (!(error instanceof ApiError)) {
    return {
      kind: "other",
      title: "예기치 않은 오류",
      message: error instanceof Error ? error.message : "알 수 없는 오류가 발생했습니다.",
    };
  }
  const base = {
    code: error.code,
    status: error.status,
    traceId: error.traceId,
    params: error.params,
    fieldErrors: error.errors,
  };
  if (error.code === CLIENT_CODES.network) {
    return {
      ...base,
      kind: "network",
      title: "서버에 연결하지 못했습니다",
      message: "API 서버가 응답하지 않습니다.",
      hint: "서버가 실행 중인지, 이 화면을 연 주소가 서버의 주소인지 확인하십시오.",
    };
  }
  if (error.code === CLIENT_CODES.badResponse) {
    return {
      ...base,
      kind: "server",
      title: "응답을 읽을 수 없습니다",
      message: `서버가 API 형식이 아닌 응답을 보냈습니다(HTTP ${error.status}).`,
      hint: "프록시와 서버 설정을 확인하십시오.",
    };
  }
  const known = BY_CODE[error.code];
  if (known) return { ...base, ...known };
  const group = BY_GROUP[error.code.split(".")[0] ?? ""];
  if (group) return { ...base, ...group };
  return { ...base, kind: "other", title: error.title, message: error.code };
}

/** Whether the person has to change the token to get past this error. */
export const needsToken = (view: ErrorView): boolean => view.kind === "auth";

/** How the person came in, as far as the words of an error depend on it (`entry.ts` has the full set). */
export type AccessMode = "demo" | "local" | "direct" | "none";

const NEW_ADDRESS_HINT =
  "서버를 다시 시작하면 접속 링크가 바뀝니다. 터미널에 출력된 새 주소를 여십시오.";

/**
 * An authentication error in the words that fit how the person came in. A demo or a local-mode server makes its token
 * itself and the person never typed one, so the way out is the new address the server printed. A typed token is
 * typed again in the settings. Other errors, and the typed token, are left as they are.
 */
export function withAccessMode(view: ErrorView, mode: AccessMode): ErrorView {
  if (view.kind !== "auth" || (mode !== "demo" && mode !== "local")) return view;
  if (view.code === "auth.token_missing") {
    return {
      ...view,
      message: "요청에 접속 링크의 토큰이 실리지 않았습니다.",
      hint: NEW_ADDRESS_HINT,
    };
  }
  return {
    ...view,
    title: "접속 링크가 더는 맞지 않습니다",
    message: "서버가 이 브라우저에 저장된 토큰을 받아들이지 않았습니다.",
    hint: NEW_ADDRESS_HINT,
  };
}

/** The resources a not-found answer can name, with the Korean noun in the forms the sentences need. */
const RESOURCES: Readonly<Record<string, { object: string; subject: string }>> = {
  kg: { object: "KG를", subject: "KG가" },
  object: { object: "객체를", subject: "객체가" },
  object_type: { object: "객체 타입을", subject: "객체 타입이" },
  link_type: { object: "링크 타입을", subject: "링크 타입이" },
  action_type: { object: "액션 타입을", subject: "액션 타입이" },
  trait: { object: "특성을", subject: "특성이" },
  identifier_scheme: { object: "식별자 체계를", subject: "식별자 체계가" },
  problem: { object: "오류 코드를", subject: "오류 코드가" },
  route: { object: "경로를", subject: "경로가" },
};

/**
 * What a screen knows about what it asked for, by the resource name the server uses (`kg`, `object`, `object_type`).
 * The server names the resource and, in `params.id`, the value it did not find; the screen's own names are for a
 * server that does not send the value yet.
 */
export type Names = Readonly<Partial<Record<"kg" | "object" | "object_type", string>>>;

/** A not-found error that names what was not found. Other errors are left as they are. */
export function withNames(view: ErrorView, names: Names): ErrorView {
  if (view.kind !== "notFound") return view;
  const resource = typeof view.params?.resource === "string" ? view.params.resource : undefined;
  const known = resource ? RESOURCES[resource] : undefined;
  const sent = view.params?.id;
  const name =
    typeof sent === "string" && sent !== ""
      ? sent
      : resource
        ? names[resource as keyof Names]
        : undefined;
  if (!known || !name) return view;
  return {
    ...view,
    title: `${known.object} 찾을 수 없습니다`,
    message: `‘${name}’라는 ${known.subject} 없거나, 이 토큰으로는 볼 수 없습니다.`,
    hint:
      resource === "kg"
        ? "KG 목록에서 읽을 수 있는 KG를 선택하십시오."
        : "주소를 잘못 입력했거나, 다른 KG의 항목은 아닌지 확인하십시오.",
  };
}

const FIELD_KEYS = ["field", "pointer", "parameter"];
const MESSAGE_KEYS = ["message", "detail", "reason"];
const SHOWN_AS_FACT = new Set([...FIELD_KEYS, ...MESSAGE_KEYS]);

const firstText = (params: ErrorView["params"], keys: readonly string[]): string | undefined => {
  for (const key of keys) {
    const value = params?.[key];
    if (typeof value === "string" && value !== "") return value;
  }
  return undefined;
};

/** The cause the server gave, to be read at once: which field was refused and what the server said about it. */
export function errorFacts(view: ErrorView): [label: string, value: string][] {
  const facts: [string, string][] = [];
  const field = firstText(view.params, FIELD_KEYS) ?? view.fieldErrors?.[0]?.pointer;
  if (field) facts.push(["필드", field]);
  const message = firstText(view.params, MESSAGE_KEYS);
  if (message) facts.push(["서버 메시지", message]);
  return facts;
}

const plain = (value: unknown): string =>
  typeof value === "string" ? value : (JSON.stringify(value) ?? String(value));

/** What a person who reports the error needs, folded away on the screen: code, status, trace id, the rest of the answer. */
export function errorDetails(view: ErrorView): [label: string, value: string][] {
  const details: [string, string][] = [];
  if (view.code) details.push(["코드", view.code]);
  if (view.status) details.push(["HTTP 상태", String(view.status)]);
  if (view.traceId) details.push(["추적 ID", view.traceId]);
  for (const [key, value] of Object.entries(view.params ?? {})) {
    if (!SHOWN_AS_FACT.has(key)) details.push([key, plain(value)]);
  }
  for (const problem of view.fieldErrors ?? []) details.push([problem.pointer, problem.code]);
  return details;
}
