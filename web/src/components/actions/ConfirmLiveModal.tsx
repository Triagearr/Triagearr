import { AlertTriangle, Zap } from "lucide-react";
import { useEffect, useRef, useState } from "react";
import { usePreviewRun } from "@/api/hooks";
import { humanBytes } from "@/lib/format";
import { m } from "@/paraglide/messages";
import { torrentLabel } from "./shared";

export function ConfirmLiveModal({ open, onClose, onConfirm }: {
  open: boolean; onClose: () => void; onConfirm: () => void;
}) {
  const [typed, setTyped] = useState("");
  const [armed, setArmed] = useState(false);
  const [count, setCount] = useState(5);
  const expected = "LIVE";
  const cardRef = useRef<HTMLDivElement>(null);
  const restoreFocus = useRef<HTMLElement | null>(null);

  // Fetch the plan a live run would execute right now, only while open.
  const preview = usePreviewRun(open);
  const candidates = preview.data?.candidates ?? [];
  const hasCandidates = candidates.length > 0;

  useEffect(() => {
    if (!open) { setTyped(""); setArmed(false); setCount(5); return; }
    const tick = setInterval(() => {
      setCount((c) => {
        if (c <= 1) { clearInterval(tick); setArmed(true); return 0; }
        return c - 1;
      });
    }, 1000);
    return () => clearInterval(tick);
  }, [open]);

  // Escape to close + a minimal focus trap: remember the previously focused
  // element, keep Tab within the card, restore focus on close.
  useEffect(() => {
    if (!open) return;
    restoreFocus.current = document.activeElement as HTMLElement | null;
    const fn = (e: KeyboardEvent) => {
      if (e.key === "Escape") { onClose(); return; }
      if (e.key !== "Tab" || !cardRef.current) return;
      const focusable = cardRef.current.querySelectorAll<HTMLElement>(
        'button:not([disabled]), input, [href], [tabindex]:not([tabindex="-1"])',
      );
      if (focusable.length === 0) return;
      const first = focusable[0];
      const last = focusable[focusable.length - 1];
      if (e.shiftKey && document.activeElement === first) { e.preventDefault(); last.focus(); }
      else if (!e.shiftKey && document.activeElement === last) { e.preventDefault(); first.focus(); }
    };
    document.addEventListener("keydown", fn);
    return () => {
      document.removeEventListener("keydown", fn);
      restoreFocus.current?.focus?.();
    };
  }, [open, onClose]);

  if (!open) return null;
  const matches = typed === expected;
  const canExecute = matches && armed && hasCandidates;

  return (
    <div className="modal-scrim" onClick={onClose}>
      <div
        className="modal-card"
        ref={cardRef}
        role="dialog"
        aria-modal="true"
        aria-label={m.actions_trigger_live_run()}
        onClick={(e) => e.stopPropagation()}
      >
        <div className="modal-head" style={{ background: "var(--red-bg)", borderBottomColor: "color-mix(in oklch, var(--red) 30%, transparent)" }}>
          <AlertTriangle size={16} style={{ color: "var(--red-2)", flex: "none" }} />
          <h3 style={{ color: "var(--red-2)", fontSize: 14, fontWeight: 600, margin: 0 }}>
            {m.actions_trigger_live_run()}
          </h3>
          <span className="badge badge-solid-danger" style={{ marginLeft: "auto" }}>{m.actions_destructive()}</span>
        </div>
        <div className="modal-body">
          <p style={{ margin: 0, fontSize: 13 }}>
            {m.actions_warning_prefix()} <strong style={{ color: "var(--red-2)" }}>{m.actions_warning_permanently_delete()}</strong> {m.actions_warning_suffix()}
          </p>

          {/* Plan preview — what a live run would delete right now */}
          <div style={{ border: "1px solid var(--border)", borderRadius: 6, overflow: "hidden" }}>
            {preview.isLoading ? (
              <div style={{ padding: "10px 12px", fontSize: 12, color: "var(--fg-3)" }}>
                {m.actions_preview_loading()}
              </div>
            ) : !hasCandidates ? (
              <div style={{ padding: "10px 12px", fontSize: 12, color: "var(--fg-3)" }}>
                {m.actions_preview_empty()}
              </div>
            ) : (
              <>
                <div style={{ padding: "8px 12px", fontSize: 12.5, fontWeight: 600, borderBottom: "1px solid var(--border)" }}>
                  {m.actions_preview_headline({
                    count: candidates.length,
                    freed: humanBytes(preview.data?.estimated_freed_bytes ?? 0),
                  })}
                </div>
                <div style={{ maxHeight: 160, overflow: "auto" }}>
                  <table className="tbl">
                    <tbody>
                      {candidates.map((c) => (
                        <tr key={c.torrent_hash}>
                          <td className="mono" style={{ color: "var(--fg-3)", width: 40 }}>#{c.rank}</td>
                          <td>{torrentLabel(c.torrent_name, c.torrent_hash)}</td>
                          <td className="num" style={{ width: 70 }}>{c.score.toFixed(1)}</td>
                          <td className="num" style={{ width: 80 }}>{humanBytes(c.would_free_bytes)}</td>
                        </tr>
                      ))}
                    </tbody>
                  </table>
                </div>
              </>
            )}
          </div>

          <div className="field">
            <label className="field-label">
              {m.actions_type_label_before()} <span style={{ fontFamily: "'Geist Mono',ui-monospace,monospace", color: "var(--red-2)" }}>{expected}</span> {m.actions_type_label_after()}
            </label>
            <input
              className="ds-input mono-val"
              value={typed}
              onChange={(e) => setTyped(e.target.value)}
              placeholder={expected}
              autoFocus
            />
          </div>
        </div>
        <div className="modal-foot">
          <button className="btn" onClick={onClose}>{m.common_cancel()}</button>
          <button
            className="btn btn-destructive"
            disabled={!canExecute}
            onClick={() => { onConfirm(); onClose(); }}
          >
            <Zap size={12} />
            {armed ? m.actions_execute_live_run() : m.actions_hold_seconds({ count })}
          </button>
        </div>
      </div>
    </div>
  );
}
