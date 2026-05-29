package logging_test

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/Triagearr/Triagearr/internal/logging"
)

func TestSetup_LevelFromEnv(t *testing.T) {
	ctx := context.Background()

	cases := []struct {
		env     string
		enabled slog.Level
		off     slog.Level
	}{
		{"debug", slog.LevelDebug, slog.LevelDebug - 1},
		{"warn", slog.LevelWarn, slog.LevelInfo},
		{"warning", slog.LevelWarn, slog.LevelInfo},
		{"error", slog.LevelError, slog.LevelWarn},
		{"", slog.LevelInfo, slog.LevelDebug},
		{"garbage", slog.LevelInfo, slog.LevelDebug},
	}
	for _, tc := range cases {
		t.Run(tc.env, func(t *testing.T) {
			t.Setenv("TRIAGEARR_LOG_LEVEL", tc.env)
			logger := logging.Setup()
			require.NotNil(t, logger)
			require.True(t, logger.Enabled(ctx, tc.enabled), "expected %s to be enabled", tc.enabled)
			require.False(t, logger.Enabled(ctx, tc.off), "expected %s to be filtered out", tc.off)
		})
	}
}

func TestRedactHandler_WithGroupRedactsNestedSecret(t *testing.T) {
	var buf bytes.Buffer
	inner := slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})
	logger := slog.New(logging.NewRedactHandler(inner))

	logger.WithGroup("arr").Info("connected", "api_key", "leaky-key", "url", "http://sonarr")

	out := buf.String()
	require.NotContains(t, out, "leaky-key", "secret under a group must be redacted")
	require.Contains(t, out, "***")

	var decoded map[string]any
	require.NoError(t, json.Unmarshal([]byte(strings.TrimSpace(out)), &decoded))
	arr, ok := decoded["arr"].(map[string]any)
	require.True(t, ok, "group key must be present in output")
	require.Equal(t, "***", arr["api_key"])
}
