/** The docs contract (harness/config/docs.yaml) and the list of documents it governs. */
import { existsSync } from "node:fs";
import path from "node:path";
import { loadYaml } from "../common";
import { listFiles } from "../files";

const CONFIG_PATH = "harness/config/docs.yaml";

export interface DocsConfig {
  docs_root: string;
  index: string;
  root_docs: string[];
  adr_dir: string;
  generated_dir: string;
  archive_dir: string;
  kinds: string[];
  statuses: string[];
  required_keys: string[];
  max_age_days: Record<string, number | null>;
  max_lines: Record<string, number>;
  path_roots: string[];
  root_files: string[];
  /** Spec whose bold goal sentence must lead `goal_docs` (DOC093). */
  goal_source?: string;
  goal_docs?: string[];
  decisions_file?: string;
  /** Folders of the generated list in the index and the heading each gets (ADR-0194). */
  index_groups?: Array<{ dir: string; title: string }>;
}

export const loadDocsConfig = (root: string): DocsConfig =>
  loadYaml<DocsConfig>(path.join(root, CONFIG_PATH));

export function discoverDocs(root: string, config: DocsConfig): string[] {
  const rootDocs = config.root_docs.filter((name) => existsSync(path.join(root, name)));
  const skipped = [`${config.generated_dir}/`, `${config.archive_dir}/`];
  const nested = listFiles(root, [config.docs_root], (name) => name.endsWith(".md")).filter(
    (f) => !skipped.some((s) => f.startsWith(s)),
  );
  return [...rootDocs, ...nested];
}
