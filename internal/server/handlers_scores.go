package server

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/Triagearr/Triagearr/internal/store"
)

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
	CandidateBoost   bool            `json:"candidate_boost"`
}

func (s *Server) handleListScores(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	rows, err := s.opts.Store.ListScores(r.Context(), store.ListScoresOpts{
		Limit:           intParam(q, "limit", 50, 1, 500),
		IncludeExcluded: boolParam(q, "include_excluded"),
		WithFactors:     true,
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
		CandidateBoost:   row.CandidateBoost,
	}
}
