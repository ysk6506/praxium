import tailwindcss from "@tailwindcss/vite";
import { defineConfig } from "vite";

// The web UI is a static single-page app (ADR-0035). The build is copied into the Go program, which serves it
// next to the API, so asset paths are relative and nothing is fetched from another origin.
export default defineConfig({
  base: "./",
  plugins: [tailwindcss()],
  esbuild: { jsx: "automatic" },
  build: { target: "es2022", sourcemap: false, cssCodeSplit: false },
  // `pnpm --filter @praxium/web dev` talks to a development server started with `praxium api serve`.
  server: { proxy: { "/v1": "http://127.0.0.1:8080" } },
});
