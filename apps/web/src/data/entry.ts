/**
 * The ways into the web UI (ADR-0042, ADR-0079). The server prints an address whose fragment carries a token:
 * `#/connect?token=<token>&mode=local` for the local mode. A fragment
 * never reaches the server, so the token is in no server log. The page takes the token into the connection store,
 * remembers how the person came in, and replaces the address with the first screen, so the token is neither on screen
 * nor in the history. It does so for the address the page was opened with and for every address pasted into the tab
 * later (the fragment changes, the page does not load again). A token typed in the settings is the other way, "direct".
 * What the page shows about the way in (the banner, the badge, the words of an authentication error) comes from
 * `useEntryMode` and nowhere else.
 */
import { useSyncExternalStore } from "react";
import {
  browserStorage,
  type ConnectionStore,
  connection,
  type StorageLike,
  useToken,
} from "./connection";

/** The storage key of the local mark: the token that came in by the local-mode address. */
export const LOCAL_KEY = "praxium.local-token";
const LANDING = "#/kgs";

/** How the person came in: by the local-mode address, or with a token typed in. */
export type EntryMode = "local" | "direct";

/** The token and the mode an entry address carries, or nothing when it is not an entry address or has no token. */
export function parseEntryMode(hash: string): { token: string; mode: EntryMode } | undefined {
  const [path = "", query = ""] = hash.replace(/^#/, "").split("?");
  if (path !== "/connect") return undefined;
  const params = new URLSearchParams(query);
  const token = (params.get("token") ?? "").trim();
  if (token === "") return undefined;
  return { token, mode: params.get("mode") === "local" ? "local" : "direct" };
}

/**
 * Whether the connection came in by one kind of address. The mark is the token itself, so a token typed in later, or
 * none, is not marked. It lives in storage and, for a browser that gives no storage, in memory for the page.
 */
export interface EntryMark {
  mark(token: string): void;
  is(token: string): boolean;
  subscribe(listener: () => void): () => void;
}

export function createMark(storage: StorageLike | undefined, key: string): EntryMark {
  const listeners = new Set<() => void>();
  let memory = "";
  try {
    memory = storage?.getItem(key) ?? "";
  } catch {
    memory = "";
  }
  return {
    mark(token) {
      memory = token;
      try {
        if (token === "") storage?.removeItem(key);
        else storage?.setItem(key, token);
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

export const localMark = createMark(browserStorage(), LOCAL_KEY);

export interface EntryLocation {
  readonly hash: string;
  readonly pathname: string;
  readonly search: string;
}

export interface EntryHistory {
  replaceState(state: null, unused: string, url: string): void;
}

/**
 * Takes the token of an entry address, and the mark of how it came in, and replaces the address with the first screen.
 * Returns whether the address was an entry. Call it before the first render, and again for every later change of the
 * fragment (`watchEntry`). The address and the mark are set before the token, so whoever hears of the new token
 * already sees the new address and the new mode.
 */
export function applyEntry(
  location: EntryLocation,
  history: EntryHistory,
  store: ConnectionStore = connection,
  localMarks: EntryMark = localMark,
): boolean {
  const entry = parseEntryMode(location.hash);
  if (!entry) return false;
  localMarks.mark(entry.mode === "local" ? entry.token : "");
  history.replaceState(null, "", `${location.pathname}${location.search}${LANDING}`);
  store.setToken(entry.token);
  return true;
}

export interface EntryWindow {
  readonly location: EntryLocation;
  readonly history: EntryHistory;
  addEventListener(type: "hashchange", listener: () => void): void;
  removeEventListener(type: "hashchange", listener: () => void): void;
}

/**
 * Takes an entry address pasted into the tab while the page is open: a change of the fragment does not load the page
 * again, so the first-load call of `applyEntry` is not enough. Register it before anything that reads the address, so
 * the others see the address after it was replaced.
 */
export function watchEntry(
  target: EntryWindow,
  store: ConnectionStore = connection,
  localMarks: EntryMark = localMark,
): () => void {
  const onChange = (): void => {
    applyEntry(target.location, target.history, store, localMarks);
  };
  target.addEventListener("hashchange", onChange);
  return () => target.removeEventListener("hashchange", onChange);
}

/** The token typed in the settings: it is a direct entry, so the mark of an earlier address goes. */
export function saveTypedToken(
  token: string,
  store: ConnectionStore = connection,
  localMarks: EntryMark = localMark,
): void {
  localMarks.mark("");
  store.setToken(token);
}

/** Forgets the token and how it came in, in the browser's storage too. */
export function forgetToken(
  store: ConnectionStore = connection,
  localMarks: EntryMark = localMark,
): void {
  localMarks.mark("");
  store.clear();
}

/** How the token now in use came in; `none` when there is no token. The component renders again when it changes. */
export function useEntryMode(
  store: ConnectionStore = connection,
  localMarks: EntryMark = localMark,
): EntryMode | "none" {
  const token = useToken(store);
  const local = useSyncExternalStore(localMarks.subscribe, () => localMarks.is(token));
  if (token === "") return "none";
  return local ? "local" : "direct";
}
