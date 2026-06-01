package server

import (
	"database/sql"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/Triagearr/Triagearr/internal/store"
	"github.com/Triagearr/Triagearr/internal/triagearr"
)

type torrentListItem struct {
	Hash       string     `json:"hash"`
	Name       string     `json:"name"`
	Category   string     `json:"category"`
	Size       int64      `json:"size"`
	AddedOn    time.Time  `json:"added_on"`
	LastSeen   time.Time  `json:"last_seen"`
	Private    bool       `json:"private"`
	Ratio      *float64   `json:"ratio,omitempty"`
	Seeders    *int       `json:"seeders,omitempty"`
	Leechers   *int       `json:"leechers,omitempty"`
	State      *string    `json:"state,omitempty"`
	SnapshotAt *time.Time `json:"snapshot_at,omitempty"`

	Score           *float64 `json:"score,omitempty"`
	Excluded        *bool    `json:"excluded,omitempty"`
	AnyTrackerAlive *bool    `json:"any_tracker_alive,omitempty"`
	CandidateBoost  bool     `json:"candidate_boost"`
}

type torrentListResponse struct {
	Torrents []torrentListItem `json:"torrents"`
	Total    int               `json:"total"`
	Limit    int               `json:"limit"`
	Offset   int               `json:"offset"`
}

type torrentCategoriesResponse struct {
	Categories []string `json:"categories"`
}

func (s *Server) handleListTorrents(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	opts := store.ListTorrentsOpts{
		Sort:         q.Get("sort"),
		Order:        q.Get("order"),
		Query:        q.Get("q"),
		Category:     q.Get("category"),
		PrivateOnly:  boolParam(q, "private"),
		ExcludedOnly: boolParam(q, "excluded"),
		Limit:        intParam(q, "limit", 50, 1, 500),
		Offset:       intParam(q, "offset", 0, 0, 1_000_000),
	}
	rows, err := s.opts.Store.ListTorrentsFiltered(r.Context(), opts)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	total, err := s.opts.Store.CountTorrentsFiltered(r.Context(), opts)
	if err != nil {
		writeInternal(w, err)
		return
	}
	items := make([]torrentListItem, len(rows))
	for i, row := range rows {
		items[i] = torrentListItem{
			Hash:            row.Hash,
			Name:            row.Name,
			Category:        row.Category,
			Size:            row.Size,
			AddedOn:         row.AddedOn,
			LastSeen:        row.LastSeen,
			Private:         row.Private,
			Ratio:           row.Ratio,
			Seeders:         row.Seeders,
			Leechers:        row.Leechers,
			State:           row.State,
			SnapshotAt:      row.SnapshotAt,
			Score:           row.Score,
			Excluded:        row.Excluded,
			AnyTrackerAlive: row.AnyTrackerAlive,
			CandidateBoost:  row.CandidateBoost,
		}
	}
	writeJSON(w, http.StatusOK, torrentListResponse{
		Torrents: items, Total: total, Limit: opts.Limit, Offset: opts.Offset,
	})
}

func (s *Server) handleTorrentCategories(w http.ResponseWriter, r *http.Request) {
	cats, err := s.opts.Store.DistinctCategories(r.Context())
	if err != nil {
		writeInternal(w, err)
		return
	}
	if cats == nil {
		cats = []string{}
	}
	writeJSON(w, http.StatusOK, torrentCategoriesResponse{Categories: cats})
}

type torrentDetailResponse struct {
	Hash             string     `json:"hash"`
	Name             string     `json:"name"`
	Category         string     `json:"category"`
	SavePath         string     `json:"save_path"`
	Size             int64      `json:"size"`
	AddedOn          time.Time  `json:"added_on"`
	CompletionOn     *time.Time `json:"completion_on,omitempty"`
	Private          bool       `json:"private"`
	Tags             string     `json:"tags"`
	LastSeen         time.Time  `json:"last_seen"`
	Protected        bool       `json:"protected"`
	ProtectedAt      *time.Time `json:"protected_at,omitempty"`
	CandidateBoost   bool       `json:"candidate_boost"`
	CandidateBoostAt *time.Time `json:"candidate_boost_at,omitempty"`

	Latest *torrentLatest `json:"latest,omitempty"`

	Trackers []trackerView `json:"trackers"`
	Links    []linkView    `json:"links"`
	Score    *scoreView    `json:"score,omitempty"`
}

type torrentLatest struct {
	Ratio      *float64   `json:"ratio,omitempty"`
	Uploaded   *int64     `json:"uploaded,omitempty"`
	Seeders    *int       `json:"seeders,omitempty"`
	Leechers   *int       `json:"leechers,omitempty"`
	State      *string    `json:"state,omitempty"`
	SnapshotAt *time.Time `json:"snapshot_at,omitempty"`
}

