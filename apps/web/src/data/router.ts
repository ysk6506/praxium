/**
 * Where the person is, kept in the address after `#`. A hash is read by the browser itself, so the page is served as
 * one static file and the server needs no route for it. Pure functions to read and write it, one hook to follow it.
 */
import { useCallback, useEffect, useSyncExternalStore } from "react";
import { forwardNavigation } from "./scroll";

export type ObjectTab = "attributes" | "identifiers" | "neighbors" | "evidence";

export const OBJECT_TABS: readonly ObjectTab[] = [
  "attributes",
  "identifiers",
  "neighbors",
  "evidence",
];

export type Route =
  | { readonly page: "settings" }
  | { readonly page: "notFound"; readonly path: string }
  | { readonly page: "kgs" }
  | { readonly page: "types"; readonly kg: string }
  | { readonly page: "objects"; readonly kg: string; readonly type: string }
  | { readonly page: "object"; readonly kg: string; readonly id: string; readonly tab: ObjectTab };

const decode = (part: string): string => {
  try {
    return decodeURIComponent(part);
  } catch {
    return part;
  }
};

/** The route of an address hash. With no token yet, the way in is the connection settings. */
export function parseRoute(hash: string, hasToken: boolean): Route {
  const [path = "", query = ""] = hash.replace(/^#/, "").split("?");
  const parts = path.split("/").filter(Boolean).map(decode);
  const [head, kg, section, third] = parts;
  if (head === "settings") return { page: "settings" };
  if (!hasToken) return { page: "settings" };
  if (head === "kg" && kg !== undefined) {
    if (section === "types" && third !== undefined) return { page: "objects", kg, type: third };
    if (section === "types") return { page: "types", kg };
    if (section === "objects" && third !== undefined) {
      const tab = new URLSearchParams(query).get("tab");
      const known = OBJECT_TABS.find((t) => t === tab);
      return { page: "object", kg, id: third, tab: known ?? "attributes" };
    }
    return { page: "types", kg };
  }
  return { page: "kgs" };
}

/** The address hash of a route; `parseRoute(routeHash(r), true)` is `r`. */
export function routeHash(route: Route): string {
  const enc = encodeURIComponent;
  switch (route.page) {
    case "notFound":
      return `#${route.path}`;
    case "settings":
      return "#/settings";
    case "kgs":
      return "#/kgs";
    case "types":
      return `#/kg/${enc(route.kg)}/types`;
    case "objects":
      return `#/kg/${enc(route.kg)}/types/${enc(route.type)}`;
    case "object": {
      const tab = route.tab === "attributes" ? "" : `?tab=${route.tab}`;
      return `#/kg/${enc(route.kg)}/objects/${enc(route.id)}${tab}`;
    }
  }
}

const queryOf = (hash: string): string => hash.replace(/^#/, "").split("?")[1] ?? "";
const pathOf = (hash: string): string => hash.replace(/^#/, "").split("?")[0] ?? "";

/** The query of an address hash, the part after `?`. */
export const hashParams = (hash: string): URLSearchParams => new URLSearchParams(queryOf(hash));

/** An address hash with its query replaced: the pairs with a value are kept, `undefined` ones are left out. */
export function withParams(
  hash: string,
  update: Readonly<Record<string, string | undefined>>,
): string {
  const params = hashParams(hash);
  for (const [key, value] of Object.entries(update)) {
    if (value === undefined) params.delete(key);
    else params.set(key, value);
  }
  const query = params.toString();
  return `#${pathOf(hash)}${query === "" ? "" : `?${query}`}`;
}

const SECTIONS = new Set(["types", "objects"]);

/**
 * The route of an address hash, including the one for an address that names no screen: `notFound` with the path as it
 * was written. `parseRoute` is the lenient reading (it falls back to the KG list); this is the one a person sees.
 * An address with no path (`""`, `#`, `#/`) is the start page, the KG list.
 */
export function resolveRoute(hash: string, hasToken: boolean): Route {
  const route = parseRoute(hash, hasToken);
  if (!hasToken) return route;
  const path = pathOf(hash);
  const parts = path.split("/").filter(Boolean).map(decode);
  const [head, kg, section, third, extra] = parts;
  if (parts.length === 0 || head === "kgs" || head === "settings") {
    return parts.length > 1 ? { page: "notFound", path } : route;
  }
  if (head === "connect") return { page: "settings" };
  const known =
    head === "kg" &&
    kg !== undefined &&
    extra === undefined &&
    (section === undefined ||
      (SECTIONS.has(section) && (section === "objects" ? third !== undefined : true)));
  return known ? route : { page: "notFound", path };
}

/**
 * The address a lenient reading corrected: an object address with a tab the screen does not have shows the first
 * tab, so the address says so too. Nothing when the address needs no correction.
 */
export function repairedHash(hash: string, hasToken: boolean): string | undefined {
  const route = resolveRoute(hash, hasToken);
  if (route.page !== "object") return undefined;
  const tab = hashParams(hash).get("tab");
  if (tab === null || OBJECT_TABS.some((t) => t === tab)) return undefined;
  return withParams(hash, { tab: undefined });
}

/** The address changed by a replacement does not fire `hashchange`; this event tells the readers of the address. */
const REPLACED = "praxium:hash-replaced";

function subscribeHash(listener: () => void): () => void {
  globalThis.addEventListener("hashchange", listener);
  globalThis.addEventListener(REPLACED, listener);
  return () => {
    globalThis.removeEventListener("hashchange", listener);
    globalThis.removeEventListener(REPLACED, listener);
  };
}

const currentHash = (): string => globalThis.location.hash;

/** Replace the address in place (no history entry) and tell whoever reads it. */
export function replaceHash(hash: string): void {
  const { pathname, search } = globalThis.location;
  globalThis.history.replaceState(null, "", `${pathname}${search}${hash}`);
  globalThis.dispatchEvent(new Event(REPLACED));
}

/** The address hash now, kept up to date. */
export function useHash(): string {
  return useSyncExternalStore(subscribeHash, currentHash);
}

/** The route of the page now, and a function to go to another. */
export function useRoute(hasToken: boolean): { route: Route; go: (route: Route) => void } {
  const hash = useHash();
  const go = useCallback((next: Route) => {
    forwardNavigation();
    globalThis.location.hash = routeHash(next);
  }, []);
  useEffect(() => {
    const repaired = repairedHash(hash, hasToken);
    if (repaired !== undefined) replaceHash(repaired);
  }, [hash, hasToken]);
  return { route: resolveRoute(hash, hasToken), go };
}

/**
 * The state of a screen that lives in the query of the address (the page, the search, the sort): it survives the back
 * button, a reload and a shared link. `set` changes some of the pairs (`undefined` removes one); a change a person
 * would want to go back over gets its own history entry (`push`), a change that follows typing replaces the address.
 */
export function useHashParams(): {
  params: URLSearchParams;
  set: (update: Readonly<Record<string, string | undefined>>, how?: "push" | "replace") => void;
} {
  const hash = useHash();
  const set = useCallback(
    (update: Readonly<Record<string, string | undefined>>, how: "push" | "replace" = "replace") => {
      const next = withParams(globalThis.location.hash, update);
      if (how === "push") {
        forwardNavigation();
        globalThis.location.hash = next;
      } else replaceHash(next);
    },
    [],
  );
  return { params: hashParams(hash), set };
}
