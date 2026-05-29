import { useRunActions } from "@/api/hooks";
import type { RunResponseT } from "@/api/schemas";
import { humanBytes, relativeTime } from "@/lib/format";
import { useIsPhone } from "@/lib/useMediaQuery";
import { m } from "@/paraglide/messages";
import {
  ActionStatusBadge, ModeBadge, labelOf, runStatusLabel, runTriggerLabel, stopReasonLabel,
} from "./labels";
import { MetricGrid } from "./MetricGrid";
import { isInFlight, torrentLabel } from "./shared";

export function RunDetail({ run, onAudit }: { run: RunResponseT; onAudit: (id: number) => void }) {
  const inFlight = isInFlight(run);
  const actions = useRunActions(run.run_id, inFlight ? 2_000 : undefined);
  const actionList = actions.data?.actions ?? [];
  const isPhone = useIsPhone();

  return (
    <div className="run-detail" style={{ padding: 20, display: "flex", flexDirection: "column", gap: 18, minWidth: 0 }}>
      {/* Run header */}
      <div>
        <div style={{ display: "flex", alignItems: "center", gap: 10, marginBottom: 10 }}>
          <span style={{ fontFamily: "'Geist Mono',ui-monospace,monospace", fontWeight: 600, fontSize: 15 }}>
            #{run.run_id}
          </span>
          <ModeBadge mode={run.mode} />
          {inFlight && <span className="badge badge-warn"><span className="dot amber pulse" style={{ marginRight: 3 }} />{m.actions_status_running()}</span>}
          {run.status === "aborted" && <span className="badge badge-danger">{m.actions_status_aborted()}</span>}
          {run.status === "completed" && <span className="badge badge-success">{m.actions_status_completed()}</span>}
        </div>
        <MetricGrid
          cols={5}
          items={[
            { k: m.actions_col_status(),    v: labelOf(runStatusLabel, run.status), plain: true },
            { k: m.actions_col_triggered(), v: labelOf(runTriggerLabel, run.triggered_by), plain: true },
            { k: m.actions_col_stop(),      v: labelOf(stopReasonLabel, run.stop_reason), plain: true },
            { k: m.actions_col_freed(),     v: humanBytes(run.estimated_freed_bytes) },
            { k: m.actions_col_started(),   v: relativeTime(run.triggered_at) },
          ]}
        />
      </div>

      {/* Candidates */}
      {run.candidates && run.candidates.length > 0 && (
        <div>
          <div style={{ fontSize: 11, color: "var(--fg-3)", textTransform: "uppercase", letterSpacing: ".06em", fontWeight: 600, marginBottom: 8 }}>
            {m.actions_candidates_count({ count: run.candidates.length })}
          </div>
          {isPhone ? (
            <div style={{ display: "flex", flexDirection: "column", gap: 6 }}>
              {run.candidates.map((c) => (
                <div key={c.torrent_hash} className="action-card">
                  <div className="action-card-row1">
                    <span className="action-card-rank">#{c.rank}</span>
                    <span className="action-card-name">{torrentLabel(c.torrent_name, c.torrent_hash)}</span>
                  </div>
                  <div className="action-card-meta">
                    <span>{m.actions_th_score()} <span className="mono">{c.score.toFixed(1)}</span></span>
                    <span>·</span>
                    <span><span className="mono">{humanBytes(c.size_bytes)}</span></span>
                    <span>·</span>
                    <span>{m.actions_th_would_free()} <span className="mono">{humanBytes(c.would_free_bytes)}</span></span>
                  </div>
                </div>
              ))}
            </div>
          ) : (
          <table className="tbl" style={{ border: "1px solid var(--border)", borderRadius: 6, overflow: "hidden" }}>
            <thead>
              <tr>
                <th style={{ width: 50 }}>{m.actions_th_rank()}</th>
                <th>{m.actions_th_hash()}</th>
                <th style={{ textAlign: "right", width: 80 }}>{m.actions_th_score()}</th>
                <th style={{ textAlign: "right", width: 90 }}>{m.actions_th_size()}</th>
                <th style={{ textAlign: "right", width: 90 }}>{m.actions_th_would_free()}</th>
              </tr>
            </thead>
            <tbody>
              {run.candidates.map((c) => (
                <tr key={c.torrent_hash}>
                  <td className="mono" style={{ color: "var(--fg-3)" }}>#{c.rank}</td>
                  <td>{torrentLabel(c.torrent_name, c.torrent_hash)}</td>
                  <td className="num">{c.score.toFixed(1)}</td>
                  <td className="num">{humanBytes(c.size_bytes)}</td>
                  <td className="num">{humanBytes(c.would_free_bytes)}</td>
                </tr>
              ))}
            </tbody>
          </table>
          )}
        </div>
      )}

      {/* Actions */}
      <div>
        <div style={{ fontSize: 11, color: "var(--fg-3)", textTransform: "uppercase", letterSpacing: ".06em", fontWeight: 600, marginBottom: 8 }}>
          {m.actions_section_actions()} {inFlight && <span className="badge badge-warn" style={{ marginLeft: 6, textTransform: "none" }}>{m.actions_live_ellipsis()}</span>}
        </div>
        {actionList.length === 0 ? (
          <div style={{ color: "var(--fg-3)", fontSize: 12 }}>
            {inFlight ? m.actions_waiting_first_action() : m.actions_none_recorded()}
          </div>
        ) : isPhone ? (
          <div style={{ display: "flex", flexDirection: "column", gap: 6 }}>
            {actionList.map((a) => (
              <button
                key={a.id}
                type="button"
                className="action-card clickable"
                onClick={() => onAudit(a.id)}
              >
                <div className="action-card-row1">
                  <span className="action-card-rank">#{a.rank}</span>
                  <span className="action-card-name">{torrentLabel(a.torrent_name, a.torrent_hash)}</span>
                  <ActionStatusBadge status={a.status} />
                </div>
                <div className="action-card-meta">
                  <span><span className="mono">{humanBytes(a.freed_bytes)}</span></span>
                  <span>·</span>
                  <span>{relativeTime(a.started_at)}</span>
                </div>
              </button>
            ))}
          </div>
        ) : (
          <table className="tbl" style={{ border: "1px solid var(--border)", borderRadius: 6, overflow: "hidden" }}>
            <thead>
              <tr>
                <th style={{ width: 50 }}>{m.actions_th_rank()}</th>
                <th>{m.actions_th_hash()}</th>
                <th>{m.actions_th_status()}</th>
                <th style={{ textAlign: "right", width: 90 }}>{m.actions_th_freed()}</th>
                <th style={{ width: 90 }}>{m.actions_th_started()}</th>
              </tr>
            </thead>
            <tbody>
              {actionList.map((a) => (
                <tr key={a.id} className="clickable" onClick={() => onAudit(a.id)}>
                  <td className="mono" style={{ color: "var(--fg-3)" }}>#{a.rank}</td>
                  <td>{torrentLabel(a.torrent_name, a.torrent_hash)}</td>
                  <td><ActionStatusBadge status={a.status} /></td>
                  <td className="num">{humanBytes(a.freed_bytes)}</td>
                  <td style={{ fontSize: 11.5, color: "var(--fg-3)" }}>{relativeTime(a.started_at)}</td>
                </tr>
              ))}
            </tbody>
          </table>
        )}
      </div>
    </div>
  );
}