type trackerView struct {
	Host        string    `json:"host"`
	URL         string    `json:"url"`
	Status      string    `json:"status"`
	Message     string    `json:"message"`
	LastChecked time.Time `json:"last_checked"`
	// FirstSeenDead is when Triagearr anchored this tracker's death (activity
	// proxy, not last_checked). Nil while the tracker is alive. Surfaced so the
	// UI can explain a still-zero tracker_dead_bonus instead of just showing 0.
	FirstSeenDead *time.Time `json:"first_seen_dead,omitempty"`
}

type linkView struct {
	ArrType      string `json:"arr_type"`
	ArrURL       string `json:"arr_url"`
	TitleSlug    string `json:"title_slug"`
	FileID       int64  `json:"file_id"`
	Size         int64  `json:"size"`
	LivePath     string `json:"live_path"`
	DroppedPath  string `json:"dropped_path"`
	ImportedPath string `json:"imported_path"`
}

type scoreView struct {
	Score            float64         `json:"score"`
	Private          bool            `json:"private"`
	AnyTrackerAlive  bool            `json:"any_tracker_alive"`
	Excluded         bool            `json:"excluded"`
	ExclusionReasons string          `json:"exclusion_reasons,omitempty"`
	Factors          json.RawMessage `json:"factors,omitempty"`
	ComputedAt       time.Time       `json:"computed_at"`
	// TrackerDeadEligibleAt is set only when every tracker is dead but the
	// grace window has not yet elapsed: it is the instant the tracker_dead
	// bonus will start contributing (max(first_seen_dead) + tracker_dead_grace).
	// Nil when the bonus is already active or not applicable, letting the UI
	// render an "active in N" countdown beside the 0.00 contribution.
	TrackerDeadEligibleAt *time.Time `json:"tracker_dead_eligible_at,omitempty"`
}

func (s *Server) handleGetTorrent(w http.ResponseWriter, r *http.Request) {
	hash := triagearr.Hash(strings.ToLower(r.PathValue("hash")))
	row, err := s.opts.Store.GetTorrent(r.Context(), hash)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			writeError(w, http.StatusNotFound, "torrent not found")
			return
		}
		writeInternal(w, err)
		return
	}

	out := torrentDetailResponse{
		Hash: row.Hash, Name: row.Name, Category: row.Category, SavePath: row.SavePath,
		Size: row.Size, AddedOn: row.AddedOn, CompletionOn: row.CompletionOn,
		Private: row.Private, Tags: row.Tags, LastSeen: row.LastSeen,
		Protected: row.Protected, ProtectedAt: row.ProtectedAt,
		CandidateBoost: row.CandidateBoost, CandidateBoostAt: row.CandidateBoostAt,
	}
	if row.SnapshotAt != nil {
		out.Latest = &torrentLatest{
			Ratio: row.Ratio, Uploaded: row.Uploaded, Seeders: row.Seeders,
			Leechers: row.Leechers, State: row.State, SnapshotAt: row.SnapshotAt,
		}
	}

	var deadEligibleAt *time.Time
	if trks, err := s.opts.Store.ListTrackers(r.Context(), hash); err == nil {
		out.Trackers = make([]trackerView, len(trks))
		for i, t := range trks {
			out.Trackers[i] = trackerView{
				Host: t.Host, URL: t.URL, Status: t.Status.String(),
				Message: t.Msg, LastChecked: t.LastChecked, FirstSeenDead: t.FirstSeenDead,
			}
		}
		var grace time.Duration
		if cfg := s.engine().Config; cfg != nil {
			grace = cfg.Scoring.TrackerDeadGrace
		}
		deadEligibleAt = trackerDeadEligibleAt(trks, grace, time.Now().UTC())
	}

	if s.opts.Linker != nil {
		if links, err := s.opts.Linker.Links(r.Context(), hash); err == nil {
			out.Links = make([]linkView, len(links))
			// Resolve each arr kind's base URL once: a multi-file torrent can
			// carry many links of the same ArrType, and arrBaseURL hits the DB.
			arrURLs := make(map[triagearr.ArrType]string)
			for i, l := range links {
				baseURL, ok := arrURLs[l.ArrType]
				if !ok {
					baseURL = s.arrBaseURL(r.Context(), l.ArrType)
					arrURLs[l.ArrType] = baseURL
				}
				out.Links[i] = linkView{
					ArrType:      string(l.ArrType),
					ArrURL:       baseURL,
					TitleSlug:    l.TitleSlug,
					FileID:       l.FileID,
					Size:         l.Size,
					LivePath:     l.LivePath,
					DroppedPath:  l.DroppedPath,
					ImportedPath: l.ImportedPath,
				}
			}
		}
	}

	if score, err := s.opts.Store.GetScore(r.Context(), hash); err == nil {
		out.Score = &scoreView{
			Score: score.Score, Private: score.Private,
			AnyTrackerAlive: score.AnyTrackerAlive, Excluded: score.Excluded,
			ExclusionReasons:      score.ExclusionReasons,
			Factors:               json.RawMessage(score.FactorsJSON),
			ComputedAt:            score.ComputedAt,
			TrackerDeadEligibleAt: deadEligibleAt,
		}
	}

	if out.Trackers == nil {
		out.Trackers = []trackerView{}
	}
	if out.Links == nil {
		out.Links = []linkView{}
	}
	writeJSON(w, http.StatusOK, out)
}

