import { fileURLToPath } from "node:url";
import { defineConfig } from "vitest/config";

// Test configuration is a protected path (harness/config/protected-paths.txt). Every test runs on
// every run: no include/exclude tricks, no "pass with no tests" (rules TCFG001-TCFG003).
export default defineConfig({
  resolve: {
    alias: [
      {
        find: /^@praxium\/core\/(.*)$/,
        replacement: `${fileURLToPath(new URL("./packages/core/src/", import.meta.url))}$1/index.ts`,
      },
    ],
  },
  test: {
    include: [
      "packages/**/src/**/*.test.ts",
      "apps/**/src/**/*.test.{ts,tsx}",
      "harness/src/**/*.test.ts",
      "tests/spec/**/*.test.ts",
    ],
    // One turn of the event loop between tests, so long files keep reporting progress (ADR-0002).
    setupFiles: ["tests/setup/yield-between-tests.ts"],
    environment: "node",
    testTimeout: 20_000,
    coverage: {
      provider: "v8",
      include: [
        "packages/*/src/**",
        "apps/server/src/**",
        "apps/web/src/logic/**",
        "apps/web/src/api/**",
        "apps/web/src/data/**",
        "apps/web/src/ui/**",
        "harness/src/**",
      ],
      exclude: ["**/*.test.ts", "**/*.test.tsx", "**/test-support/**"],
      reporter: ["text-summary", "json-summary", "json"],
      reportsDirectory: "coverage",
    },
  },
});
