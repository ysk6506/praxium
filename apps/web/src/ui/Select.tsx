import { Select as BaseSelect } from "@base-ui/react/select";

export interface SelectOption<V extends string | number> {
  readonly value: V;
  readonly label: string;
}

export interface SelectProps<V extends string | number> {
  readonly label: string;
  readonly value: V;
  readonly options: readonly SelectOption<V>[];
  readonly onChange: (value: V) => void;
}

/** A drop-down list. `label` names it for screen readers and for the person who tabs to it. */
export function Select<V extends string | number>({
  label,
  value,
  options,
  onChange,
}: SelectProps<V>) {
  return (
    <BaseSelect.Root<V>
      value={value}
      onValueChange={(next) => {
        if (next !== null) onChange(next);
      }}
      items={options.map((o) => ({ value: o.value, label: o.label }))}
    >
      <BaseSelect.Trigger
        aria-label={label}
        className="inline-flex h-8 min-w-24 cursor-pointer items-center justify-between gap-2 rounded-md border border-line-strong bg-surface px-2.5 text-sm text-fg hover:bg-sunken"
      >
        <BaseSelect.Value />
        <span aria-hidden className="text-xs text-subtle">
          ▾
        </span>
      </BaseSelect.Trigger>
      <BaseSelect.Portal>
        <BaseSelect.Positioner sideOffset={4} className="z-50">
          <BaseSelect.Popup className="min-w-[var(--anchor-width)] rounded-md border border-line bg-surface p-1 shadow-pop">
            {options.map((option) => (
              <BaseSelect.Item
                key={option.value}
                value={option.value}
                className="flex cursor-pointer items-center rounded-sm px-2 py-1 text-sm text-fg data-[highlighted]:bg-accent-soft data-[selected]:font-medium"
              >
                <BaseSelect.ItemText>{option.label}</BaseSelect.ItemText>
              </BaseSelect.Item>
            ))}
          </BaseSelect.Popup>
        </BaseSelect.Positioner>
      </BaseSelect.Portal>
    </BaseSelect.Root>
  );
}
