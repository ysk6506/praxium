import { StrictMode } from "react";
import { createRoot } from "react-dom/client";
import { App } from "./App";
import { applyEntry } from "./data";

// A demo address carries its token after the #: take it and replace the address before the first screen is drawn.
applyEntry(globalThis.location, globalThis.history);

const root = document.getElementById("root");
if (root) {
  createRoot(root).render(
    <StrictMode>
      <App />
    </StrictMode>,
  );
}
