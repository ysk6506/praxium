/**
 * The API layer: the typed client of the REST contract. Everything under `generated/` is made from the contract by
 * `pnpm web:gen-api`; `client.ts` is the one hand-written part. Names are listed one by one (no `export *`).
 */
export type { ClientOptions, QueryObject, Requester, RequestSpec } from "./client";
export { ApiError, buildUrl, CLIENT_CODES, createClient, serializeQuery } from "./client";
export {
  askFind,
  askGet,
  askNeighbors,
  getObjectType,
  listKGs,
  listObjectTypes,
} from "./generated/operations";
export type {
  AttributeValue,
  Evidence,
  FindData,
  FindItem,
  FindRequest,
  Freshness,
  GetData,
  GetRequest,
  IdentifierItem,
  KG,
  LinkCounts,
  NeighborGroup,
  NeighborItem,
  NeighborsData,
  NeighborsRequest,
  ObjectEvidence,
  ObjectRef,
  ObjectType,
  ObjectTypeSummary,
  Problem,
  Property,
  QueryMeta,
  ResultComplete,
  TypeCount,
  Valid,
} from "./generated/types";
