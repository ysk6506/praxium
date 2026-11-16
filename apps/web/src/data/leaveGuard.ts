/**
 * A warning before leaving a form with changes that are not saved. Two ways of leaving are caught:
 * - **Closing the tab or the window, or reloading.** The browser asks by its own words (it does not let a page choose
 *   them); this only says that it should ask.
 * - **Going to another screen** by a link, a button or the back button. The screen has already changed when the
 *   address does, so the guard puts the address back at once and offers the choice: `leave()` goes where the person
 *   was going, `stay()` stays. A change of the same screen's query (a tab, a sort) is not leaving.
 */
import { useEffect, useRef, useState } from "react";
import { replaceHash } from "./router";

export interface PendingLeave {
  /** Where the person was going, as an address hash. */
  readonly target: string;
  /** Go there, and lose what was not saved. */
  readonly leave: () => void;
  /** Stay on the screen. */
  readonly stay: () => void;
}

export interface LeaveGuard {
  /** Set while the person has tried to leave and has not chosen yet; a dialog asks and calls `leave` or `stay`. */
  readonly pending: PendingLeave | undefined;
}

const hashOf = (url: string): string => {
  const at = url.indexOf("#");
  return at < 0 ? "" : url.slice(at);
};
const pathOf = (hash: string): string => hash.replace(/^#/, "").split("?")[0] ?? "";

/** Guard the screen while `dirty` is true (there are changes that are not saved). */
export function useLeaveGuard(dirty: boolean): LeaveGuard {
  const [pending, setPending] = useState<PendingLeave | undefined>(undefined);
  const leaving = useRef(false);
  const restoring = useRef(false);

  useEffect(() => {
    if (!dirty) {
      setPending(undefined);
      return;
    }
    leaving.current = false;
    const beforeUnload = (event: BeforeUnloadEvent): void => {
      event.preventDefault();
      // Older browsers ask only when the event has a return value.
      event.returnValue = "";
    };
    const hashChange = (event: HashChangeEvent): void => {
      if (leaving.current || restoring.current || !event.oldURL || !event.newURL) return;
      const from = hashOf(event.oldURL);
      const to = hashOf(event.newURL);
      if (pathOf(from) === pathOf(to)) return;
      // Put the address back before the screens read it, so nothing on this screen is lost while the person chooses.
      restoring.current = true;
      try {
        replaceHash(from);
      } finally {
        restoring.current = false;
      }
      setPending({
        target: to,
        stay: () => setPending(undefined),
        leave: () => {
          leaving.current = true;
          setPending(undefined);
          globalThis.location.hash = to;
        },
      });
    };
    globalThis.addEventListener("beforeunload", beforeUnload);
    // In the capture phase, so this runs before the listener that reads the address for the screens.
    globalThis.addEventListener("hashchange", hashChange, true);
    return () => {
      globalThis.removeEventListener("beforeunload", beforeUnload);
      globalThis.removeEventListener("hashchange", hashChange, true);
    };
  }, [dirty]);

  return { pending };
}
