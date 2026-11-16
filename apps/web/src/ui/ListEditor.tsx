import { type ReactNode, useEffect, useId, useRef } from "react";
import { Button } from "./Button";
import { cx } from "./cx";

export interface ListEditorProps<T> {
  /** What the list is, such as "헤더". Names the group for a screen reader. */
  readonly label: string;
  readonly items: readonly T[];
  readonly onChange: (items: readonly T[]) => void;
  /** Draw one item. Its inputs call `onChange` of the list with the item replaced; the editor adds the buttons. */
  readonly renderItem: (item: T, index: number) => ReactNode;
  /** A new, empty item for "더하기". */
  readonly newItem: () => T;
  /** What the person calls one item, such as "헤더": the buttons say "헤더 2 지우기". Defaults to "항목". */
  readonly itemName?: string;
  /** What is wrong with the item at this place, in words, or `undefined` for none. */
  readonly itemErrors?: readonly (string | undefined)[];
  /** What is wrong with the list as a whole (too few items, for one). */
  readonly error?: string;
  /** Enter in an input of an item adds a new item after it, unless the person is still composing a letter. */
  readonly addOnEnter?: boolean;
  readonly emptyText?: string;
}

/** Whether this key press only confirms a letter of an input method (Korean, Japanese, Chinese). Safari reports 229. */
export const isComposingKey = (event: { isComposing?: boolean; keyCode?: number }): boolean =>
  event.isComposing === true || event.keyCode === 229;

const FOCUSABLE = "input, textarea, select, [contenteditable='true']";

/**
 * A list of items a person adds to, removes from and puts in order. Each item has its own buttons (up, down, remove),
 * named by the item ("헤더 2 위로 옮기기"), so the whole list works from the keyboard; Alt+Up and Alt+Down in an
 * item's input move it as well. Focus stays on the moved item, goes to the new item after "더하기", and to the next
 * item (or to "더하기") after a removal. The item's own inputs come from `renderItem`.
 */
