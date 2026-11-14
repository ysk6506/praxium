import { StrictMode } from "react";
import { createRoot } from "react-dom/client";
import { App } from "./App";
import { applyEntry, watchEntry, watchScroll } from "./data";

// A server's address carries its token after the #: take it and replace the address before the first screen is drawn,
// and take the next one that is pasted into this tab (the page does not load again for a change of the fragment).
applyEntry(globalThis.location, globalThis.history);
watchEntry(globalThis);
watchScroll(globalThis);

const root = document.getElementById("root");
if (root) {
  createRoot(root).render(
    <StrictMode>
      <App />
    </StrictMode>,
  );
}
