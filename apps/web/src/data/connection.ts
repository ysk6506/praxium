/**
 * The connection to the API: the development token the person types in. The browser keeps it between visits.
 * Only the development authenticator exists (ADR-0024), and its token is made up for a development server; it is
 * not a credential of a person. Storage can be missing or refuse (private windows), so every access is guarded and
 * the token then lives for the page only.
 */
import { useSyncExternalStore } from "react";

export const TOKEN_KEY = "praxium.dev-token";

export interface StorageLike {
  getItem(key: string): string | null;
  setItem(key: string, value: string): void;
  removeItem(key: string): void;
}

export interface ConnectionStore {
  getToken(): string;
  setToken(token: string): void;
  clear(): void;
  subscribe(listener: () => void): () => void;
}

/** The browser's local storage, or nothing when the browser does not give it out. */
export function browserStorage(): StorageLike | undefined {
  try {
    return globalThis.localStorage;
  } catch {
    return undefined;
  }
}

export function createConnectionStore(storage: StorageLike | undefined): ConnectionStore {
  const listeners = new Set<() => void>();
  let memory = "";
  try {
    memory = storage?.getItem(TOKEN_KEY) ?? "";
  } catch {
    memory = "";
  }
  const write = (token: string): void => {
    memory = token;
    try {
      if (token === "") storage?.removeItem(TOKEN_KEY);
      else storage?.setItem(TOKEN_KEY, token);
    } catch {
      // The token stays in memory for this page.
    }
    for (const listener of listeners) listener();
  };
  return {
    getToken: () => memory,
    setToken: (token) => write(token.trim()),
    clear: () => write(""),
    subscribe(listener) {
      listeners.add(listener);
      return () => listeners.delete(listener);
    },
  };
}

export const connection = createConnectionStore(browserStorage());

/** The token now in use; the component renders again when it changes. */
export function useToken(store: ConnectionStore = connection): string {
  return useSyncExternalStore(store.subscribe, store.getToken);
}
