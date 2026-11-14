/**
 * Where the page is scrolled when the screen changes (ADR-0042). A new screen opens at the top. Going back or forward
 * returns to where the person was on that screen. The browser cannot do this for a page whose content arrives after
 * the address changes, so the page keeps the positions itself and tries again while the content is still growing.
 * A change of the query only (the tab, the search, the sort) keeps the position: the person is on the same screen.
 */
import { parseEntryMode } from "./entry";

export interface ScrollWindow {
  readonly scrollY: number;
  readonly location: { readonly hash: string };
  readonly history: { scrollRestoration: "auto" | "manual" };
  scrollTo(x: number, y: number): void;
  addEventListener(type: string, listener: (event: Event) => void, options?: boolean): void;
  removeEventListener(type: string, listener: (event: Event) => void, options?: boolean): void;
  setTimeout(handler: () => void, ms: number): unknown;
}

/** How long a click on a link counts as the cause of the next change of the address. */
const INTENT_MS = 100;
/** How often and how long a restore waits for the content to grow tall enough. */
const RETRY_MS = 50;
const RETRY_TIMES = 40;

const pathOf = (hash: string): string => hash.replace(/^#/, "").split("?")[0] ?? "";

interface HashChange {
  readonly oldURL: string;
  readonly newURL: string;
}

const hashOfUrl = (url: string): string => {
  const at = url.indexOf("#");
  return at < 0 ? "" : url.slice(at);
};

export interface ScrollMemory {
  /** The next change of the address is one the page itself made (a link, a button): the new screen opens at the top. */
  forward(): void;
  dispose(): void;
}

let active: ScrollMemory | undefined;

/** A change of the address that the page makes itself (a button, not a link): call it just before. */
export const forwardNavigation = (): void => active?.forward();

export function watchScroll(win: ScrollWindow): ScrollMemory {
  const positions = new Map<string, number>();
  let intent = false;
  let generation = 0;
  const previous = win.history.scrollRestoration;
  win.history.scrollRestoration = "manual";

  const onScroll = (): void => {
    positions.set(win.location.hash, win.scrollY);
  };

  const restore = (y: number): void => {
    const mine = ++generation;
    let tries = 0;
    const step = (): void => {
      if (mine !== generation) return;
      win.scrollTo(0, y);
      if (Math.abs(win.scrollY - y) > 2 && tries++ < RETRY_TIMES) win.setTimeout(step, RETRY_MS);
    };
    step();
  };

  const onHashChange = (event: Event): void => {
    const { newURL, oldURL } = event as unknown as HashChange;
    const target = hashOfUrl(newURL);
    const before = hashOfUrl(oldURL);
    const fromLink = intent;
    intent = false;
    if (parseEntryMode(target)) {
      restore(0);
      return;
    }
    if (pathOf(target) === pathOf(before) && fromLink) {
      generation++;
      return;
    }
    if (fromLink) restore(0);
    else restore(positions.get(win.location.hash) ?? 0);
  };

  const onClick = (raw: Event): void => {
    const event = raw as MouseEvent;
    if (
      event.defaultPrevented ||
      event.button !== 0 ||
      event.metaKey ||
      event.ctrlKey ||
      event.shiftKey
    )
      return;
    const element = event.target as { closest?: (selector: string) => Element | null } | null;
    const link = element?.closest?.("a[href^='#']");
    if (link) forward();
  };

  function forward(): void {
    intent = true;
    win.setTimeout(() => {
      intent = false;
    }, INTENT_MS);
  }

  win.addEventListener("scroll", onScroll);
  win.addEventListener("hashchange", onHashChange);
  win.addEventListener("click", onClick, true);
  const memory: ScrollMemory = {
    forward,
    dispose() {
      win.removeEventListener("scroll", onScroll);
      win.removeEventListener("hashchange", onHashChange);
      win.removeEventListener("click", onClick, true);
      win.history.scrollRestoration = previous;
      if (active === memory) active = undefined;
    },
  };
  active = memory;
  return memory;
}
