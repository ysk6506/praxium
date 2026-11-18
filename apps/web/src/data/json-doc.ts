/**
 * Tools for a JSON document held as a tree of plain values, addressed by JSON Pointer (RFC 6901). A form edits a
 * document such as a source definition or a mapping: it reads a field with `getAt`, writes it with `writeValue`, and
 * moves the items of a list with `moveItem`. Every function is pure: it returns a new document that shares what did
 * not change, and never touches the one it was given.
 *
 * What the forms rely on:
 * - Keys the form does not know stay where they are, in the same order. The document may hold more than the form shows.
 * - An emptied field is not written as an empty text: `writeValue` takes the key out.
 * - A server error names its place by a pointer into the body; `attachIssues` ties it to the field that shows it.
 */

export type Json =
  | null
  | boolean
  | number
  | string
  | readonly Json[]
  | { readonly [key: string]: Json };

/** A pointer that does not fit the document: it is not a pointer, or it steps into a value that has no children. */
export class PointerError extends Error {
  constructor(message: string) {
    super(message);
    this.name = "PointerError";
  }
}

const INDEX = /^(0|[1-9][0-9]*)$/;

const isList = (node: unknown): node is readonly Json[] => Array.isArray(node);
const isRecord = (node: unknown): node is { readonly [key: string]: Json } =>
  typeof node === "object" && node !== null && !Array.isArray(node);

/** The steps of a pointer: `/a/b~1c` is `["a", "b/c"]` and the empty pointer is the whole document. */
export function parsePointer(pointer: string): string[] {
  if (pointer === "") return [];
  if (!pointer.startsWith("/")) throw new PointerError(`not a JSON pointer: ${pointer}`);
  return pointer
    .slice(1)
    .split("/")
    .map((token) => {
      if (/~(?![01])/.test(token)) throw new PointerError(`bad escape in pointer: ${pointer}`);
      return token.replace(/~1/g, "/").replace(/~0/g, "~");
    });
}

/** The pointer of these steps; the inverse of `parsePointer`. */
export function formatPointer(tokens: readonly string[]): string {
  return tokens.map((token) => `/${token.replace(/~/g, "~0").replace(/\//g, "~1")}`).join("");
}

/** The pointer of the child `key` (or the item at an index) of the value at `pointer`. */
export const childPointer = (pointer: string, key: string | number): string =>
  `${pointer}${formatPointer([String(key)])}`;

function childOf(node: Json | undefined, token: string): Json | undefined {
  if (isList(node)) return INDEX.test(token) ? node[Number(token)] : undefined;
  if (isRecord(node)) return Object.hasOwn(node, token) ? node[token] : undefined;
  return undefined;
}

/** The value at the pointer, or `undefined` when the document has nothing there (`null` is a value). */
export function getAt(doc: Json, pointer: string): Json | undefined {
  let node: Json | undefined = doc;
  for (const token of parsePointer(pointer)) {
    node = childOf(node, token);
    if (node === undefined) return undefined;
  }
  return node;
}

function listIndex(token: string, length: number, pointer: string): number {
  const index = token === "-" ? length : INDEX.test(token) ? Number(token) : Number.NaN;
  if (Number.isNaN(index)) throw new PointerError(`not an index of a list: ${pointer}`);
  if (index > length) throw new PointerError(`the list has no place ${index} yet: ${pointer}`);
  return index;
}

function setIn(
  node: Json | undefined,
  tokens: readonly string[],
  at: number,
  value: Json,
  pointer: string,
): Json {
  const token = tokens[at];
  if (token === undefined) return value;
  // A missing step becomes a list when it is stepped into by an index (or `-`), and an object otherwise.
  const container: Json | undefined =
    node === undefined ? (INDEX.test(token) || token === "-" ? [] : {}) : node;
  if (isList(container)) {
    const index = listIndex(token, container.length, pointer);
    const copy = container.slice();
    copy[index] = setIn(container[index], tokens, at + 1, value, pointer);
    return copy;
  }
  if (isRecord(container)) {
    const next = setIn(childOf(container, token), tokens, at + 1, value, pointer);
    return { ...container, [token]: next };
  }
  throw new PointerError(`a ${typeof container} has no children: ${pointer}`);
}

/**
 * The document with `value` at the pointer. Steps that are missing are made (an object, or a list when the step is an
 * index); a list takes a new item at its end (`/items/-`, or the index equal to its length). A pointer that cannot fit
 * (through a text or a number, past the end of a list) throws a `PointerError`.
 */
export function setAt(doc: Json, pointer: string, value: Json): Json {
  return setIn(doc, parsePointer(pointer), 0, value, pointer);
}

