import { useEffect, type RefObject } from "react";

type SwipeDirection = "left" | "right";

export function useDrawerSwipe(dialogRef: RefObject<HTMLDialogElement | null>, direction: SwipeDirection) {
  useEffect(() => {
    const currentDialog = dialogRef.current;
    if (!currentDialog) return;
    const dialog: HTMLDialogElement = currentDialog;

    const sign = direction === "right" ? 1 : -1;
    let gesture: { id: number; x: number; y: number; time: number; dragging: boolean } | null = null;
    let settling = false;
    let timer: number | undefined;

    function reset() {
      if (timer !== undefined) window.clearTimeout(timer);
      timer = undefined;
      gesture = null;
      settling = false;
      dialog.style.animation = "";
      dialog.style.transition = "";
      dialog.style.transform = "";
      dialog.style.userSelect = "";
    }

    function settle(shouldClose: boolean) {
      settling = true;
      const duration = window.matchMedia("(prefers-reduced-motion: reduce)").matches ? 0 : 180;
      dialog.style.transition = duration ? `transform ${duration}ms ease-out` : "none";
      dialog.style.transform = shouldClose ? `translate3d(${sign * 100}%, 0, 0)` : "translate3d(0, 0, 0)";
      timer = window.setTimeout(() => {
        if (shouldClose && dialog.open) dialog.close();
        else if (dialog.open) {
          dialog.style.transition = "";
          dialog.style.transform = "";
          dialog.style.userSelect = "";
          settling = false;
        }
      }, duration);
    }

    function onPointerDown(event: PointerEvent) {
      if (event.pointerType !== "touch" || !event.isPrimary || !dialog.open || settling) return;
      const bounds = dialog.getBoundingClientRect();
      if (event.clientX < bounds.left || event.clientX > bounds.right ||
          event.clientY < bounds.top || event.clientY > bounds.bottom) return;
      if (event.target instanceof Element && event.target.closest("button, a, label, input, select, textarea, [contenteditable]")) return;
      gesture = { id: event.pointerId, x: event.clientX, y: event.clientY, time: event.timeStamp, dragging: false };
    }

    function onPointerMove(event: PointerEvent) {
      if (!gesture || gesture.id !== event.pointerId) return;
      const dx = event.clientX - gesture.x;
      const dy = event.clientY - gesture.y;
      if (!gesture.dragging) {
        if (Math.abs(dy) > 12 && Math.abs(dy) > Math.abs(dx)) { gesture = null; return; }
        if (Math.abs(dx) < 12 || Math.abs(dx) <= Math.abs(dy)) return;
        if (dx * sign <= 0) { gesture = null; return; }
        gesture.dragging = true;
        dialog.setPointerCapture(event.pointerId);
        dialog.style.animation = "none";
        dialog.style.transition = "none";
        dialog.style.userSelect = "none";
      }
      dialog.style.transform = `translate3d(${Math.max(0, dx * sign) * sign}px, 0, 0)`;
    }

    function onPointerUp(event: PointerEvent) {
      if (!gesture || gesture.id !== event.pointerId) return;
      const { x, time, dragging } = gesture;
      gesture = null;
      if (!dragging) return;
      const distance = (event.clientX - x) * sign;
      const speed = distance / Math.max(1, event.timeStamp - time);
      settle(distance >= Math.min(120, dialog.clientWidth * 0.28) || (distance >= 35 && speed >= 0.6));
    }

    function onPointerCancel(event: PointerEvent) {
      if (!gesture || gesture.id !== event.pointerId) return;
      const dragging = gesture.dragging;
      gesture = null;
      if (dragging) settle(false);
    }

    function onLostPointerCapture(event: PointerEvent) {
      if (event.target === dialog) onPointerCancel(event);
    }

    dialog.addEventListener("pointerdown", onPointerDown);
    dialog.addEventListener("pointermove", onPointerMove);
    dialog.addEventListener("pointerup", onPointerUp);
    dialog.addEventListener("pointercancel", onPointerCancel);
    dialog.addEventListener("lostpointercapture", onLostPointerCapture);
    dialog.addEventListener("close", reset);
    return () => {
      dialog.removeEventListener("pointerdown", onPointerDown);
      dialog.removeEventListener("pointermove", onPointerMove);
      dialog.removeEventListener("pointerup", onPointerUp);
      dialog.removeEventListener("pointercancel", onPointerCancel);
      dialog.removeEventListener("lostpointercapture", onLostPointerCapture);
      dialog.removeEventListener("close", reset);
      reset();
    };
  }, [dialogRef, direction]);
}
