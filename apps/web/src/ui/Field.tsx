import { Field as BaseField } from "@base-ui/react/field";
import type { ReactNode } from "react";

export interface FieldFrameProps {
  readonly label: string;
  /** Help that stays under the label: what to type, where the value comes from. Tied to the input as its description. */
  readonly description?: ReactNode;
  /** What is wrong with the value, in words. One or several; empty or absent means nothing is wrong. */
  readonly error?: string | readonly string[];
  /** Marks the field as required: the label says so in words and the input says so to a screen reader. */
  readonly required?: boolean;
  readonly disabled?: boolean;
  readonly children: ReactNode;
}

const errorsOf = (error: FieldFrameProps["error"]): readonly string[] =>
  error === undefined ? [] : typeof error === "string" ? (error === "" ? [] : [error]) : error;

/**
 * What every field of a form shares: a visible label, a description and an error text, all tied to the input in the
 * way a screen reader follows (`aria-describedby`, and `aria-invalid` while there is an error). The input goes inside
 * as a child. The error is an alert, so it is read out when it shows.
 */
export function FieldFrame({
  label,
  description,
  error,
  required,
  disabled,
  children,
}: FieldFrameProps) {
  const errors = errorsOf(error);
  return (
    <BaseField.Root className="flex flex-col gap-1" invalid={errors.length > 0} disabled={disabled}>
      <BaseField.Label className="text-sm font-medium text-fg">
        {label}
        {required ? <span className="ml-1 text-xs font-normal text-muted">(필수)</span> : null}
      </BaseField.Label>
      {children}
      {description ? (
        <BaseField.Description className="text-xs text-muted">{description}</BaseField.Description>
      ) : null}
      {errors.length > 0 ? (
        <BaseField.Error match className="flex flex-col text-xs text-danger" role="alert">
          {errors.map((message) => (
            <span key={message}>
              <span className="mr-1 font-semibold">오류</span>
              {message}
            </span>
          ))}
        </BaseField.Error>
      ) : null}
    </BaseField.Root>
  );
}

/** The look of the edge of an input, a text area and the like: one place, so the fields do not drift apart. */
export const INPUT_CLASS =
  "min-h-8 w-full rounded-md border border-control-line bg-surface px-2.5 text-sm text-fg placeholder:text-subtle aria-[invalid=true]:border-danger";
