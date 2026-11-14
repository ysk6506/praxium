/**
 * Where the person is, kept in the address after `#`. A hash is read by the browser itself, so the page is served as
 * one static file and the server needs no route for it. Pure functions to read and write it, one hook to follow it.
 */
import { useCallback, useSyncExternalStore } from "react";

export type ObjectTab = "attributes" | "identifiers" | "neighbors" | "evidence";

export const OBJECT_TABS: readonly ObjectTab[] = [
  "attributes",
  "identifiers",
  "neighbors",
  "evidence",
];

export type Route =
  | { readonly page: "settings" }
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

function subscribeHash(listener: () => void): () => void {
  globalThis.addEventListener("hashchange", listener);
  return () => globalThis.removeEventListener("hashchange", listener);
}

const currentHash = (): string => globalThis.location.hash;

/** The route of the page now, and a function to go to another. */
export function useRoute(hasToken: boolean): { route: Route; go: (route: Route) => void } {
  const hash = useSyncExternalStore(subscribeHash, currentHash);
  const go = useCallback((next: Route) => {
    globalThis.location.hash = routeHash(next);
  }, []);
  return { route: parseRoute(hash, hasToken), go };
}
