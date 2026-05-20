package server

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"strings"
)

// LoadOrGenerateAPIKey returns the API key stored at path. When the file does
// not exist, it generates a random 32-byte hex key, writes it in 0600, and
// returns generated=true so the caller can announce it in the logs once.
//
// Mirrors Sonarr/Radarr's behaviour: there is always an API key, the user
// never has to think about it, and the file is the single source of truth.
func LoadOrGenerateAPIKey(path string) (key string, generated bool, err error) {
	raw, err := os.ReadFile(path) //nolint:gosec // path is the operator-configured data dir.
	if err == nil {
		k := strings.TrimSpace(string(raw))
		if k == "" {
			return "", false, fmt.Errorf("api_key file %q is empty — delete it to regenerate", path)
		}
		return k, false, nil
	}
	if !errors.Is(err, fs.ErrNotExist) {
		return "", false, fmt.Errorf("reading api_key %q: %w", path, err)
	}

	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", false, fmt.Errorf("generating api_key: %w", err)
	}
	k := hex.EncodeToString(buf)
	if err := os.WriteFile(path, []byte(k+"\n"), 0o600); err != nil {
		return "", false, fmt.Errorf("writing api_key to %q: %w", path, err)
	}
	return k, true, nil
}
