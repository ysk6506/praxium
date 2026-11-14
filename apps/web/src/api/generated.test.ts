import { describe, expect, it } from "vitest";
import type { Requester, RequestSpec } from "./client";
import { operationTable } from "./generated/operation-table";
import * as operations from "./generated/operations";

type AnyOperation = (client: Requester, args: unknown) => Promise<unknown>;

const functions = Object.entries(operations).filter(([, value]) => typeof value === "function") as [
  string,
  AnyOperation,
][];

describe("[REQ-276] the generated functions are the operations of the contract", () => {
  it("has one function for every operation in the table and none besides", () => {
    expect(functions.map(([name]) => name).sort()).toEqual(Object.keys(operationTable).sort());
  });

  it("sends each operation to its method and path", async () => {
    for (const [name, call] of functions) {
      const seen: RequestSpec[] = [];
      const client: Requester = {
        async request<T>(spec: RequestSpec): Promise<T> {
          seen.push(spec);
          return undefined as T;
        },
      };
      await call(client, { path: { x: "1" }, query: { y: 1 }, body: { z: 1 } });
      const expected = operationTable[name as keyof typeof operationTable];
      expect([name, seen[0]?.method, seen[0]?.path]).toEqual([
        name,
        expected.method,
        expected.path,
      ]);
    }
  });

  it("marks every read of the contract as safe and names a role for it", () => {
    for (const entry of Object.values(operationTable)) {
      if (entry.method === "GET") expect(entry.safe).toBe(true);
      expect(entry.roles.length).toBeGreaterThan(0);
    }
  });
});
