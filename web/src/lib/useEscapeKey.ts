import { useEffect, useRef } from "react";

// useEscapeKey wires a single document-level keydown listener while `active`
// is true. The callback is held in a ref so callers can pass a fresh inline
// closure each render without thrashing addEventListener/removeEventListener.
export function useEscapeKey(active: boolean, onEscape: () => void) {
  const cbRef = useRef(onEscape);
  cbRef.current = onEscape;
  useEffect(() => {
    if (!active) return;
    const onKey = (e: KeyboardEvent) => {
      if (e.key === "Escape") cbRef.current();
    };
    document.addEventListener("keydown", onKey);
    return () => document.removeEventListener("keydown", onKey);
  }, [active]);
}
