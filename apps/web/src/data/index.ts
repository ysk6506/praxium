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
export { columnCandidates, DEFAULT_COLUMN_COUNT, defaultColumns, orderedColumns } from "./columns";
export { connection, useToken } from "./connection";
export {
  FRESHNESS_NAMES,
  freshnessName,
  KG_ROLE_NAMES,
  KG_STATE_NAMES,
  kgRoleName,
  kgStateName,
  LAYER_NAMES,
  layerName,
} from "./display";
export {
  applyEntry,
  DEMO_LANDING,
  type Entry,
  type EntryMode,
  forgetToken,
  parseEntry,
  parseEntryMode,
  saveTypedToken,
  useDemo,
  useEntryMode,
  watchEntry,
} from "./entry";
export {
  type AccessMode,
  describeError,
  type ErrorKind,
  type ErrorView,
  errorDetails,
  errorFacts,
  type Names,
  needsToken,
  withAccessMode,
  withNames,
} from "./errors";
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
  groupKey,
  groupNeighbors,
  groupTotals,
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
export {
  OBJECT_TABS,
  parseRoute,
  repairedHash,
  resolveRoute,
  routeHash,
  useHash,
  useHashParams,
  useRoute,
  withParams,
} from "./router";
export { forwardNavigation, watchScroll } from "./scroll";
export {
  createTrail,
  nextSort,
  type ObjectsView,
  objectsViewParams,
  readObjectsView,
  readTypesView,
  type SortDirection,
  sortTypes,
  type Trail,
  type TypeSortKey,
  type TypesView,
  tabStorage,
  typesViewParams,
} from "./urlstate";
export type { AsyncResult, AsyncState } from "./useAsync";
