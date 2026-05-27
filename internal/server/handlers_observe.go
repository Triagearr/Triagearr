package server

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
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
	Hash         string     `json:"hash"`
	Name         string     `json:"name"`
	Category     string     `json:"category"`
	SavePath     string     `json:"save_path"`
	Size         int64      `json:"size"`
	AddedOn      time.Time  `json:"added_on"`
	CompletionOn *time.Time `json:"completion_on,omitempty"`
	Private      bool       `json:"private"`
	Tags         string     `json:"tags"`
	LastSeen     time.Time  `json:"last_seen"`
	Protected    bool       `json:"protected"`
	ProtectedAt  *time.Time `json:"protected_at,omitempty"`

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
	}
	if row.SnapshotAt != nil {
		out.Latest = &torrentLatest{
			Ratio: row.Ratio, Uploaded: row.Uploaded, Seeders: row.Seeders,
			Leechers: row.Leechers, State: row.State, SnapshotAt: row.SnapshotAt,
		}
	}

	if trks, err := s.opts.Store.ListTrackers(r.Context(), hash); err == nil {
		out.Trackers = make([]trackerView, len(trks))
		for i, t := range trks {
			out.Trackers[i] = trackerView{
				Host: t.Host, URL: t.URL, Status: t.Status.String(),
				Message: t.Msg, LastChecked: t.LastChecked,
			}
		}
	}

	if s.opts.Linker != nil {
		if links, err := s.opts.Linker.Links(r.Context(), hash); err == nil {
			out.Links = make([]linkView, len(links))
			for i, l := range links {
				out.Links[i] = linkView{
					ArrType:      string(l.ArrType),
					ArrURL:       s.arrBaseURL(r.Context(), l.ArrType),
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
			ExclusionReasons: score.ExclusionReasons,
			Factors:          json.RawMessage(score.FactorsJSON),
			ComputedAt:       score.ComputedAt,
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

	if s.opts.Scorer != nil {
		if _, err := s.opts.Scorer.ScoreOne(r.Context(), hash); err != nil {
			slog.Warn("rescore after protect toggle failed", "hash", hash, "err", err)
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

type scoreListItem struct {
	Hash             string          `json:"hash"`
	Name             string          `json:"name"`
	Score            float64         `json:"score"`
	Private          bool            `json:"private"`
	AnyTrackerAlive  bool            `json:"any_tracker_alive"`
	Excluded         bool            `json:"excluded"`
	ExclusionReasons string          `json:"exclusion_reasons,omitempty"`
	Factors          json.RawMessage `json:"factors,omitempty"`
	ComputedAt       time.Time       `json:"computed_at"`
}

func (s *Server) handleListScores(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	rows, err := s.opts.Store.ListScores(r.Context(), store.ListScoresOpts{
		Limit:           intParam(q, "limit", 50, 1, 500),
		IncludeExcluded: boolParam(q, "include_excluded"),
	})
	if err != nil {
		writeInternal(w, err)
		return
	}
	out := make([]scoreListItem, len(rows))
	for i, row := range rows {
		out[i] = scoreItemFromRow(row)
	}
	writeJSON(w, http.StatusOK, map[string]any{"scores": out})
}

func scoreItemFromRow(row store.ScoreRow) scoreListItem {
	return scoreListItem{
		Hash: row.Hash, Name: row.Name, Score: row.Score, Private: row.Private,
		AnyTrackerAlive: row.AnyTrackerAlive, Excluded: row.Excluded,
		ExclusionReasons: row.ExclusionReasons,
		Factors:          json.RawMessage(row.FactorsJSON),
		ComputedAt:       row.ComputedAt,
	}
}

type volumeView struct {
	Name                 string     `json:"name"`
	Path                 string     `json:"path"`
	TargetFreePercent    float64    `json:"target_free_percent,omitempty"`
	ThresholdFreePercent float64    `json:"threshold_free_percent,omitempty"`
	TotalBytes           uint64     `json:"total_bytes,omitempty"`
	UsedBytes            uint64     `json:"used_bytes,omitempty"`
	FreeBytes            uint64     `json:"free_bytes,omitempty"`
	FreePercent          float64    `json:"free_percent,omitempty"`
	MeasuredAt           *time.Time `json:"measured_at,omitempty"`
}

func (s *Server) handleVolume(w http.ResponseWriter, r *http.Request) {
	vv, err := s.buildVolumeView(r.Context())
	if err != nil {
		writeInternal(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"volume": vv})
}

func (s *Server) buildVolumeView(ctx context.Context) (volumeView, error) {
	latest, err := s.opts.Store.LatestDiskUsage(ctx)
	if err != nil {
		return volumeView{}, err
	}
	var vv volumeView
	if s.opts.Config != nil {
		v := s.opts.Config.Volume
		vv = volumeView{
			Name: v.Name, Path: v.Path,
			TargetFreePercent:    v.DiskPressure.TargetFreePercent,
			ThresholdFreePercent: v.DiskPressure.ThresholdFreePercent,
		}
	}
	if latest != nil {
		vv.Path = latest.Path
		vv.TotalBytes = latest.TotalBytes
		vv.UsedBytes = latest.UsedBytes
		vv.FreeBytes = latest.FreeBytes
		vv.FreePercent = latest.FreePercent
		t := latest.Timestamp
		vv.MeasuredAt = &t
	}
	return vv, nil
}

type volumeHistoryPoint struct {
	Timestamp   time.Time `json:"ts"`
	TotalBytes  int64     `json:"total_bytes"`
	UsedBytes   int64     `json:"used_bytes"`
	FreeBytes   int64     `json:"free_bytes"`
	FreePercent float64   `json:"free_percent"`
}

func (s *Server) handleVolumeHistory(w http.ResponseWriter, r *http.Request) {
	since := sinceParam(r, 24*time.Hour)
	limit := intParam(r.URL.Query(), "limit", 2000, 1, 10000)
	pts, err := s.opts.Store.ListDiskUsageHistory(r.Context(), since, limit)
	if err != nil {
		writeInternal(w, err)
		return
	}
	out := make([]volumeHistoryPoint, len(pts))
	for i, p := range pts {
		out[i] = volumeHistoryPoint{
			Timestamp: p.Timestamp, TotalBytes: p.TotalBytes,
			UsedBytes: p.UsedBytes, FreeBytes: p.FreeBytes,
			FreePercent: p.FreePercent,
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"history": out})
}

type arrView struct {
	Name            string     `json:"name"`
	Type            string     `json:"type"`
	URL             string     `json:"url"`
	Healthy         bool       `json:"healthy"`
	LastHealthCheck *time.Time `json:"last_health_check,omitempty"`
	LastError       string     `json:"last_error,omitempty"`
}

func (s *Server) handleListArrs(w http.ResponseWriter, r *http.Request) {
	out, err := s.buildArrViews(r.Context())
	if err != nil {
		writeInternal(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"arrs": out})
}

func (s *Server) buildArrViews(ctx context.Context) ([]arrView, error) {
	rows, err := s.opts.Store.ListArrInstances(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]arrView, len(rows))
	for i, row := range rows {
		v := arrView{
			Name: row.Kind, Type: row.Kind, URL: row.URL,
			Healthy: row.Healthy, LastHealthCheck: row.LastHealthCheck,
		}
		if row.LastError != nil {
			v.LastError = *row.LastError
		}
		out[i] = v
	}
	return out, nil
}

type actionView struct {
	ID           int64      `json:"id"`
	RunID        int64      `json:"run_id"`
	Rank         int        `json:"rank"`
	TorrentHash  string     `json:"torrent_hash"`
	TorrentName  string     `json:"torrent_name,omitempty"`
	Status       string     `json:"status"`
	StartedAt    time.Time  `json:"started_at"`
	FinishedAt   *time.Time `json:"finished_at,omitempty"`
	FreedBytes   int64      `json:"freed_bytes"`
}

type actionListResponse struct {
	Actions []actionView `json:"actions"`
	Total   int          `json:"total"`
	Limit   int          `json:"limit"`
	Offset  int          `json:"offset"`
}

func (s *Server) handleListActions(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	limit := intParam(q, "limit", 50, 1, 500)
	offset := intParam(q, "offset", 0, 0, 1_000_000)
	rows, err := s.opts.Store.ListActionsRecent(r.Context(), limit, offset)
	if err != nil {
		writeInternal(w, err)
		return
	}
	total, err := s.opts.Store.CountActions(r.Context())
	if err != nil {
		writeInternal(w, err)
		return
	}
	names, _ := s.opts.Store.TorrentNamesByHashes(r.Context(), hashesFromActions(rows))
	writeJSON(w, http.StatusOK, actionListResponse{
		Actions: viewsFromActions(rows, names), Total: total, Limit: limit, Offset: offset,
	})
}

func actionToView(a triagearr.Action, names map[triagearr.Hash]string) actionView {
	v := actionView{
		ID: a.ID, RunID: a.RunID, Rank: a.Rank,
		TorrentHash: string(a.TorrentHash), Status: string(a.Status),
		StartedAt: a.StartedAt, FreedBytes: a.FreedBytes,
	}
	if n, ok := names[a.TorrentHash]; ok {
		v.TorrentName = n
	}
	if !a.FinishedAt.IsZero() {
		finished := a.FinishedAt
		v.FinishedAt = &finished
	}
	return v
}

func viewsFromActions(rows []triagearr.Action, names map[triagearr.Hash]string) []actionView {
	out := make([]actionView, len(rows))
	for i, a := range rows {
		out[i] = actionToView(a, names)
	}
	return out
}

func hashesFromActions(rows []triagearr.Action) []triagearr.Hash {
	h := make([]triagearr.Hash, len(rows))
	for i, a := range rows {
		h[i] = a.TorrentHash
	}
	return h
}

func (s *Server) handleRunActions(w http.ResponseWriter, r *http.Request) {
	id, ok := parseIDPath(w, r)
	if !ok {
		return
	}
	rows, err := s.opts.Store.ListActionsByRun(r.Context(), id)
	if err != nil {
		writeInternal(w, err)
		return
	}
	names, _ := s.opts.Store.TorrentNamesByHashes(r.Context(), hashesFromActions(rows))
	writeJSON(w, http.StatusOK, map[string]any{"actions": viewsFromActions(rows, names)})
}

type auditView struct {
	ID        int64     `json:"id"`
	Timestamp time.Time `json:"ts"`
	Step      string    `json:"step"`
	Outcome   string    `json:"outcome"`
	ArrType   string    `json:"arr_type,omitempty"`
	ArrFileID int64     `json:"arr_file_id,omitempty"`
	Detail    string    `json:"detail,omitempty"`
}

type actionDetailResponse struct {
	Action actionView  `json:"action"`
	Audit  []auditView `json:"audit"`
}

func (s *Server) handleGetAction(w http.ResponseWriter, r *http.Request) {
	id, ok := parseIDPath(w, r)
	if !ok {
		return
	}
	a, err := s.opts.Store.GetAction(r.Context(), id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			writeError(w, http.StatusNotFound, "action not found")
			return
		}
		writeInternal(w, err)
		return
	}
	audit, err := s.opts.Store.ListAuditByAction(r.Context(), id)
	if err != nil {
		writeInternal(w, err)
		return
	}
	names, _ := s.opts.Store.TorrentNamesByHashes(r.Context(), []triagearr.Hash{a.TorrentHash})
	resp := actionDetailResponse{
		Action: actionToView(a, names),
		Audit:  make([]auditView, len(audit)),
	}
	for i, e := range audit {
		resp.Audit[i] = auditView{
			ID: e.ID, Timestamp: e.Timestamp,
			Step: string(e.Step), Outcome: string(e.Outcome),
			ArrType: e.ArrType, ArrFileID: e.ArrFileID, Detail: e.Detail,
		}
	}
	writeJSON(w, http.StatusOK, resp)
}

type summaryResponse struct {
	Volume   volumeView      `json:"volume"`
	Arrs     []arrView       `json:"arrs"`
	Counts   summaryCounts   `json:"counts"`
	LastRuns []runResponse   `json:"last_runs"`
	TopScore []scoreListItem `json:"top_score"`
}

type summaryCounts struct {
	Torrents int `json:"torrents"`
	Scored   int `json:"scored"`
	Actions  int `json:"actions"`
}

func (s *Server) handleSummary(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// Fan out the seven independent reads across the reader pool. Each goroutine
	// owns its slot in the response struct, so no mutex is needed.
	var (
		wg       sync.WaitGroup
		volume   volumeView
		arrs     []arrView
		counts   summaryCounts
		lastRuns []runResponse
		top      []scoreListItem
	)
	run := func(label string, fn func() error) {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := fn(); err != nil {
				slog.Warn("summary: "+label, "err", err)
			}
		}()
	}
	run("volume", func() error {
		vv, err := s.buildVolumeView(ctx)
		volume = vv
		return err
	})
	run("arrs", func() error {
		a, err := s.buildArrViews(ctx)
		arrs = a
		return err
	})
	run("count torrents", func() error {
		n, err := s.opts.Store.CountTorrents(ctx)
		counts.Torrents = n
		return err
	})
	run("count scored", func() error {
		n, err := s.opts.Store.CountScored(ctx)
		counts.Scored = n
		return err
	})
	run("count actions", func() error {
		n, err := s.opts.Store.CountActions(ctx)
		counts.Actions = n
		return err
	})
	run("list runs", func() error {
		runs, err := s.opts.Store.ListRuns(ctx, store.ListRunsOpts{Limit: 10})
		if err != nil {
			return err
		}
		lastRuns = make([]runResponse, len(runs))
		for i, rn := range runs {
			lastRuns[i] = buildResponse(rn, nil, nil)
		}
		return nil
	})
	run("list scores", func() error {
		rows, err := s.opts.Store.ListScores(ctx, store.ListScoresOpts{Limit: 10})
		if err != nil {
			return err
		}
		top = make([]scoreListItem, len(rows))
		for i, row := range rows {
			top[i] = scoreItemFromRow(row)
		}
		return nil
	})
	wg.Wait()

	writeJSON(w, http.StatusOK, summaryResponse{
		Volume: volume, Arrs: arrs, Counts: counts,
		LastRuns: lastRuns, TopScore: top,
	})
}

func (s *Server) handleConfig(w http.ResponseWriter, _ *http.Request) {
	if s.opts.Config == nil {
		writeError(w, http.StatusServiceUnavailable, "config not wired into server")
		return
	}
	writeJSON(w, http.StatusOK, s.opts.Config.Redacted())
}

func (s *Server) handleVersion(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, s.opts.Version)
}

// arrBaseURL returns the browser-facing base URL for the given arr type, used
// to build deep links in the dashboard. It reads the DB-owned arr_connections
// row (ADR-0022) and prefers public_url when set, falling back to the internal
// url (consumed by API clients). Empty when the kind is unknown or the row is
// absent.
func (s *Server) arrBaseURL(ctx context.Context, t triagearr.ArrType) string {
	if s.opts.Store == nil {
		return ""
	}
	conn, err := s.opts.Store.GetArrConnectionByKind(ctx, string(t))
	if err != nil {
		return ""
	}
	if conn.PublicURL != "" {
		return strings.TrimRight(conn.PublicURL, "/")
	}
	return strings.TrimRight(conn.URL, "/")
}

func intParam(q url.Values, key string, def, min, max int) int {
	raw := q.Get(key)
	if raw == "" {
		return def
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n < min || n > max {
		return def
	}
	return n
}

func boolParam(q url.Values, key string) bool {
	v := q.Get(key)
	return v == "1" || v == "true"
}

// sinceParam reads ?since=<rfc3339> or ?since=<duration ago>, falling back to
// `now - defaultWindow`.
func sinceParam(r *http.Request, defaultWindow time.Duration) time.Time {
	raw := r.URL.Query().Get("since")
	if raw == "" {
		return time.Now().UTC().Add(-defaultWindow)
	}
	if t, err := time.Parse(time.RFC3339, raw); err == nil {
		return t.UTC()
	}
	if d, err := time.ParseDuration(raw); err == nil {
		return time.Now().UTC().Add(-d)
	}
	return time.Now().UTC().Add(-defaultWindow)
}
