/**
 * The data layer: what the screens read from the API and how they keep track of it (the connection, the address, the
 * loading and error states, the paging). It knows no component. Screens import this and `ui`, nothing else.
 */

export type {
  AttributeValue,
  FindItem,
  GetData,
  IdentifierItem,
  KG,
  NeighborItem,
  ObjectType,
  Property,
  QueryMeta,
  ResultComplete,
} from "../api";
export { type TypeEntry, useKgs, useObjectTypeDetail, useTypeCatalog } from "./catalog";
export { columnCandidates, DEFAULT_COLUMN_COUNT, defaultColumns } from "./columns";
export { connection, useToken } from "./connection";
export {
  applyEntry,
  DEMO_LANDING,
  type Entry,
  parseEntry,
  useDemo,
} from "./entry";
export { describeError, type ErrorKind, type ErrorView, needsToken } from "./errors";
export {
  basisLabel,
  directionLabel,
  EMPTY_CELL,
  formatCount,
  formatQuantity,
  formatTimestamp,
  formatValue,
  gradeLabel,
  gradeTone,
  shortId,
} from "./format";
export {
  groupNeighbors,
  NEIGHBOR_STEPS,
  type NeighborGroupView,
  type ObjectDetail,
  type RowDetail,
  useNeighbors,
  useObject,
  useObjectPage,
  useRowDetails,
} from "./objects";
export { PAGE_SIZES, usePager } from "./pager";
export type { ObjectTab, Route } from "./router";
export { OBJECT_TABS, parseRoute, routeHash, useRoute } from "./router";
export type { AsyncResult, AsyncState } from "./useAsync";
