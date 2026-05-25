package store

import (
	"encoding/json"
	"fmt"
)

// decodeStringList parses a JSON array column, tolerating empty text as [].
// Shared by every connection repo that persists []string as a TEXT column.
func decodeStringList(raw string) ([]string, error) {
	if raw == "" {
		return nil, nil
	}
	var out []string
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		return nil, fmt.Errorf("invalid JSON %q: %w", raw, err)
	}
	return out, nil
}

// encodeStringList renders a string slice as a JSON array column. A nil slice
// becomes "[]" so the NOT NULL column always holds valid JSON.
func encodeStringList(v []string) (string, error) {
	if v == nil {
		return "[]", nil
	}
	b, err := json.Marshal(v)
	if err != nil {
		return "", fmt.Errorf("encoding string list: %w", err)
	}
	return string(b), nil
}
