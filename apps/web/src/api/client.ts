/** The HTTP client under the generated functions: one `request` that knows the token, the paths and the problem format. */
import type { Problem } from "./generated/types";

export type QueryValue =
  | string
  | number
  | boolean
  | null
  | undefined
  | readonly QueryValue[]
  | QueryObject;
export interface QueryObject {
  readonly [key: string]: QueryValue;
}

export interface RequestSpec {
  readonly method: string;
  /** The contract path with `{name}` placeholders, such as `/v1/kgs/{kg}/objects/{object_id}`. */
  readonly path: string;
  readonly pathParams?: Readonly<Record<string, string | number>>;
  readonly query?: QueryObject;
  readonly body?: unknown;
}

/** What the generated functions need from a client. */
export interface Requester {
  request<T>(spec: RequestSpec): Promise<T>;
}

export interface ClientOptions {
  /** Where the API is; empty means the origin that served the page. */
  readonly baseUrl?: string;
  /** The bearer token. Absent means the request carries no credentials. */
  readonly token?: string;
  readonly fetch?: typeof fetch;
  readonly signal?: AbortSignal;
}

/** A problem answer of the API (RFC 9457), or a failure to get an answer at all (status 0). */
export class ApiError extends Error {
  readonly status: number;
  readonly code: string;
  readonly title: string;
  readonly traceId: string | undefined;
  readonly params: Readonly<Record<string, unknown>>;
  readonly errors: readonly { readonly pointer: string; readonly code: string }[];

  constructor(init: {
    status: number;
    code: string;
    title: string;
    traceId?: string;
    params?: Readonly<Record<string, unknown>>;
    errors?: readonly { readonly pointer: string; readonly code: string }[];
  }) {
    super(`${init.code}: ${init.title}`);
    this.name = "ApiError";
    this.status = init.status;
    this.code = init.code;
    this.title = init.title;
    this.traceId = init.traceId;
    this.params = init.params ?? {};
    this.errors = init.errors ?? [];
  }
}

/** Codes of failures that never reached the API's problem format. */
export const CLIENT_CODES = {
  network: "client.network",
  badResponse: "client.bad_response",
} as const;

function pairs(name: string, value: QueryValue): string[] {
  if (value === undefined || value === null) return [];
  if (Array.isArray(value)) {
    const items = value.filter((v) => v !== undefined && v !== null);
    return items.length === 0
      ? []
      : [`${name}=${items.map((v) => encodeURIComponent(String(v))).join(",")}`];
  }
  if (typeof value === "object") {
    return Object.entries(value).flatMap(([key, child]) =>
      pairs(`${name}[${key}]`, child as QueryValue),
    );
  }
  return [`${name}=${encodeURIComponent(String(value))}`];
}

/** The query string (without `?`): arrays join with commas, objects use the deepObject style `filter[a][equals]=x`. */
export function serializeQuery(query: QueryObject | undefined): string {
  if (!query) return "";
  return Object.entries(query)
    .flatMap(([name, value]) => pairs(name, value))
    .join("&");
}

/** The URL of a request: placeholders filled and escaped, the query appended. */
export function buildUrl(baseUrl: string, spec: RequestSpec): string {
  const path = spec.path.replace(/\{([^}]+)\}/g, (_, name: string) => {
    const value = spec.pathParams?.[name];
    return encodeURIComponent(value === undefined ? "" : String(value));
  });
  const query = serializeQuery(spec.query);
  return `${baseUrl.replace(/\/$/, "")}${path}${query === "" ? "" : `?${query}`}`;
}

function isProblem(value: unknown): value is Problem {
  if (typeof value !== "object" || value === null) return false;
  const v = value as Record<string, unknown>;
  return typeof v.code === "string" && typeof v.title === "string" && typeof v.status === "number";
}

async function errorOf(response: Response): Promise<ApiError> {
  let body: unknown;
  try {
    body = await response.json();
  } catch {
    body = undefined;
  }
  if (isProblem(body)) {
    return new ApiError({
      status: body.status,
      code: body.code,
      title: body.title,
      traceId: body.trace_id,
      params: body.params,
      errors: body.errors,
    });
  }
  return new ApiError({
    status: response.status,
    code: CLIENT_CODES.badResponse,
    title: response.statusText || `HTTP ${response.status}`,
  });
}

/** A client over `fetch`. The token goes in the `Authorization` header of every request and nowhere else. */
export function createClient(options: ClientOptions = {}): Requester {
  const doFetch = options.fetch ?? ((input, init) => fetch(input, init));
  return {
    async request<T>(spec: RequestSpec): Promise<T> {
      const headers: Record<string, string> = { Accept: "application/json" };
      if (options.token) headers.Authorization = `Bearer ${options.token}`;
      if (spec.body !== undefined) headers["Content-Type"] = "application/json";
      let response: Response;
      try {
        response = await doFetch(buildUrl(options.baseUrl ?? "", spec), {
          method: spec.method,
          headers,
          body: spec.body === undefined ? undefined : JSON.stringify(spec.body),
          signal: options.signal,
        });
      } catch (cause) {
        if (cause instanceof DOMException && cause.name === "AbortError") throw cause;
        throw new ApiError({
          status: 0,
          code: CLIENT_CODES.network,
          title: "The API could not be reached.",
        });
      }
      if (response.status === 204 || response.status === 304) return undefined as T;
      if (!response.ok) throw await errorOf(response);
      try {
        return (await response.json()) as T;
      } catch {
        throw new ApiError({
          status: response.status,
          code: CLIENT_CODES.badResponse,
          title: "The answer is not JSON.",
        });
      }
    },
  };
}
