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
  GRADE_STYLES,
  type GradeShape,
  type GradeStyle,
  type GradeTone,
  gradeRank,
  gradeStyle,
  IMPACT_GRADES,
  type ImpactGrade,
  impactGradeName,
  isImpactGrade,
  START_STYLE,
  sortedGrades,
  WAYPOINT_STYLE,
} from "./impact-grades";
export {
  buildOverview,
  type ImpactGroup,
  type ImpactOverview,
  needsOverview,
  OVERVIEW_THRESHOLD,
} from "./impact-group";
export {
  type ImpactDataInput,
  type ImpactDirection,
  type ImpactReading,
  impactSource,
} from "./impact-input";
export {
  LANE_ORDER,
  LANE_WIDTH,
  type Lane,
  type Layout,
  type LayoutInput,
  layoutEdges,
  layoutLanes,
  type Placed,
  type PlacedEdge,
  ROW_HEIGHT,
} from "./impact-layout";
export {
  nodePicture,
  overviewPicture,
  type Picture,
  type PictureEdge,
  type PictureLane,
  type PictureNode,
} from "./impact-picture";
export {
  type ImpactRow,
  impactRows,
  pageOfRows,
  ROWS_PER_PAGE,
  type RowPage,
  type RowSection,
  sectionsByGrade,
  sortRows,
} from "./impact-rows";
export {
  initialSpread,
  type SpreadAction,
  type SpreadPlan,
  type SpreadState,
  spreadDuration,
  spreadPlan,
  spreadReducer,
} from "./impact-spread";
export {
  countByGrade,
  countByStep,
  describeStep,
  type GradeCount,
  laneFirstStep,
  lastStep,
  type StepCount,
} from "./impact-stats";
export {
  buildImpactTree,
  type ImpactEdge,
  type ImpactNode,
  type ImpactTree,
  itemIds,
} from "./impact-tree";
export {
  DEFAULT_IMPACT_VIEW,
  type ImpactRequestBody,
  type ImpactSortKey,
  type ImpactView,
  impactAddress,
  impactViewParams,
  isAnswerable,
  itemsRequest,
  readImpactAddress,
  readImpactView,
  summaryRequest,
} from "./impact-view";
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
export type { AsyncResult, AsyncState } from "./useAsync";
export { type Spread, useReducedMotion, useSpread } from "./useSpread";