function removeIn(node: Json | undefined, tokens: readonly string[], at: number): Json | undefined {
  const token = tokens[at];
  if (token === undefined || node === undefined) return node;
  const last = at === tokens.length - 1;
  if (isList(node)) {
    if (!INDEX.test(token) || Number(token) >= node.length) return node;
    const index = Number(token);
    if (last) return node.filter((_, i) => i !== index);
    const child = removeIn(node[index], tokens, at + 1);
    if (child === node[index]) return node;
    const copy = node.slice();
    copy[index] = child as Json;
    return copy;
  }
  if (isRecord(node)) {
    if (!Object.hasOwn(node, token)) return node;
    if (last) return Object.fromEntries(Object.entries(node).filter(([key]) => key !== token));
    const child = removeIn(node[token], tokens, at + 1);
    return child === node[token] ? node : { ...node, [token]: child as Json };
  }
  return node;
}

/**
 * The document without what is at the pointer: the key of an object, or an item of a list (the items after it move
 * up). A pointer that leads nowhere changes nothing and gives the same document back. The whole document cannot be
 * removed.
 */
export function removeAt(doc: Json, pointer: string): Json {
  const tokens = parsePointer(pointer);
  if (tokens.length === 0) throw new PointerError("the whole document cannot be removed");
  return removeIn(doc, tokens, 0) as Json;
}

/**
 * What a field writes: a value sets the field; no value (`undefined`) or an empty text takes the key out, so an emptied
 * field does not leave `""` in the document. `0`, `false` and `null` are values and are written.
 */
export function writeValue(doc: Json, pointer: string, value: Json | undefined): Json {
  return value === undefined || value === "" ? removeAt(doc, pointer) : setAt(doc, pointer, value);
}

function listAt(doc: Json, pointer: string): readonly Json[] {
  const list = getAt(doc, pointer);
  if (!isList(list)) throw new PointerError(`not a list: ${pointer}`);
  return list;
}

/** The document with `item` put into the list at `listPointer`, at `index` (the end when it is left out). */
export function insertItem(doc: Json, listPointer: string, item: Json, index?: number): Json {
  const list = listAt(doc, listPointer);
  const at = index ?? list.length;
  if (!Number.isInteger(at) || at < 0 || at > list.length)
    throw new PointerError(`no place ${at} in the list: ${listPointer}`);
  return setAt(doc, listPointer, [...list.slice(0, at), item, ...list.slice(at)]);
}

/** The document without the item at `index` of the list at `listPointer`. */
export function removeItem(doc: Json, listPointer: string, index: number): Json {
  const list = listAt(doc, listPointer);
  if (!Number.isInteger(index) || index < 0 || index >= list.length)
    throw new PointerError(`no item ${index} in the list: ${listPointer}`);
  return setAt(
    doc,
    listPointer,
    list.filter((_, i) => i !== index),
  );
}

/** The document with the item at `from` moved to `to` in the list at `listPointer`; the other items keep their order. */
export function moveItem(doc: Json, listPointer: string, from: number, to: number): Json {
  const list = listAt(doc, listPointer);
  for (const place of [from, to]) {
    if (!Number.isInteger(place) || place < 0 || place >= list.length)
      throw new PointerError(`no item ${place} in the list: ${listPointer}`);
  }
  if (from === to) return doc;
  const rest = list.filter((_, i) => i !== from);
  return setAt(doc, listPointer, [...rest.slice(0, to), list[from] as Json, ...rest.slice(to)]);
}

/** What the server said was wrong about one place of the body: a pointer and a code (`errors[]` of a problem). */
export interface Issue {
  readonly pointer: string;
  readonly code: string;
}

export interface AttachedIssues {
  /** The codes to show at each field, by the field's pointer, in the order the server gave them. */
  readonly byField: ReadonlyMap<string, readonly string[]>;
  /** The issues no field shows: they name a place the form has no field for, or the whole body. */
  readonly unattached: readonly Issue[];
}

const isBelow = (pointer: string, ancestor: string): boolean =>
  ancestor !== "" && pointer.startsWith(`${ancestor}/`);

/**
 * Tie the issues of the server to the fields of the form. An issue goes to the field with the same pointer; when there
 * is none, to the closest field above it (an error at `/items/2/name` shows at the field `/items/2` when the form has
 * no field for the name); when there is none either, it stays unattached, to be shown with the form as a whole.
 */
export function attachIssues(
  issues: readonly Issue[],
  fieldPointers: Iterable<string>,
): AttachedIssues {
  const fields = [...fieldPointers];
  const byField = new Map<string, string[]>();
  const unattached: Issue[] = [];
  for (const issue of issues) {
    let target: string | undefined;
    for (const field of fields) {
      const closer = field === issue.pointer || isBelow(issue.pointer, field);
      if (closer && (target === undefined || field.length > target.length)) target = field;
    }
    if (target === undefined) unattached.push(issue);
    else byField.set(target, [...(byField.get(target) ?? []), issue.code]);
  }
  return { byField, unattached };
}

/** Whether any issue is at or below the pointer: a list shows that one of its items has a problem. */
export const hasIssueUnder = (issues: readonly Issue[], pointer: string): boolean =>
  issues.some((issue) => issue.pointer === pointer || isBelow(issue.pointer, pointer));
