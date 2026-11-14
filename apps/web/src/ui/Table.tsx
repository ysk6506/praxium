import type { ComponentProps } from "react";
import { cx } from "./cx";

/** A dense table inside a frame that scrolls sideways when the columns do not fit. */
export function Table({ className, children, ...rest }: ComponentProps<"table">) {
  return (
    <div className="overflow-x-auto rounded-lg border border-line bg-surface">
      <table className={cx("w-full border-collapse text-left text-sm", className)} {...rest}>
        {children}
      </table>
    </div>
  );
}

export function THead(props: ComponentProps<"thead">) {
  return <thead className="bg-sunken" {...props} />;
}

export function TBody(props: ComponentProps<"tbody">) {
  return <tbody className="divide-y divide-line" {...props} />;
}

export function Tr({ className, ...rest }: ComponentProps<"tr">) {
  return <tr className={cx("hover:bg-sunken/60", className)} {...rest} />;
}

export function Th({ className, ...rest }: ComponentProps<"th">) {
  return (
    <th
      scope="col"
      className={cx(
        "whitespace-nowrap border-b border-line px-3 py-2 text-xs font-semibold text-muted",
        className,
      )}
      {...rest}
    />
  );
}

export function Td({ className, ...rest }: ComponentProps<"td">) {
  return <td className={cx("px-3 py-1.5 align-top", className)} {...rest} />;
}
