import { Dialog as BaseDialog } from "@base-ui/react/dialog";
import type { ReactNode } from "react";
import { Button } from "./Button";

export interface DialogProps {
  readonly open: boolean;
  readonly onOpenChange: (open: boolean) => void;
  readonly title: string;
  readonly description?: string;
  readonly closeLabel?: string;
  readonly children: ReactNode;
}

/** A modal dialog: focus stays inside while it is open, Esc closes it, and it comes back to the button that opened it. */
export function Dialog({
  open,
  onOpenChange,
  title,
  description,
  closeLabel = "닫기",
  children,
}: DialogProps) {
  return (
    <BaseDialog.Root open={open} onOpenChange={onOpenChange}>
      <BaseDialog.Portal>
        <BaseDialog.Backdrop className="fixed inset-0 z-40 bg-overlay" />
        <BaseDialog.Popup className="fixed top-1/2 left-1/2 z-50 w-[min(32rem,calc(100vw-2rem))] max-h-[85vh] -translate-x-1/2 -translate-y-1/2 overflow-auto rounded-lg border border-line bg-surface p-5 shadow-pop">
          <BaseDialog.Title className="text-lg font-semibold">{title}</BaseDialog.Title>
          {description ? (
            <BaseDialog.Description className="mt-1 text-sm text-muted">
              {description}
            </BaseDialog.Description>
          ) : null}
          <div className="mt-4">{children}</div>
          <div className="mt-5 flex justify-end">
            <BaseDialog.Close render={<Button variant="secondary" />}>
              {closeLabel}
            </BaseDialog.Close>
          </div>
        </BaseDialog.Popup>
      </BaseDialog.Portal>
    </BaseDialog.Root>
  );
}
