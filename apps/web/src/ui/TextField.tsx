import { Field } from "@base-ui/react/field";
import type { ComponentProps, ReactNode } from "react";
import { cx } from "./cx";
import { FieldFrame, INPUT_CLASS } from "./Field";

export interface TextFieldProps extends Omit<ComponentProps<"input">, "className"> {
  readonly label: string;
  readonly description?: ReactNode;
  /** What is wrong with the value, in words. Shown under the input and read out as an alert. */
  readonly error?: string | readonly string[];
  readonly mono?: boolean;
}

/** A labelled one-line input. The label, the description and the error are tied to the input for screen readers. */
export function TextField({ label, description, error, mono, required, ...rest }: TextFieldProps) {
  return (
    <FieldFrame
      label={label}
      description={description}
      error={error}
      required={required}
      disabled={rest.disabled}
    >
      <Field.Control
        className={cx(INPUT_CLASS, mono && "font-mono")}
        required={required}
        {...rest}
      />
    </FieldFrame>
  );
}
