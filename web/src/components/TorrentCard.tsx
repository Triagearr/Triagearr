import { memo } from "react";
import { EyeOff, Flame, Lock, Skull, Unlock } from "lucide-react";
import { ScoreCell } from "@/components/ScoreCell";
import { humanBytes, relativeTime } from "@/lib/format";
import { m } from "@/paraglide/messages";
import type { TorrentListItemT } from "@/api/schemas";

type Props = {
  torrent: TorrentListItemT;
  selected: boolean;
  onClick: () => void;
};

function TorrentCardImpl({ torrent: t, selected, onClick }: Props) {
  return (
    <button
      type="button"
      onClick={onClick}
      className={`torrent-card${selected ? " selected" : ""}`}
    >
      <div className="torrent-card-row1">
        <div className="torrent-card-name">{t.name}</div>
        <ScoreCell score={t.score} />
      </div>
      <div className="torrent-card-meta">
        {t.private
          ? <span className="badge"><Lock size={9} /> <span className="badge-label">{m.torrents_badge_private()}</span></span>
          : <span className="badge"><Unlock size={9} /> <span className="badge-label">{m.torrents_badge_public()}</span></span>
        }
        {t.excluded && (
          <span className="badge badge-warn"><EyeOff size={9} /> <span className="badge-label">{m.torrents_badge_excluded()}</span></span>
        )}
        {t.candidate_boost && (
          <span className="badge badge-danger"><Flame size={9} /> <span className="badge-label">{m.torrents_badge_prioritized()}</span></span>
        )}
        {t.any_tracker_alive === false && (
          <span className="badge badge-danger"><Skull size={9} /> <span className="badge-label">{m.torrents_badge_tracker_dead()}</span></span>
        )}
        <span className="torrent-card-sep">·</span>
        <span className="mono">{humanBytes(t.size)}</span>
        {t.ratio != null && (
          <>
            <span className="torrent-card-sep">·</span>
            <span className="mono">r{t.ratio.toFixed(2)}</span>
          </>
        )}
      </div>
      <div className="torrent-card-foot">
        <span>{t.category || "—"}</span>
        {t.state && <><span className="torrent-card-sep">·</span><span>{t.state}</span></>}
        <span className="torrent-card-sep">·</span>
        <span>{relativeTime(t.last_seen)}</span>
      </div>
    </button>
  );
}

export const TorrentCard = memo(TorrentCardImpl);

