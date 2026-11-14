/**
 * The way in of a demo server (ADR-0038). `praxium demo` prints an address whose fragment carries a token:
 * `#/connect?token=<token>&demo=1`. A fragment never reaches the server, so the token is in no server log. The page
 * takes the token into the connection store, remembers that the server is a demo, and replaces the address with the
 * first screen before anything is drawn, so the token is neither on screen nor in the history.
 */
import { useSyncExternalStore } from "react";
import {
  browserStorage,
  type ConnectionStore,
  connection,
  type StorageLike,
  useToken,
} from "./connection";

/** The storage key of the demo mark: the token that came in by the demo address. */
export const DEMO_KEY = "praxium.demo-token";
/** Where a demo address lands: the object types of the KG every server names `main`. */
export const DEMO_LANDING = "#/kg/main/types";
const PLAIN_LANDING = "#/kgs";

export interface Entry {
  readonly token: string;
  readonly demo: boolean;
}

/** The entry an address hash carries, or nothing when it is not an entry address or has no token. */
export function parseEntry(hash: string): Entry | undefined {
  const [path = "", query = ""] = hash.replace(/^#/, "").split("?");
  if (path !== "/connect") return undefined;
  const params = new URLSearchParams(query);
  const token = (params.get("token") ?? "").trim();
  if (token === "") return undefined;
  return { token, demo: params.get("demo") === "1" };
}

/**
 * Whether the connection is to a demo server. The mark is the token itself, so a token typed in later, or none,
 * is not a demo. It lives in storage and, for a browser that gives no storage, in memory for the page.
 */
export interface DemoMark {
  mark(token: string): void;
  is(token: string): boolean;
  subscribe(listener: () => void): () => void;
}

export function createDemoMark(storage: StorageLike | undefined): DemoMark {
  const listeners = new Set<() => void>();
  let memory = "";
  try {
    memory = storage?.getItem(DEMO_KEY) ?? "";
  } catch {
    memory = "";
  }
  return {
    mark(token) {
      memory = token;
      try {
        if (token === "") storage?.removeItem(DEMO_KEY);
        else storage?.setItem(DEMO_KEY, token);
      } catch {
        // The mark stays in memory for this page.
      }
      for (const listener of listeners) listener();
    },
    is: (token) => token !== "" && memory === token,
    subscribe(listener) {
      listeners.add(listener);
      return () => listeners.delete(listener);
    },
  };
}

export const demoMark = createDemoMark(browserStorage());

export interface EntryLocation {
  readonly hash: string;
  readonly pathname: string;
  readonly search: string;
}

export interface EntryHistory {
  replaceState(state: null, unused: string, url: string): void;
}

/**
 * Takes the token of an entry address, and the demo mark with it, and replaces the address with the first screen.
 * Returns whether the address was an entry. Call it before the first render.
 */
export function applyEntry(
  location: EntryLocation,
  history: EntryHistory,
  store: ConnectionStore = connection,
  marks: DemoMark = demoMark,
): boolean {
  const entry = parseEntry(location.hash);
  if (!entry) return false;
  store.setToken(entry.token);
  marks.mark(entry.demo ? store.getToken() : "");
  history.replaceState(
    null,
    "",
    `${location.pathname}${location.search}${entry.demo ? DEMO_LANDING : PLAIN_LANDING}`,
  );
  return true;
}

/** Whether the token now in use came in by a demo address; the component renders again when it changes. */
export function useDemo(store: ConnectionStore = connection, marks: DemoMark = demoMark): boolean {
  const token = useToken(store);
  return useSyncExternalStore(marks.subscribe, () => marks.is(token));
}
