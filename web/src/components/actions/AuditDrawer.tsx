import { X } from "lucide-react";
import { useEffect } from "react";
import { useAction } from "@/api/hooks";
import { humanBytes, relativeTime } from "@/lib/format";
import { m } from "@/paraglide/messages";
import { ActionStatusBadge, OutcomeBadge, auditStepLabel, labelOf } from "./labels";
import { MetricGrid } from "./MetricGrid";

export function AuditDrawer({ id, onClose }: { id: number | undefined; onClose: () => void }) {
  const detail = useAction(id);

  useEffect(() => {
    if (!id) return;
    const fn = (e: KeyboardEvent) => { if (e.key === "Escape") onClose(); };
    document.addEventListener("keydown", fn);
    return () => document.removeEventListener("keydown", fn);
  }, [id, onClose]);

  if (!id) return null;
  return (
    <>
      <div className="scrim" onClick={onClose} />
      <aside className="ds-drawer" style={{ width: 500 }} role="dialog" aria-modal="true">
        <div className="drawer-head">
          <div style={{ flex: 1 }}>
            <div style={{ fontSize: 11, color: "var(--fg-3)", textTransform: "uppercase", letterSpacing: ".05em" }}>
              {m.actions_audit_action({ id })}
            </div>
          </div>
          <button className="btn btn-ghost btn-sm" onClick={onClose}><X size={12} /> {m.common_close()}</button>
        </div>
        <div className="drawer-body">
          {detail.isLoading && <div style={{ color: "var(--fg-3)", fontSize: 12 }}>{m.common_loading()}</div>}
          {detail.data && (() => {
            const { action, audit } = detail.data;
            return (
              <>
                <div className="drawer-section" style={{ marginTop: 0 }}>
                  <MetricGrid
                    cols={3}
                    items={[
                      { k: m.actions_col_status(), v: <ActionStatusBadge status={action.status} />, plain: true },
                      { k: m.actions_col_freed(), v: humanBytes(action.freed_bytes) },
                      { k: m.actions_col_started(), v: relativeTime(action.started_at) },
                    ]}
                  />
                  {action.torrent_name && (
                    <div style={{ marginTop: 8, fontSize: 12.5, fontWeight: 600 }}>
                      {action.torrent_name}
                    </div>
                  )}
                  <div style={{ marginTop: 4, fontFamily: "'Geist Mono',ui-monospace,monospace", fontSize: 10.5, color: "var(--fg-3)", wordBreak: "break-all" }}>
                    {action.torrent_hash}
                  </div>
                </div>

                <div className="drawer-section">
                  <div className="drawer-section-title">{m.actions_audit_trail()}</div>
                  <div style={{ background: "var(--card-2)", border: "1px solid var(--border)", borderRadius: 6, padding: "4px 12px" }}>
                    {audit.length === 0
                      ? <div style={{ color: "var(--fg-3)", fontSize: 12, padding: "8px 0" }}>{m.actions_no_audit_rows()}</div>
                      : audit.map((e) => (
                        <div key={e.id} className="audit-step">
                          <span className="audit-step-time">{relativeTime(e.ts)}</span>
                          <span className="audit-step-name">{labelOf(auditStepLabel, e.step)}</span>
                          <OutcomeBadge outcome={e.outcome} />
                          {e.detail && <span className="audit-step-detail">{e.detail}</span>}
                        </div>
                      ))
                    }
                  </div>
                </div>
              </>
            );
          })()}
        </div>
      </aside>
    </>
  );
}
