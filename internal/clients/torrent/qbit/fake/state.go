// Package fake is an in-memory qBittorrent WebUI v2 server suitable for
// integration tests and dev fixtures. It implements the subset of endpoints
// consumed by [github.com/Triagearr/Triagearr/internal/clients/qbit].
//
// State is fully in-memory; restarting the process resets it. Concurrent
// callers are serialized via a single RWMutex on State.
package fake

import (
	"sync"
	"sync/atomic"
)

// Torrent is the wire-level shape qBit returns from /api/v2/torrents/info,
// augmented with the per-torrent files and trackers the fake also serves
// (qBit exposes those via separate endpoints; we keep them attached for
// scenario authoring convenience).
type Torrent struct {
	Hash          string  `json:"hash" yaml:"hash"`
	Name          string  `json:"name" yaml:"name"`
	Category      string  `json:"category" yaml:"category"`
	SavePath      string  `json:"save_path" yaml:"save_path"`
	Size          int64   `json:"size" yaml:"size"`
	AddedOn       int64   `json:"added_on" yaml:"added_on"`
	CompletionOn  int64   `json:"completion_on" yaml:"completion_on"`
	Ratio         float64 `json:"ratio" yaml:"ratio"`
	Uploaded      int64   `json:"uploaded" yaml:"uploaded"`
	NumSeeds      int     `json:"num_seeds" yaml:"num_seeds"`
	NumComplete   int     `json:"num_complete" yaml:"num_complete"`
	NumLeechs     int     `json:"num_leechs" yaml:"num_leechs"`
	NumIncomplete int     `json:"num_incomplete" yaml:"num_incomplete"`
	State         string  `json:"state" yaml:"state"`
	LastActivity  int64   `json:"last_activity" yaml:"last_activity"`
	Private       bool    `json:"private" yaml:"private"`
	Tags          string  `json:"tags" yaml:"tags"`

	Files    []File    `json:"-" yaml:"files,omitempty"`
	Trackers []Tracker `json:"-" yaml:"trackers,omitempty"`
}

// File is one entry of /api/v2/torrents/files.
type File struct {
	Name     string  `json:"name" yaml:"name"`
	Size     int64   `json:"size" yaml:"size"`
	Progress float64 `json:"progress" yaml:"progress"`
}

// Tracker is one entry of /api/v2/torrents/trackers. Status follows qBit's
// enum (0=disabled, 1=not_contacted, 2=working, 3=updating, 4=not_working).
type Tracker struct {
	URL    string `json:"url" yaml:"url"`
	Status int    `json:"status" yaml:"status"`
	Msg    string `json:"msg" yaml:"msg"`
}

// State is the mutable in-memory backing store for the fake.
//
// When username/password are empty, the fake mirrors qBit's auth-bypass mode
// and accepts requests without a session cookie. Otherwise /api/v2/auth/login
// must succeed before any other endpoint will respond.
type State struct {
	username string
	password string

	mu       sync.RWMutex
	torrents map[string]*Torrent

	sessions sync.Map // cookie value (string) → struct{}{}

	deleteCalls atomic.Int64
}

// NewState builds an empty State. Pass empty username to disable auth.
func NewState(username, password string) *State {
	return &State{
		username: username,
		password: password,
		torrents: make(map[string]*Torrent),
	}
}

// Add inserts a torrent, overwriting any existing entry with the same hash.
func (s *State) Add(t Torrent) {
	s.mu.Lock()
	defer s.mu.Unlock()
	cp := t
	s.torrents[t.Hash] = &cp
}

// AddMany is a convenience for seed scripts.
func (s *State) AddMany(ts []Torrent) {
	for _, t := range ts {
		s.Add(t)
	}
}

// Delete removes a torrent. Returns true if the hash existed.
func (s *State) Delete(hash string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.torrents[hash]; !ok {
		return false
	}
	delete(s.torrents, hash)
	return true
}

// List returns a deep-enough copy of all torrents (the slice itself and each
// torrent struct are copies; nested files/trackers are shared but never
// mutated by handlers).
func (s *State) List() []Torrent {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]Torrent, 0, len(s.torrents))
	for _, t := range s.torrents {
		out = append(out, *t)
	}
	return out
}

// Get returns a torrent by hash. The second return is false if absent.
func (s *State) Get(hash string) (Torrent, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	t, ok := s.torrents[hash]
	if !ok {
		return Torrent{}, false
	}
	return *t, true
}

// Len returns the current torrent count.
func (s *State) Len() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.torrents)
}

// DeleteCalls returns how many times /api/v2/torrents/delete has been hit.
// Useful for tests that assert ordering or call counts.
func (s *State) DeleteCalls() int64 {
	return s.deleteCalls.Load()
}

// authBypassed reports whether the fake was constructed without credentials.
func (s *State) authBypassed() bool {
	return s.username == ""
}

// checkCredentials validates a login form payload against the configured
// username/password. Returns true on match (or when auth is bypassed).
func (s *State) checkCredentials(user, pass string) bool {
	if s.authBypassed() {
		return true
	}
	return user == s.username && pass == s.password
}

// registerSession marks a cookie value as valid until State is dropped.
func (s *State) registerSession(cookie string) {
	s.sessions.Store(cookie, struct{}{})
}

// sessionValid checks whether a SID cookie was issued by login.
func (s *State) sessionValid(cookie string) bool {
	if s.authBypassed() {
		return true
	}
	_, ok := s.sessions.Load(cookie)
	return ok
}
