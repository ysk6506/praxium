/** Turns what went wrong into words a person can act on. The API sends codes; this is where they become Korean. */
import { ApiError, CLIENT_CODES } from "../api";

export type ErrorKind = "auth" | "permission" | "notFound" | "network" | "input" | "server" | "other";

export interface ErrorView {
  readonly kind: ErrorKind;
  readonly title: string;
  readonly message: string;
  /** What to do next, when there is something to do. */
  readonly hint?: string;
  readonly code?: string;
  readonly status?: number;
  readonly traceId?: string;
}

const BY_CODE: Readonly<Record<string, Pick<ErrorView, "kind" | "title" | "message" | "hint">>> = {
  "auth.token_missing": {
    kind: "auth",
    title: "토큰이 없습니다",
    message: "요청에 토큰이 실리지 않았습니다.",
    hint: "접속 설정에서 개발용 토큰을 입력하세요.",
  },
  "auth.token_invalid": {
    kind: "auth",
    title: "토큰이 맞지 않습니다",
    message: "서버가 이 토큰을 받아들이지 않았습니다.",
    hint: "접속 설정에서 서버를 띄울 때 지정한 토큰을 다시 입력하세요.",
  },
  "auth.token_expired": {
    kind: "auth",
    title: "토큰이 만료되었습니다",
    message: "토큰의 유효 기간이 지났습니다.",
    hint: "접속 설정에서 새 토큰을 입력하세요.",
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
    hint: "읽기(read) 역할이 있는 토큰으로 접속하세요.",
  },
  "invalid_input.cursor_invalid": {
    kind: "input",
    title: "페이지 위치가 맞지 않습니다",
    message: "다음 페이지를 가리키는 값이 더는 쓸 수 없습니다.",
    hint: "첫 페이지부터 다시 보세요.",
  },
  "stale.projection_not_ready": {
    kind: "server",
    title: "아직 읽을 수 없습니다",
    message: "서버가 이 데이터를 아직 준비하지 못했습니다.",
    hint: "잠시 뒤에 다시 시도하세요.",
  },
};

const BY_GROUP: Readonly<Record<string, Pick<ErrorView, "kind" | "title" | "message">>> = {
  auth: { kind: "auth", title: "인증에 실패했습니다", message: "서버가 이 요청의 인증을 거절했습니다." },
  permission: { kind: "permission", title: "권한이 없습니다", message: "이 요청을 할 권한이 없습니다." },
  not_found: { kind: "notFound", title: "찾을 수 없습니다", message: "요청한 항목이 없습니다." },
  invalid_input: { kind: "input", title: "요청이 맞지 않습니다", message: "서버가 요청의 값을 받아들이지 않았습니다." },
  conflict: { kind: "other", title: "충돌했습니다", message: "데이터가 바뀌어 요청을 처리하지 못했습니다." },
  stale: { kind: "server", title: "데이터가 아직 준비되지 않았습니다", message: "요청한 시점의 데이터를 읽을 수 없습니다." },
  rate_limited: { kind: "server", title: "요청이 너무 많습니다", message: "잠시 뒤에 다시 시도하세요." },
  server: { kind: "server", title: "서버에 문제가 있습니다", message: "서버가 요청을 처리하지 못했습니다." },
};

/** An error of any kind as something to show. An abort is not an error and never reaches here. */
export function describeError(error: unknown): ErrorView {
  if (!(error instanceof ApiError)) {
    return {
      kind: "other",
      title: "예기치 않은 오류",
      message: error instanceof Error ? error.message : "알 수 없는 오류가 났습니다.",
    };
  }
  const base = { code: error.code, status: error.status, traceId: error.traceId };
  if (error.code === CLIENT_CODES.network) {
    return {
      ...base,
      kind: "network",
      title: "서버에 연결하지 못했습니다",
      message: "API 서버가 응답하지 않습니다.",
      hint: "서버가 실행 중인지, 이 화면을 연 주소가 서버의 주소인지 확인하세요.",
    };
  }
  if (error.code === CLIENT_CODES.badResponse) {
    return {
      ...base,
      kind: "server",
      title: "응답을 읽을 수 없습니다",
      message: `서버가 API 형식이 아닌 응답을 보냈습니다(HTTP ${error.status}).`,
      hint: "프락시와 서버 설정을 확인하세요.",
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
