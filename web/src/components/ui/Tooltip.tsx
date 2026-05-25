import { useState, useRef, useEffect, ReactNode } from "react";
import { createPortal } from "react-dom";

type Props = {
  content: ReactNode;
  children: ReactNode;
};

/**
 * Lightweight hover tooltip — no external dependency.
 * Renders the panel via a portal so it escapes overflow:hidden containers
 * (pre blocks, cards, etc.) and always appears above the hovered element.
 */
export function Tooltip({ content, children }: Props) {
  const [visible, setVisible] = useState(false);
  const [pos, setPos] = useState({ top: 0, left: 0 });
  const anchorRef = useRef<HTMLSpanElement>(null);

  function show() {
    if (!anchorRef.current) return;
    const r = anchorRef.current.getBoundingClientRect();
    setPos({
      top: r.top + window.scrollY - 6,
      left: r.left + window.scrollX + r.width / 2,
    });
    setVisible(true);
  }

  function hide() { setVisible(false); }

  // Hide on scroll so the tooltip doesn't drift.
  useEffect(() => {
    if (!visible) return;
    window.addEventListener("scroll", hide, { passive: true });
    return () => window.removeEventListener("scroll", hide);
  }, [visible]);

  return (
    <>
      <span ref={anchorRef} onMouseEnter={show} onMouseLeave={hide}>
        {children}
      </span>

      {visible && createPortal(
        <div
          style={{
            position: "absolute",
            top: pos.top,
            left: pos.left,
            transform: "translate(-50%, -100%)",
            zIndex: 9999,
            pointerEvents: "none",
          }}
        >
          <div
            style={{
              background: "var(--fg)",
              color: "var(--bg)",
              fontSize: "0.7rem",
              fontFamily: "var(--font-mono, monospace)",
              borderRadius: "5px",
              padding: "4px 8px",
              whiteSpace: "pre",
              boxShadow: "0 2px 8px oklch(0 0 0 / 0.25)",
              maxWidth: "320px",
              overflowX: "auto",
            }}
          >
            {content}
          </div>
          {/* Arrow */}
          <div
            style={{
              width: 0,
              height: 0,
              margin: "0 auto",
              borderLeft: "5px solid transparent",
              borderRight: "5px solid transparent",
              borderTop: "5px solid var(--fg)",
            }}
          />
        </div>,
        document.body,
      )}
    </>
  );
}
