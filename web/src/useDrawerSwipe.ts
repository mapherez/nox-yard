import { useEffect, type RefObject } from "react";

type SwipeDirection = "left" | "right";

export function useDrawerSwipe<T extends HTMLElement>(
  elementRef: RefObject<T | null>, direction: SwipeDirection, active: boolean, dismiss: () => void,
) {
  useEffect(() => {
    const currentElement = elementRef.current;
    if (!active || !currentElement) return;
    const element: HTMLElement = currentElement;

    const sign = direction === "right" ? 1 : -1;
    let gesture: { id: number; x: number; y: number; time: number; dragging: boolean } | null = null;
    let settling = false;
    let timer: number | undefined;

    function reset() {
      if (timer !== undefined) window.clearTimeout(timer);
      timer = undefined;
      gesture = null;
      settling = false;
      element.style.animation = "";
      element.style.transition = "";
      element.style.transform = "";
      element.style.userSelect = "";
    }

    function settle(shouldClose: boolean) {
      settling = true;
      const duration = window.matchMedia("(prefers-reduced-motion: reduce)").matches ? 0 : 180;
      element.style.transition = duration ? `transform ${duration}ms ease-out` : "none";
      element.style.transform = shouldClose ? `translate3d(${sign * 100}%, 0, 0)` : "translate3d(0, 0, 0)";
      timer = window.setTimeout(() => {
        if (shouldClose) dismiss();
        else {
          element.style.transition = "";
          element.style.transform = "";
          element.style.userSelect = "";
          settling = false;
        }
      }, duration);
    }

    function onPointerDown(event: PointerEvent) {
      if (event.pointerType !== "touch" || !event.isPrimary || settling) return;
      const bounds = element.getBoundingClientRect();
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
        element.setPointerCapture(event.pointerId);
        element.style.animation = "none";
        element.style.transition = "none";
        element.style.userSelect = "none";
      }
      element.style.transform = `translate3d(${Math.max(0, dx * sign) * sign}px, 0, 0)`;
    }

    function onPointerUp(event: PointerEvent) {
      if (!gesture || gesture.id !== event.pointerId) return;
      const { x, time, dragging } = gesture;
      gesture = null;
      if (!dragging) return;
      const distance = (event.clientX - x) * sign;
      const speed = distance / Math.max(1, event.timeStamp - time);
      settle(distance >= Math.min(120, element.clientWidth * 0.28) || (distance >= 35 && speed >= 0.6));
    }

    function onPointerCancel(event: PointerEvent) {
      if (!gesture || gesture.id !== event.pointerId) return;
      const dragging = gesture.dragging;
      gesture = null;
      if (dragging) settle(false);
    }

    function onLostPointerCapture(event: PointerEvent) {
      if (event.target === element) onPointerCancel(event);
    }

    element.addEventListener("pointerdown", onPointerDown);
    element.addEventListener("pointermove", onPointerMove);
    element.addEventListener("pointerup", onPointerUp);
    element.addEventListener("pointercancel", onPointerCancel);
    element.addEventListener("lostpointercapture", onLostPointerCapture);
    return () => {
      element.removeEventListener("pointerdown", onPointerDown);
      element.removeEventListener("pointermove", onPointerMove);
      element.removeEventListener("pointerup", onPointerUp);
      element.removeEventListener("pointercancel", onPointerCancel);
      element.removeEventListener("lostpointercapture", onLostPointerCapture);
      reset();
    };
  }, [elementRef, direction, active, dismiss]);
}
