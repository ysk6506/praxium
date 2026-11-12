/**
 * Lets the worker talk to the test runner between tests (ADR-0140). Some spec files run hundreds of
 * CPU-bound tests in one chain of promises; the worker then never reaches its event loop, the runner's
 * progress message waits more than its 60 s limit and the run ends with "Timeout calling onTaskUpdate"
 * although every test passed. One turn of the event loop per test fixes that. It changes no test,
 * no assertion and no timeout.
 */
import { beforeEach } from "vitest";

// Taken now so a test that fakes timers cannot stop the yield.
const realSetImmediate = globalThis.setImmediate;

beforeEach(
  () =>
    new Promise<void>((resolve) => {
      realSetImmediate(resolve);
    }),
);
