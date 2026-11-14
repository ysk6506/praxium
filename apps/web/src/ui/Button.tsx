import { Button as BaseButton } from "@base-ui/react/button";
import type { ComponentProps } from "react";
import { cx } from "./cx";

export type ButtonVariant = "primary" | "secondary" | "ghost";

const VARIANTS: Readonly<Record<ButtonVariant, string>> = {
  primary: "bg-accent text-accent-fg border-accent hover:opacity-90",
  secondary: "bg-surface text-fg border-line-strong hover:bg-sunken",
  ghost: "bg-transparent text-muted border-transparent hover:bg-sunken hover:text-fg",
};

export interface ButtonProps extends ComponentProps<typeof BaseButton> {
  readonly variant?: ButtonVariant;
}

export function Button({
  variant = "secondary",
  className,
  type = "button",
  ...rest
}: ButtonProps) {
  return (
    <BaseButton
      type={type}
      className={cx(
        "inline-flex h-8 items-center justify-center gap-1.5 rounded-md border px-3 text-sm font-medium",
        "cursor-pointer transition-colors data-[disabled]:cursor-not-allowed data-[disabled]:opacity-50",
        VARIANTS[variant],
        typeof className === "string" && className,
      )}
      {...rest}
    />
  );
}
