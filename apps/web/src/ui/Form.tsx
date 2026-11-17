import { type FormEvent, type ReactNode, useEffect, useRef, useState } from "react";
import { Button } from "./Button";
import { isComposingKey } from "./ListEditor";

export interface FormProps {
  /**
   * Send the form. Give back `true` when it was saved and `false` when it was not: the form then moves focus to the
   * first field that is marked invalid, or to the failure notice when no field is to blame. Checking the values
   * before sending is part of this function; the submit button is never turned off for it.
   */
  readonly onSubmit: () => boolean | Promise<boolean>;
  /** Names the form for a screen reader. */
  readonly label: string;
  readonly className?: string;
  readonly children: ReactNode;
}

/** Where focus goes after a failed submit: the first invalid field, else the failure notice. */
function focusFirstProblem(form: HTMLFormElement): void {
  const field = form.querySelector<HTMLElement>('[aria-invalid="true"]');
  if (field) field.focus();
  else form.querySelector<HTMLElement>("[data-form-result]")?.focus();
}

/**
 * A form of fields and a submit button. The browser's own checks are off (`noValidate`): the fields are checked by
 * the code that sends, and the words come from the same place as the server's. A failed submit sends focus to the
 * first field to fix. Enter in a one-line input submits the form, but Enter that only confirms a letter of an input
 * method (Korean, for one) does not.
 */
export function Form({ onSubmit, label, className, children }: FormProps) {
  const form = useRef<HTMLFormElement>(null);
  const [failures, setFailures] = useState(0);
  useEffect(() => {
    if (failures > 0 && form.current) focusFirstProblem(form.current);
  }, [failures]);

  async function submit(event: FormEvent): Promise<void> {
    event.preventDefault();
    if (!(await onSubmit())) setFailures((n) => n + 1);
  }

  // Enter that only confirms a letter of an input method must not send the form (Safari reports it after the letter
  // is confirmed, with `keyCode` 229). The browser sends a form on the key press, so the press is stopped here.
  useEffect(() => {
    const element = form.current;
    if (!element) return;
    const keyDown = (event: KeyboardEvent): void => {
      const target = event.target as HTMLElement;
      if (event.key === "Enter" && target.tagName === "INPUT" && isComposingKey(event)) {
        event.preventDefault();
      }
    };
    element.addEventListener("keydown", keyDown);
    return () => element.removeEventListener("keydown", keyDown);
  }, []);

  return (
    <form ref={form} aria-label={label} className={className} noValidate onSubmit={submit}>
      {children}
    </form>
  );
}

export interface SubmitButtonProps {
  readonly children: ReactNode;
  /** The send is going on. The button waits meanwhile, and says so; it is never turned off before. */
  readonly pending?: boolean;
  readonly pendingLabel?: string;
}

/** The button that sends a form. It is always there to press; it waits only while a send is going on. */
export function SubmitButton({
  children,
  pending = false,
  pendingLabel = "저장하는 중",
}: SubmitButtonProps) {
  return (
    <Button type="submit" variant="primary" disabled={pending} focusableWhenDisabled>
      {pending ? pendingLabel : children}
    </Button>
  );
}
