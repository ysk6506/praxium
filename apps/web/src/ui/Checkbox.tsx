import { Checkbox as BaseCheckbox } from "@base-ui/react/checkbox";
import { type ReactNode, useId } from "react";

export interface CheckboxProps {
  readonly checked: boolean;
  readonly onChange: (checked: boolean) => void;
  readonly children: ReactNode;
}

export function Checkbox({ checked, onChange, children }: CheckboxProps) {
  const id = useId();
  return (
    <div className="flex items-center gap-2 py-1 text-sm">
      <BaseCheckbox.Root
        id={id}
        checked={checked}
        onCheckedChange={(next) => onChange(next)}
        className="flex size-4 shrink-0 items-center justify-center rounded-sm border border-line-strong bg-surface data-[checked]:border-accent data-[checked]:bg-accent"
      >
        <BaseCheckbox.Indicator className="text-xs leading-none text-accent-fg">
          ✓
        </BaseCheckbox.Indicator>
      </BaseCheckbox.Root>
      <label htmlFor={id} className="cursor-pointer">
        {children}
      </label>
    </div>
  );
}
