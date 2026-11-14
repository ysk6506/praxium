/**
 * Generate the typed API layer of the web UI from the REST contract (ADR-0035).
 *
 *   pnpm web:gen-api          write apps/web/src/api/generated/*.ts
 *   pnpm web:gen-api --check  fail when the files on disk are not what the contract generates
 *
 * The generator itself is `apps/web/src/api/generator` (pure, no dependencies). This script only reads the contract,
 * formats the text with the repository's formatter and writes it.
 */

import { execFileSync } from "node:child_process";
import { createHash } from "node:crypto";
import { existsSync, mkdirSync, readFileSync, writeFileSync } from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";
import {
  CONTRACT_PATH,
  GENERATED_DIR,
  generateBodies,
  type OpenApiDocument,
  renderFile,
} from "../apps/web/src/api/generator/index";

const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..");
const sha256 = (text: string): string => createHash("sha256").update(text).digest("hex");

function format(file: string, text: string): string {
  const biome = path.join(root, "node_modules/.bin/biome");
  return execFileSync(
    biome,
    ["check", "--write", "--linter-enabled=false", `--stdin-file-path=${file}`],
    {
      cwd: root,
      input: text,
      encoding: "utf8",
    },
  );
}

export function generateAll(): Record<string, string> {
  const contractText = readFileSync(path.join(root, CONTRACT_PATH), "utf8");
  const doc = JSON.parse(contractText) as OpenApiDocument;
  const out: Record<string, string> = {};
  for (const [name, body] of Object.entries(generateBodies(doc))) {
    const formatted = format(`${GENERATED_DIR}/${name}`, body);
    out[`${GENERATED_DIR}/${name}`] = renderFile(formatted, sha256(contractText), sha256);
  }
  return out;
}

const check = process.argv.includes("--check");
let stale = 0;
for (const [file, text] of Object.entries(generateAll())) {
  const full = path.join(root, file);
  if (check) {
    if (!existsSync(full) || readFileSync(full, "utf8") !== text) {
      process.stderr.write(
        `${file} is not what the contract generates; run \`pnpm web:gen-api\`\n`,
      );
      stale++;
    }
  } else {
    mkdirSync(path.dirname(full), { recursive: true });
    writeFileSync(full, text);
    process.stdout.write(`wrote ${file}\n`);
  }
}
process.exit(stale === 0 ? 0 : 1);
