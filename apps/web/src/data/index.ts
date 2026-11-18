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
  ObjectRef,
  ObjectType,
  Property,
  QueryMeta,
  ResultComplete,
} from "../api";
export { type Requester, withEtag } from "../api";
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
  currentEtag,
  describeError,
  type ErrorKind,
  type ErrorView,
  errorDetails,
  errorFacts,
  fieldErrorMessage,
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
export { newIdempotencyKey } from "./idempotency";
export { invalidate, onInvalidate } from "./invalidation";
export {
  type AttachedIssues,
  attachIssues,
  childPointer,
  formatPointer,
  getAt,
  hasIssueUnder,
  type Issue,
  insertItem,
  type Json,
  moveItem,
  PointerError,
  parsePointer,
  removeAt,
  removeItem,
  setAt,
  writeValue,
} from "./json-doc";
export { type LeaveGuard, type PendingLeave, useLeaveGuard } from "./leaveGuard";
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
  hashParams,
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
export {
  fallbackTitle,
  PRODUCT,
  ScreenReportsProvider,
  screenKey,
  titleOf,
  useScreen,
  useScreenTitle,
} from "./screen";
export { forwardNavigation, lastChangeWasForward, watchScroll } from "./scroll";
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
export {
  type AsyncOptions,
  type AsyncResult,
  type AsyncState,
  type PollOptions,
  type RefreshableResult,
  useAsync,
} from "./useAsync";
export {
  IDEMPOTENCY_KEY_LENGTH,
  isIdempotencyKey,
  type Mutation,
  type MutationContext,
  type MutationOptions,
  type MutationState,
  useMutation,
} from "./useMutation";
