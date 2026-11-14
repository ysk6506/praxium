/**
 * Import this for its effect: from here on `os.tmpdir()` (and the `TMPDIR` of every program the test
 * starts) is a folder of this test file, removed when the file ends, passed or failed, timed out
 * or not (ADR-0036). An import of `mini-repo` brings it in, and so does every other support module.
 */
import { afterAll } from "vitest";
import { openScratch } from "../scratch";

const before = process.env.TMPDIR;
const scratch = openScratch();
process.env.TMPDIR = scratch.dir;

function release(): void {
  if (before === undefined) delete process.env.TMPDIR;
  else process.env.TMPDIR = before;
  scratch.dispose();
}

afterAll(release);
// The hook does not run when the worker is stopped in the middle of a file.
process.once("exit", scratch.dispose);
