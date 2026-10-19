import { useLayoutEffect, useRef } from "react";

/** Replays on navigation only; data refreshes and typing keep their current layout. */
export function useEnterMotion(key: string) {
  const ref = useRef<HTMLDivElement>(null);
  useLayoutEffect(() => {
    const element = ref.current;
    if (!element?.animate || matchMedia("(prefers-reduced-motion: reduce)").matches) return;
    const animation = element.animate(
      [{ opacity: 0.35, transform: "translateY(6px)" }, { opacity: 1, transform: "translateY(0)" }],
      { duration: 230, easing: "cubic-bezier(.22,1,.36,1)" },
    );
    return () => animation.cancel();
  }, [key]);
  return ref;
}
