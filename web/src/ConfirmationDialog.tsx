import { useEffect, useId, useRef, type ReactNode, type RefObject } from "react";
import styles from "./App.module.css";

export function ConfirmationDialog({ open, title, onClose, busy = false, children, actions, returnFocusRef }: {
  open: boolean;
  title: string;
  onClose: () => void;
  busy?: boolean;
  children: ReactNode;
  actions: ReactNode;
  returnFocusRef?: RefObject<HTMLElement | null>;
}) {
  const dialogRef = useRef<HTMLDialogElement>(null);
  const invoker = useRef<HTMLElement | null>(null);
  const selection = useRef<{ start: number; end: number; direction: "forward" | "backward" | "none"; scrollTop: number; scrollLeft: number } | null>(null);
  const titleID = useId();
  useEffect(() => {
    const dialog = dialogRef.current;
    if (!dialog) return;
    if (open && !dialog.open) {
      const active = returnFocusRef?.current ?? document.activeElement;
      invoker.current = active instanceof HTMLElement ? returnFocusRef ? active : active.closest("details")?.querySelector("summary") ?? active : null;
      selection.current = (active instanceof HTMLInputElement || active instanceof HTMLTextAreaElement) && active.selectionStart !== null && active.selectionEnd !== null
        ? { start: active.selectionStart, end: active.selectionEnd, direction: active.selectionDirection ?? "none", scrollTop: active.scrollTop, scrollLeft: active.scrollLeft } : null;
      dialog.showModal();
    }
    if (!open) {
      if (dialog.open) dialog.close();
      if (invoker.current?.isConnected) {
        const target = invoker.current.getClientRects().length ? invoker.current : invoker.current.closest("details")?.querySelector("summary");
        target?.focus({ preventScroll: true });
        if (selection.current && (target instanceof HTMLInputElement || target instanceof HTMLTextAreaElement)) {
          target.setSelectionRange(selection.current.start, selection.current.end, selection.current.direction);
          target.scrollTop = selection.current.scrollTop; target.scrollLeft = selection.current.scrollLeft;
        }
      }
      invoker.current = null;
    }
  }, [open, returnFocusRef]);

  return <dialog ref={dialogRef} className={styles.confirmDialog} aria-labelledby={titleID}
    onClose={event => { event.stopPropagation(); if (open && !event.currentTarget.open) onClose(); }}
    onCancel={event => { event.stopPropagation(); if (busy) event.preventDefault(); }}>
    {open && <div className={styles.confirmBody}>
      <h2 id={titleID}>{title}</h2>
      {children}
      <div className={styles.confirmActions}>{actions}</div>
    </div>}
  </dialog>;
}
