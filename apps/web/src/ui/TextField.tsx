import { Field } from "@base-ui/react/field";
import type { ComponentProps, ReactNode } from "react";
import { cx } from "./cx";

export interface TextFieldProps extends Omit<ComponentProps<"input">, "className"> {
  readonly label: string;
  readonly description?: ReactNode;
  readonly mono?: boolean;
}

/** A labelled one-line input. The label and the description are tied to the input for screen readers. */
export function TextField({ label, description, mono, ...rest }: TextFieldProps) {
  return (
    <Field.Root className="flex flex-col gap-1">
      <Field.Label className="text-sm font-medium text-fg">{label}</Field.Label>
      <Field.Control
        className={cx(
          "h-8 w-full rounded-md border border-line-strong bg-surface px-2.5 text-sm text-fg",
          "placeholder:text-subtle",
          mono && "font-mono",
        )}
        {...rest}
      />
      {description ? (
        <Field.Description className="text-xs text-muted">{description}</Field.Description>
      ) : null}
    </Field.Root>
  );
}