// trackerDeadEligibleAt mirrors the scorer's allTrackersDeadSustained gate and
// returns when the tracker_dead bonus will start contributing — but only while
// it is still pending. It returns nil when the bonus is already active, when a
// tracker is alive, when first_seen_dead is unknown, or when there are no
// trackers. The UI uses the non-nil case to show an "active in N" countdown.
func trackerDeadEligibleAt(trks []store.TrackerRow, grace time.Duration, now time.Time) *time.Time {
	if len(trks) == 0 {
		return nil
	}
	var maxFSD time.Time
	for _, t := range trks {
		if t.Status != triagearr.TrackerNotWorking || t.FirstSeenDead == nil {
			return nil
		}
		if t.FirstSeenDead.After(maxFSD) {
			maxFSD = *t.FirstSeenDead
		}
	}
	eligibleAt := maxFSD.Add(grace)
	if eligibleAt.After(now) {
		return &eligibleAt
	}
	return nil
}

type setProtectedRequest struct {
	Protected bool `json:"protected"`
}

// handleSetTorrentProtected toggles the user-driven protection flag. Idempotent
// (PUT). Triggers an immediate single-hash rescore so the Decider's view of the
// torrent (excluded yes/no) updates without waiting for the next scoring pass.
func (s *Server) handleSetTorrentProtected(w http.ResponseWriter, r *http.Request) {
	hash := triagearr.Hash(strings.ToLower(r.PathValue("hash")))
	var body setProtectedRequest
	if !decodeJSONBody(w, r, &body) {
		return
	}

	if err := s.opts.Store.SetTorrentProtected(r.Context(), hash, body.Protected); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			writeError(w, http.StatusNotFound, "torrent not found")
			return
		}
		writeInternal(w, err)
		return
	}

	if sc := s.engine().Scorer; sc != nil {
		if _, err := sc.ScoreOne(r.Context(), hash); err != nil {
			slog.Warn("rescore after protect toggle failed", "hash", hash, "err", err)
		}
	}

	w.WriteHeader(http.StatusNoContent)
}

type setCandidateBoostRequest struct {
	CandidateBoost bool `json:"candidate_boost"`
}

// handleSetTorrentCandidateBoost toggles the user-driven "prioritize deletion"
// flag (the inverse of protect; ADR-0030). Idempotent (PUT). Boosting clears any
// protect flag in the store. Triggers an immediate single-hash rescore so the
// boosted score lands without waiting for the next scoring pass.
func (s *Server) handleSetTorrentCandidateBoost(w http.ResponseWriter, r *http.Request) {
	hash := triagearr.Hash(strings.ToLower(r.PathValue("hash")))
	var body setCandidateBoostRequest
	if !decodeJSONBody(w, r, &body) {
		return
	}

	if err := s.opts.Store.SetTorrentCandidateBoost(r.Context(), hash, body.CandidateBoost); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			writeError(w, http.StatusNotFound, "torrent not found")
			return
		}
		writeInternal(w, err)
		return
	}

	if sc := s.engine().Scorer; sc != nil {
		if _, err := sc.ScoreOne(r.Context(), hash); err != nil {
			slog.Warn("rescore after candidate_boost toggle failed", "hash", hash, "err", err)
		}
	}

	w.WriteHeader(http.StatusNoContent)
}

type snapshotPoint struct {
	Timestamp time.Time `json:"ts"`
	Ratio     float64   `json:"ratio"`
	Uploaded  int64     `json:"uploaded"`
	Seeders   int       `json:"seeders"`
	Leechers  int       `json:"leechers"`
	State     string    `json:"state"`
}

func (s *Server) handleTorrentSnapshots(w http.ResponseWriter, r *http.Request) {
	hash := triagearr.Hash(strings.ToLower(r.PathValue("hash")))
	since := sinceParam(r, 30*24*time.Hour)
	limit := intParam(r.URL.Query(), "limit", 2000, 1, 10000)

	points, err := s.opts.Store.ListSnapshotsRaw(r.Context(), hash, since, limit)
	if err != nil {
		writeInternal(w, err)
		return
	}
	out := make([]snapshotPoint, len(points))
	for i, p := range points {
		out[i] = snapshotPoint{
			Timestamp: p.Timestamp, Ratio: p.Ratio, Uploaded: p.Uploaded,
			Seeders: p.Seeders, Leechers: p.Leechers, State: p.State,
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"snapshots": out})
}
