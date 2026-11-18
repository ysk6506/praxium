import { Field } from "@base-ui/react/field";
import type { ComponentProps, ReactNode } from "react";
import { cx } from "./cx";
import { FieldFrame, INPUT_CLASS } from "./Field";
import { TextField, type TextFieldProps } from "./TextField";

interface FieldTexts {
  readonly label: string;
  readonly description?: ReactNode;
  readonly error?: string | readonly string[];
}

export interface TextAreaFieldProps
  extends FieldTexts,
    Omit<ComponentProps<"textarea">, "className"> {
  readonly mono?: boolean;
}

/** A labelled input of several lines. Enter makes a new line, so it never submits the form. */
export function TextAreaField({
  label,
  description,
  error,
  mono,
  required,
  rows = 4,
  ...rest
}: TextAreaFieldProps) {
  return (
    <FieldFrame
      label={label}
      description={description}
      error={error}
      required={required}
      disabled={rest.disabled}
    >
      <Field.Control
        render={<textarea rows={rows} />}
        required={required}
        className={cx(INPUT_CLASS, "py-1.5", mono && "font-mono")}
        {...(rest as object)}
      />
    </FieldFrame>
  );
}

export interface NumberInputProps extends FieldTexts {
  /** The number, or `null` while the box is empty. */
  readonly value: number | null;
  readonly onChange: (value: number | null) => void;
  readonly min?: number;
  readonly max?: number;
  readonly step?: number;
  readonly required?: boolean;
  readonly disabled?: boolean;
}

/**
 * A labelled input for a number. A box that is empty gives `null`, never `0`, and the digits are written as they are
 * (a port reads `8080`, not `8,080`). It is the browser's own number input, in the field of this file: Base UI has a
 * richer one (`NumberField`, with step buttons and dragging), but it adds about 4.9 KB to the gzipped bundle (ADR-0072)
 * for what a configuration form does not need.
 */
export function NumberInput({
  label,
  description,
  error,
  value,
  onChange,
  min,
  max,
  step,
  required,
  disabled,
}: NumberInputProps) {
  return (
    <FieldFrame
      label={label}
      description={description}
      error={error}
      required={required}
      disabled={disabled}
    >
      <Field.Control
        type="number"
        inputMode="decimal"
        value={value ?? ""}
        min={min}
        max={max}
        step={step}
        required={required}
        disabled={disabled}
        onChange={(event) => {
          const text = event.target.value;
          onChange(text === "" || Number.isNaN(Number(text)) ? null : Number(text));
        }}
        className={cx(INPUT_CLASS, "font-mono")}
      />
    </FieldFrame>
  );
}

export type SecretFieldProps = Omit<
  TextFieldProps,
  "type" | "autoComplete" | "mono" | "name" | "spellCheck"
>;

/**
 * A labelled input for a value that must not leak: a token, a password, a key. The box shows dots, the browser is
 * told not to fill it in or remember it, and it has no `name`. The field itself keeps the value nowhere: it never
 * writes it into the address (an address is logged and shared), into the browser's storage (that outlives the page)
 * or into a cookie. A screen that has to keep a secret does it in its own code, on purpose and in one place (the
 * connection settings keep the access token so), and never puts it in the address.
 */
export function SecretField(props: SecretFieldProps) {
  return (
    <TextField
      {...props}
      type="password"
      mono
      autoComplete="off"
      spellCheck={false}
      autoCapitalize="off"
    />
  );
}
