import { describe, expect, it } from "vitest";
import { generateOperationFiles, OPERATIONS_PER_FILE } from "./operations";
import type { OpenApiDocument } from "./spec";

describe("[REQ-276] the operations are split over files of a fixed size", () => {
  const count = OPERATIONS_PER_FILE + 2;
  const doc: OpenApiDocument = {
    paths: Object.fromEntries(
      Array.from({ length: count }, (_, i) => [
        `/v1/things-${String(i).padStart(2, "0")}`,
        {
          get: {
            operationId: `listThing${String(i).padStart(2, "0")}`,
            parameters:
              i === 0 ? [{ name: "limit", in: "query", schema: { type: "integer" } }] : [],
            responses: {},
          },
        },
      ]),
    ),
  };

  it("fills a file, starts the next one and re-exports every name from operations.ts", () => {
    const files = generateOperationFiles(doc);
    expect(Object.keys(files).sort()).toEqual([
      "operations-01.ts",
      "operations-02.ts",
      "operations.ts",
    ]);
    expect((files["operations-01.ts"] as string).match(/^export const /gm)).toHaveLength(
      OPERATIONS_PER_FILE,
    );
    expect((files["operations-02.ts"] as string).match(/^export const /gm)).toHaveLength(2);
    const barrel = files["operations.ts"] as string;
    expect(barrel).toContain('export type { ListThing00Args } from "./operations-01";');
    expect(barrel).toContain('from "./operations-02";');
    for (let i = 0; i < count; i++)
      expect(barrel).toContain(`listThing${String(i).padStart(2, "0")}`);
  });
});
