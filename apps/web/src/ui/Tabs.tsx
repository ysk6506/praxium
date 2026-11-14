import { Tabs as BaseTabs } from "@base-ui/react/tabs";
import type { ReactNode } from "react";

export interface TabItem<V extends string> {
  readonly value: V;
  readonly label: string;
  readonly content: ReactNode;
}

export interface TabsProps<V extends string> {
  readonly value: V;
  readonly onChange: (value: V) => void;
  readonly items: readonly TabItem<V>[];
}

/** Tabs with arrow-key movement. Only the open tab's content is in the page. */
export function Tabs<V extends string>({ value, onChange, items }: TabsProps<V>) {
  return (
    <BaseTabs.Root
      value={value}
      onValueChange={(next) => {
        if (items.some((i) => i.value === next)) onChange(next as V);
      }}
    >
      <BaseTabs.List className="flex gap-1 border-b border-line">
        {items.map((item) => (
          <BaseTabs.Tab
            key={item.value}
            value={item.value}
            className="-mb-px cursor-pointer border-b-2 border-transparent px-3 py-2 text-sm font-medium text-muted hover:text-fg data-[active]:border-accent data-[active]:text-fg forced-colors:data-[active]:underline"
          >
            {item.label}
          </BaseTabs.Tab>
        ))}
      </BaseTabs.List>
      {items.map((item) => (
        <BaseTabs.Panel key={item.value} value={item.value} className="pt-4">
          {item.content}
        </BaseTabs.Panel>
      ))}
    </BaseTabs.Root>
  );
}
