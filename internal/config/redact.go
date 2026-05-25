package config

// Redacted returns a deep copy of the config with every secret-bearing field
// replaced by RedactedPlaceholder. The result is safe to expose through the
// HTTP API or to log.
//
// Secrets covered:
//   - TorrentClients.<kind>.Password
//   - Arrs.<type>.APIKey
//   - Notifications.Telegram.BotToken
//
// Non-secret values (URLs, names, intervals, etc.) are preserved verbatim so
// the UI can still display effective config.
func (c Config) Redacted() Config {
	out := c

	out.TorrentClients.EachPtr(func(_ string, inst *TorrentClientInstanceConfig) {
		if inst.Password != "" {
			inst.Password = RedactedPlaceholder
		}
	})

	if out.Notifications.Telegram.BotToken != "" {
		out.Notifications.Telegram.BotToken = RedactedPlaceholder
	}

	out.Arrs.EachPtr(func(_ string, inst *ArrInstanceConfig) {
		if inst.APIKey != "" {
			inst.APIKey = RedactedPlaceholder
		}
	})

	return out
}

// RedactedPlaceholder is what every redacted secret is replaced by. The UI
// special-cases this string to render a "secret" badge.
const RedactedPlaceholder = "***"
