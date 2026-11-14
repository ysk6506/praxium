import tailwindcss from "@tailwindcss/vite";
import { defineConfig } from "vite";

// The web UI is a static single-page app (ADR-0035). The build is copied into the Go program, which serves it
// next to the API, so asset paths are relative and nothing is fetched from another origin.
export default defineConfig({
  base: "./",
  plugins: [tailwindcss()],
  esbuild: { jsx: "automatic" },
  build: {
    // The oldest browsers the page is made for (docs/design-docs/web-ui-rules.md, ADR-0045): the floor Tailwind CSS v4
    // documents. A named list, not an ES year, so the output follows the browsers and not a language edition.
    target: ["chrome111", "edge111", "firefox128", "safari16.4"],
    sourcemap: false,
    cssCodeSplit: false,
    rollupOptions: {
      // Base UI marks its files "use client" for server rendering; a static page has no use for the mark.
      onwarn(warning, warn) {
        if (warning.code !== "MODULE_LEVEL_DIRECTIVE") warn(warning);
      },
    },
  },
  // `pnpm --filter @praxium/web dev` talks to a development server started with `praxium api serve`.
  server: { proxy: { "/v1": "http://127.0.0.1:8080" } },
});
