import { AlertDialog } from "@base-ui/react/alert-dialog";
import { type ReactNode, useRef } from "react";
import { Button } from "./Button";

export interface ConfirmDialogProps {
  readonly open: boolean;
  /** Called with `false` when the person cancels, presses Esc or closes the dialog; never when they confirm. */
  readonly onOpenChange: (open: boolean) => void;
  /** Called when the person confirms. The dialog stays open: the caller closes it when the work is done or failed. */
  readonly onConfirm: () => void;
  readonly title: string;
  readonly description?: ReactNode;
  readonly confirmLabel: string;
  readonly cancelLabel?: string;
  /** The action cannot be taken back: the confirm button is marked and the dialog says so in words. */
  readonly danger?: boolean;
  /** The work is going on: both buttons wait, so it cannot be confirmed twice or abandoned half way. */
  readonly pending?: boolean;
  readonly children?: ReactNode;
}

/**
 * Ask before something that matters. A dialog that needs an answer: clicking outside does not close it, focus stays
 * inside and starts on the cancel button (so a quick Enter does not confirm), Esc cancels, and focus goes back to
 * where it was. A `danger` dialog says "되돌릴 수 없습니다" in words as well as in the color of the button.
 */
export function ConfirmDialog({
  open,
  onOpenChange,
  onConfirm,
  title,
  description,
  confirmLabel,
  cancelLabel = "취소",
  danger = false,
  pending = false,
  children,
}: ConfirmDialogProps) {
  const cancel = useRef<HTMLButtonElement>(null);
  return (
    <AlertDialog.Root open={open} onOpenChange={(next) => onOpenChange(next)}>
      <AlertDialog.Portal>
        <AlertDialog.Backdrop className="fixed inset-0 z-40 bg-overlay" />
        <AlertDialog.Popup
          initialFocus={cancel}
          className="fixed top-1/2 left-1/2 z-50 w-[min(28rem,calc(100vw-2rem))] max-h-[85dvh] -translate-x-1/2 -translate-y-1/2 overflow-auto rounded-lg border border-line bg-surface p-5 shadow-pop"
        >
          <AlertDialog.Title className="text-lg font-semibold">{title}</AlertDialog.Title>
          {danger ? (
            <p className="mt-1 text-sm font-semibold text-danger">
              <span className="mr-1">주의</span>되돌릴 수 없습니다.
            </p>
          ) : null}
          {description ? (
            <AlertDialog.Description className="mt-1 text-sm text-muted">
              {description}
            </AlertDialog.Description>
          ) : null}
          {children ? <div className="mt-3">{children}</div> : null}
          <div className="mt-5 flex justify-end gap-2">
            <Button
              ref={cancel}
              variant="secondary"
              onClick={() => onOpenChange(false)}
              disabled={pending}
              focusableWhenDisabled
            >
              {cancelLabel}
            </Button>
            <Button
              variant={danger ? "danger" : "primary"}
              onClick={onConfirm}
              disabled={pending}
              focusableWhenDisabled
            >
              {pending ? "처리하는 중" : confirmLabel}
            </Button>
          </div>
        </AlertDialog.Popup>
      </AlertDialog.Portal>
    </AlertDialog.Root>
  );
}
