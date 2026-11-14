/** Paging by cursor: the API gives the way forward only, so the pages already seen are kept to go back. */
import { useCallback, useReducer } from "react";

export interface PagerState {
  readonly limit: number;
  /** The cursor of every page from the first (`undefined`) to the current one. */
  readonly cursors: readonly (string | undefined)[];
}

export type PagerAction =
  | { type: "next"; cursor: string }
  | { type: "previous" }
  | { type: "first" }
  | { type: "limit"; limit: number };

export const PAGE_SIZES = [25, 50, 100] as const;

export const initialPager = (limit: number = PAGE_SIZES[0]): PagerState => ({
  limit,
  cursors: [undefined],
});

export function pagerReducer(state: PagerState, action: PagerAction): PagerState {
  switch (action.type) {
    case "next":
      return { ...state, cursors: [...state.cursors, action.cursor] };
    case "previous":
      return state.cursors.length <= 1 ? state : { ...state, cursors: state.cursors.slice(0, -1) };
    case "first":
      return { ...state, cursors: [undefined] };
    case "limit":
      return { limit: action.limit, cursors: [undefined] };
  }
}

/** The cursor of the current page. */
export const currentCursor = (state: PagerState): string | undefined =>
  state.cursors[state.cursors.length - 1];

/** The 1-based number of the current page. */
export const pageNumber = (state: PagerState): number => state.cursors.length;

export function usePager(limit?: number) {
  const [state, dispatch] = useReducer(pagerReducer, limit, initialPager);
  return {
    state,
    cursor: currentCursor(state),
    page: pageNumber(state),
    hasPrevious: state.cursors.length > 1,
    next: useCallback((cursor: string) => dispatch({ type: "next", cursor }), []),
    previous: useCallback(() => dispatch({ type: "previous" }), []),
    first: useCallback(() => dispatch({ type: "first" }), []),
    setLimit: useCallback((value: number) => dispatch({ type: "limit", limit: value }), []),
  };
}