export function ListEditor<T>({
  label,
  items,
  onChange,
  renderItem,
  newItem,
  itemName = "항목",
  itemErrors,
  error,
  addOnEnter = false,
  emptyText = "항목이 없습니다.",
}: ListEditorProps<T>) {
  // Each item keeps its own key while the list is reordered, so the item that is moved keeps its inputs and its focus.
  const keys = useRef<number[]>([]);
  const counter = useRef(0);
  while (keys.current.length < items.length) keys.current.push(counter.current++);
  keys.current.length = items.length;

  const rows = useRef(new Map<number, HTMLLIElement>());
  const addButton = useRef<HTMLButtonElement>(null);
  const focusAfter = useRef<{ key: number } | "add" | undefined>(undefined);
  useEffect(() => {
    const target = focusAfter.current;
    focusAfter.current = undefined;
    if (target === undefined) return;
    if (target === "add") addButton.current?.focus();
    else rows.current.get(target.key)?.querySelector<HTMLElement>(FOCUSABLE)?.focus();
  });

  const id = useId();
  const nameOf = (index: number): string => `${itemName} ${index + 1}`;

  function add(at: number): void {
    const key = counter.current++;
    keys.current.splice(at, 0, key);
    focusAfter.current = { key };
    const next = [...items];
    next.splice(at, 0, newItem());
    onChange(next);
  }

  function remove(index: number): void {
    const [gone] = keys.current.splice(index, 1);
    const following = keys.current[index] ?? keys.current[index - 1];
    focusAfter.current = following === undefined || gone === undefined ? "add" : { key: following };
    onChange(items.filter((_, i) => i !== index));
  }

  function move(from: number, to: number): void {
    if (to < 0 || to >= items.length) return;
    const [key] = keys.current.splice(from, 1);
    keys.current.splice(to, 0, key as number);
    const next = [...items];
    const [item] = next.splice(from, 1);
    next.splice(to, 0, item as T);
    onChange(next);
  }

  // The keys of an item's input reach the list through one listener on the group, so an item's own inputs stay
  // plain inputs. It reads the latest list from `latest`, which every render refreshes.
  const group = useRef<HTMLFieldSetElement>(null);
  const latest = useRef({ add, move, addOnEnter });
  latest.current = { add, move, addOnEnter };
  useEffect(() => {
    const element = group.current;
    if (!element) return;
    const onKeyDown = (event: KeyboardEvent): void => {
      const target = event.target as HTMLElement;
      if (!target.matches(FOCUSABLE)) return;
      const row = target.closest("li");
      const key = [...rows.current].find(([, li]) => li === row)?.[0];
      const index = key === undefined ? -1 : keys.current.indexOf(key);
      if (index < 0) return;
      if (event.altKey && (event.key === "ArrowUp" || event.key === "ArrowDown")) {
        event.preventDefault();
        latest.current.move(index, index + (event.key === "ArrowUp" ? -1 : 1));
        return;
      }
      if (event.key === "Enter" && target.tagName === "INPUT" && latest.current.addOnEnter) {
        // Enter that confirms a letter of an input method is not a command.
        if (isComposingKey(event)) return;
        event.preventDefault();
        latest.current.add(index + 1);
      }
    };
    element.addEventListener("keydown", onKeyDown);
    return () => element.removeEventListener("keydown", onKeyDown);
  }, []);

  return (
    <fieldset
      ref={group}
      className="flex min-w-0 flex-col gap-2 border-0 p-0"
      aria-describedby={error ? `${id}-error` : undefined}
    >
      <legend className="mb-1 text-sm font-medium text-fg">{label}</legend>
      {items.length === 0 ? <p className="text-sm text-muted">{emptyText}</p> : null}
      <ol className="flex flex-col gap-2">
        {items.map((item, index) => {
          const key = keys.current[index] as number;
          const problem = itemErrors?.[index];
          const errorId = `${id}-item-${key}-error`;
          return (
            <li
              key={key}
              ref={(element) => {
                if (element) rows.current.set(key, element);
                else rows.current.delete(key);
              }}
              className={cx(
                "flex flex-col gap-1 rounded-md border bg-surface p-2",
                problem ? "border-danger" : "border-line",
              )}
            >
              <fieldset
                aria-describedby={problem ? errorId : undefined}
                className="m-0 flex min-w-0 flex-wrap items-start gap-2 border-0 p-0"
              >
                <legend className="sr-only">{nameOf(index)}</legend>
                <div className="min-w-0 flex-1">{renderItem(item, index)}</div>
                <div className="flex shrink-0 gap-1">
                  <Button
                    variant="ghost"
                    aria-label={`${nameOf(index)} 위로 옮기기`}
                    focusableWhenDisabled
                    disabled={index === 0}
                    onClick={() => move(index, index - 1)}
                  >
                    위로
                  </Button>
                  <Button
                    variant="ghost"
                    aria-label={`${nameOf(index)} 아래로 옮기기`}
                    focusableWhenDisabled
                    disabled={index === items.length - 1}
                    onClick={() => move(index, index + 1)}
                  >
                    아래로
                  </Button>
                  <Button
                    variant="ghost"
                    aria-label={`${nameOf(index)} 지우기`}
                    onClick={() => remove(index)}
                  >
                    지우기
                  </Button>
                </div>
              </fieldset>
              {problem ? (
                <p id={errorId} role="alert" className="text-xs text-danger">
                  <span className="mr-1 font-semibold">오류</span>
                  {problem}
                </p>
              ) : null}
            </li>
          );
        })}
      </ol>
      {error ? (
        <p id={`${id}-error`} role="alert" className="text-xs text-danger">
          <span className="mr-1 font-semibold">오류</span>
          {error}
        </p>
      ) : null}
      <div>
        <Button ref={addButton} onClick={() => add(items.length)}>
          {itemName} 더하기
        </Button>
      </div>
    </fieldset>
  );
}
