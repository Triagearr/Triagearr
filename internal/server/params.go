package server

import (
	"net/http"
	"net/url"
	"strconv"
	"time"
)

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
