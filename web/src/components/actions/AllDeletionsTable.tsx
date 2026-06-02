import type { ActionViewT } from "@/api/schemas";
import { humanBytes, relativeTime } from "@/lib/format";
import { useIsPhone } from "@/lib/useMediaQuery";
import { m } from "@/paraglide/messages";
import { ActionStatusBadge } from "./labels";
import { torrentLabel } from "./shared";

// AllDeletionsTable lists every recorded action across all runs. It is the
// landing view shown when no run is selected (only live runs record actions).
export function AllDeletionsTable({ actions, onSelectRun, onAudit }: {
  actions: ActionViewT[]; onSelectRun: (id: number) => void; onAudit: (id: number) => void;
}) {
  const isPhone = useIsPhone();
  if (isPhone) {
    if (actions.length === 0) {
      return (
        <div style={{ textAlign: "center", color: "var(--fg-3)", padding: "16px 14px", fontSize: 12 }}>
          {m.actions_no_live_deletions()}
        </div>
      );
    }
    return (
      <div style={{ display: "flex", flexDirection: "column", gap: 6, padding: 12 }}>
        {actions.map((a) => (
          <div
            key={a.id}
            role="button"
            tabIndex={0}
            className="action-card clickable"
            onClick={() => onAudit(a.id)}
            onKeyDown={(e) => { if (e.key === "Enter") onAudit(a.id); }}
          >
            <div className="action-card-row1">
              <button
                className="btn btn-ghost btn-sm"
                style={{ fontFamily: "'Geist Mono',ui-monospace,monospace", padding: "0 4px" }}
                onClick={(e) => { e.stopPropagation(); onSelectRun(a.run_id); }}
              >
                #{a.run_id}
              </button>
              <span className="action-card-name">{torrentLabel(a.torrent_name, a.torrent_hash)}</span>
              <ActionStatusBadge status={a.status} />
            </div>
            <div className="action-card-meta">
              <span><span className="mono">{humanBytes(a.freed_bytes)}</span></span>
              <span>·</span>
              <span>{relativeTime(a.started_at)}</span>
            </div>
          </div>
        ))}
      </div>
    );
  }
  return (
    <table className="tbl">
      <thead>
        <tr>
          <th style={{ width: 60 }}>{m.actions_th_run()}</th>
          <th>{m.actions_th_hash()}</th>
          <th>{m.actions_th_status()}</th>
          <th style={{ textAlign: "right", width: 90 }}>{m.actions_th_freed()}</th>
          <th style={{ width: 90 }}>{m.actions_th_when()}</th>
        </tr>
      </thead>
      <tbody>
        {actions.map((a) => (
          <tr key={a.id} className="clickable" onClick={() => onAudit(a.id)}>
            <td>
              <button
                className="btn btn-ghost btn-sm"
                style={{ fontFamily: "'Geist Mono',ui-monospace,monospace", padding: "0 4px" }}
                onClick={(e) => { e.stopPropagation(); onSelectRun(a.run_id); }}
              >
                #{a.run_id}
              </button>
            </td>
            <td className="name-cell">
              <span className="name-text" title={a.torrent_name ?? a.torrent_hash}>
                {torrentLabel(a.torrent_name, a.torrent_hash)}
              </span>
            </td>
            <td><ActionStatusBadge status={a.status} /></td>
            <td className="num">{humanBytes(a.freed_bytes)}</td>
            <td style={{ fontSize: 11.5, color: "var(--fg-3)" }}>{relativeTime(a.started_at)}</td>
          </tr>
        ))}
        {actions.length === 0 && (
          <tr>
            <td colSpan={5} style={{ textAlign: "center", color: "var(--fg-3)", padding: "10px 14px", fontSize: 12 }}>
              {m.actions_no_live_deletions()}
            </td>
          </tr>
        )}
      </tbody>
    </table>
  );
}
