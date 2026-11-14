import { spawn } from "node:child_process";
import { existsSync, mkdtempSync } from "node:fs";
import { tmpdir } from "node:os";
import path from "node:path";
import { describe, expect, test } from "vitest";
import "./test-support/scratch-hook";

describe("a run that is stopped removes its folder [REQ-024]", () => {
  test("SIGTERM removes the folder and the process still ends by the signal", async () => {
    const base = mkdtempSync(path.join(tmpdir(), "scratch-signal-"));
    const code = `const { openScratch } = require(${JSON.stringify(path.join(__dirname, "scratch.ts"))});
      const s = openScratch(${JSON.stringify(base)}); process.stdout.write(s.dir + "\\n"); setInterval(() => {}, 1000);`;
    const child = spawn(process.execPath, ["--import", "tsx", "-e", code], {
      cwd: path.resolve(__dirname, "../.."),
    });
    const dir = await new Promise<string>((resolve) => {
      child.stdout.once("data", (chunk: Buffer) => resolve(chunk.toString().trim()));
    });
    expect(existsSync(dir)).toBe(true);
    const ended = new Promise<string | null>((resolve) => {
      child.once("exit", (_code, signal) => resolve(signal));
    });
    child.kill("SIGTERM");
    expect(await ended).toBe("SIGTERM");
    expect(existsSync(dir)).toBe(false);
  });
});
