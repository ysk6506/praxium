/**
 * Runs the official MCP conformance tests (@modelcontextprotocol/conformance, ADR-0068) against a local demo server.
 * Run it by hand before a release: `pnpm mcp:conformance` (it is not part of `pnpm check`).
 *
 * What it starts, all on 127.0.0.1 and all stopped by their own process id at the end:
 *  1. the demo server (built from backend/cmd/praxium into a temporary folder), with the proxy's own origin allowed,
 *     because the test sends a call from a local origin and expects it to be accepted;
 *  2. a small proxy in front of it that adds the run's token to every request, because the conformance client
 *     cannot send a token and the server refuses a call without one. The proxy changes nothing else.
 *
 * Arguments after `--` go to the conformance command (for example `--scenario ping --verbose`). Known failures,
 * the scenarios of the test suite that need features Praxium does not serve (resources, prompts, example tools),
 * are listed in scripts/mcp-conformance-expected.yaml; a failure that is not listed fails the run, and so does a
 * listed one that now passes.
 */
import { type ChildProcess, spawn, spawnSync } from "node:child_process";
import { mkdtempSync, rmSync } from "node:fs";
import http from "node:http";
import net from "node:net";
import { tmpdir } from "node:os";
import path from "node:path";
import { fileURLToPath } from "node:url";

const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..");

const freePort = (): Promise<number> =>
  new Promise((resolve, reject) => {
    const probe = net.createServer();
    probe.once("error", reject);
    probe.listen(0, "127.0.0.1", () => {
      const { port } = probe.address() as net.AddressInfo;
      probe.close(() => resolve(port));
    });
  });

/** Starts the demo server and resolves with the token it prints for this run. */
const startServer = (
  binary: string,
  port: number,
  origin: string,
): Promise<{ child: ChildProcess; token: string }> =>
  new Promise((resolve, reject) => {
    const child = spawn(
      binary,
      ["demo", "-addr", `127.0.0.1:${port}`, "-scale", "500", "-mcp-allowed-origins", origin],
      {
        stdio: ["ignore", "pipe", "inherit"],
      },
    );
    let seen = "";
    child.stdout.on("data", (chunk: Buffer) => {
      seen += chunk.toString();
      const token = /token of this run is ([0-9a-f]+)/.exec(seen)?.[1];
      if (token) resolve({ child, token });
    });
    child.once("exit", (code) => reject(new Error(`the demo server ended early (exit ${code})`)));
  });

/** A proxy that adds the token and passes everything else through. */
const startProxy = (target: number, token: string, port: number): Promise<http.Server> =>
  new Promise((resolve) => {
    const proxy = http.createServer((req, res) => {
      const headers = { ...req.headers, authorization: `Bearer ${token}` };
      const up = http.request(
        { host: "127.0.0.1", port: target, method: req.method, path: req.url, headers },
        (back) => {
          res.writeHead(back.statusCode ?? 502, back.headers);
          back.pipe(res);
        },
      );
      up.once("error", () => res.writeHead(502).end());
      req.pipe(up);
    });
    proxy.listen(port, "127.0.0.1", () => resolve(proxy));
  });

const main = async (): Promise<number> => {
  const work = mkdtempSync(path.join(tmpdir(), "praxium-conformance-"));
  const binary = path.join(work, "praxium");
  let server: ChildProcess | undefined;
  let proxy: http.Server | undefined;
  try {
    const built = spawnSync("go", ["build", "-o", binary, "./cmd/praxium"], {
      cwd: path.join(root, "backend"),
      stdio: "inherit",
    });
    if (built.status !== 0) return 1;
    const serverPort = await freePort();
    const proxyPort = await freePort();
    const started = await startServer(binary, serverPort, `http://127.0.0.1:${proxyPort}`);
    server = started.child;
    proxy = await startProxy(serverPort, started.token, proxyPort);
    const extra = process.argv.slice(2).filter((arg) => arg !== "--");
    const run = spawn(
      path.join(root, "node_modules", ".bin", "conformance"),
      [
        "server",
        "--url",
        `http://127.0.0.1:${proxyPort}/mcp`,
        "--expected-failures",
        path.join(root, "scripts", "mcp-conformance-expected.yaml"),
        "--output-dir",
        path.join(work, "results"),
        ...extra,
      ],
      { cwd: root, stdio: "inherit" },
    );
    return await new Promise<number>((resolve) => run.once("exit", (code) => resolve(code ?? 1)));
  } finally {
    proxy?.close();
    server?.kill("SIGTERM");
    rmSync(work, { recursive: true, force: true });
  }
};

process.exitCode = await main();
