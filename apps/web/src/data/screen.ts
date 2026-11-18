/**
 * What the page says about the screen it shows, for the browser tab, the history list and a screen reader (WCAG 2.4.2,
 * 2.4.3, 4.1.3). The page loads once and the router only changes the address, so nothing else would tell them that
 * the screen changed: the title is set here, and after a move the person made (a link, a button) focus goes to the
 * heading of the new screen. Back and forward keep the focus where the browser leaves it; a polite message says
 * where the person arrived instead. A change of the query only (the tab, the sort, the page) is the same screen.
 */
import { createContext, useCallback, useContext, useEffect, useRef, useState } from "react";
import { type Route, routeHash } from "./router";
import { lastChangeWasForward } from "./scroll";

export const PRODUCT = "프락시움";
const SEPARATOR = " · ";

/** A title of the page: the parts from the most specific to the product name, e.g. `PhysicalServer · 객체 타입 · 프락시움`. */
export const titleOf = (...parts: readonly string[]): string => [...parts, PRODUCT].join(SEPARATOR);

/** What a screen is called before it has read what it shows (the name of an object arrives with the object). */
export function fallbackTitle(route: Route): string {
  switch (route.page) {
    case "settings":
      return titleOf("접속 설정");
    case "kgs":
      return titleOf("KG 목록");
    case "types":
      return titleOf("객체 타입", route.kg);
    case "objects":
      return titleOf(`${route.type} 객체`, route.kg);
    case "object":
      return titleOf("객체", route.kg);
    case "notFound":
      return titleOf("페이지를 찾을 수 없습니다");
  }
}

/** The screen a route shows, without its query: two routes with the same key are one screen. */
export const screenKey = (route: Route): string =>
  routeHash(route).replace(/^#/, "").split("?")[0] ?? "";

type Report = (title: string | undefined) => void;
const noReport: Report = () => undefined;
const ScreenReports = createContext<Report>(noReport);
export const ScreenReportsProvider = ScreenReports.Provider;

/**
 * A screen tells the page what it is called once it knows (`titleOf(...)`), e.g. an object screen with the object's
 * name. Until then, and when it is left, the title is the one of the route.
 */
export function useScreenTitle(title: string | undefined): void {
  const report = useContext(ScreenReports);
  useEffect(() => {
    report(title);
    return () => report(undefined);
  }, [report, title]);
}

interface Reported {
  readonly key: string;
  readonly title: string;
}

/** Whether focus is where a screen change left it, so moving it to the heading takes nothing from the person. */
function focusIsFree(main: Element): boolean {
  const active = document.activeElement;
  return active === null || active === document.body || active === main;
}

export interface Screen {
  /** For `ScreenReportsProvider`: the screens report their title through it. */
  readonly report: Report;
  /** The title of the page now. */
  readonly title: string;
  /** What a screen reader is told after back or forward; empty at other times. */
  readonly announcement: string;
}

/** Keep the title of the page, the focus and the announcement for the route shown. Call once, where the router is. */
export function useScreen(route: Route): Screen {
  const key = screenKey(route);
  const keyNow = useRef(key);
  keyNow.current = key;
  const [reported, setReported] = useState<Reported | undefined>(undefined);
  const [arrived, setArrived] = useState<string | undefined>(undefined);
  const shown = useRef(key);
  const wantsFocus = useRef(false);
  const report = useCallback<Report>((title) => {
    setReported(title === undefined ? undefined : { key: keyNow.current, title });
  }, []);
  const title = reported?.key === key ? reported.title : fallbackTitle(route);

  useEffect(() => {
    document.title = title;
  }, [title]);

  useEffect(() => {
    if (shown.current === key) return;
    shown.current = key;
    if (lastChangeWasForward()) {
      wantsFocus.current = true;
      const main = document.querySelector("main");
      if (main instanceof HTMLElement) main.focus({ preventScroll: true });
    } else {
      wantsFocus.current = false;
      setArrived(key);
    }
  }, [key]);

  // After every render: the heading of the new screen may have appeared (a screen that reads first has none yet).
  useEffect(() => {
    if (!wantsFocus.current) return;
    const main = document.querySelector("main");
    if (!main || !focusIsFree(main)) {
      wantsFocus.current = false;
      return;
    }
    const heading = main.querySelector("h1");
    if (heading instanceof HTMLElement) {
      wantsFocus.current = false;
      heading.focus({ preventScroll: true });
    }
  });

  return { report, title, announcement: arrived === key ? title : "" };
}
