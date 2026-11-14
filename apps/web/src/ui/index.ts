/**
 * The parts of the web UI, under Praxium's own names. This is the only place that imports the behavior library
 * (Base UI), and the only place that styles anything: with the names of the tokens, never a color or a size of its
 * own. Screens put these together.
 */
export { Badge, type BadgeTone } from "./Badge";
export { Button, type ButtonVariant } from "./Button";
export { Checkbox } from "./Checkbox";
export { Dialog } from "./Dialog";
export { AppShell, KeyValues, Mono, NavLink, NavSection, PageHeader, Section } from "./layout";
export { Pagination } from "./Pagination";
export { Select } from "./Select";
export { EmptyState, ErrorState, LoadingState } from "./states";
export { Table, TBody, Td, THead, Th, Tr } from "./Table";
export { type TabItem, Tabs } from "./Tabs";
export { TextField } from "./TextField";
