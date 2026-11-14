import { useCallback } from "react";
import {
  hashParams,
  type ImpactView,
  impactViewParams,
  readImpactView,
  useHashParams,
} from "../../data";

/**
 * The view of the impact screen as the address holds it, and a way to change some of it. A change of the question or
 * of the reading gets its own history entry (`push`: back goes to the question before); a selection, a filter, a tab
 * replaces the address.
 */
export function useImpactView(): {
  readonly view: ImpactView;
  readonly change: (patch: Partial<ImpactView>, how?: "push" | "replace") => void;
} {
  const { params, set } = useHashParams();
  const view = readImpactView(params);
  const change = useCallback(
    (patch: Partial<ImpactView>, how: "push" | "replace" = "replace") => {
      const now = readImpactView(hashParams(globalThis.location.hash));
      set(impactViewParams({ ...now, ...patch }), how);
    },
    [set],
  );
  return { view, change };
}
