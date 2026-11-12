/**
 * Counts that only the running core code can tell (DOC095): printed as JSON so the docs check
 * (harness/src/checks/doc-facts.ts, which may not import core) can run this file in a child process, and only
 * when a document states one of these facts. Add a fact here when a document starts to quote a count.
 */
import { isMain } from "../harness/src/common";

export const coreFacts = (): Record<string, number> => ({});

if (isMain(import.meta.url)) process.stdout.write(`${JSON.stringify(coreFacts())}\n`);
