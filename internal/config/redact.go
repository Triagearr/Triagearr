package config

// Redacted returns a deep copy of the config with every secret-bearing field
// replaced by RedactedPlaceholder. The result is safe to expose through the
// HTTP API or to log.
//
// Secrets covered:
//   - Qbit.Password
//   - Arrs.<type>[].APIKey
//
// Non-secret values (URLs, names, intervals, etc.) are preserved verbatim so
// the UI can still display effective config.
func (c Config) Redacted() Config {
	out := c

	if out.Qbit.Password != "" {
		out.Qbit.Password = RedactedPlaceholder
	}

	for _, slot := range []*[]ArrInstanceConfig{
		&out.Arrs.Sonarr, &out.Arrs.Radarr, &out.Arrs.Lidarr,
		&out.Arrs.Readarr, &out.Arrs.WhisparrV2, &out.Arrs.WhisparrV3,
	} {
		*slot = redactArrSlice(*slot)
	}

	return out
}

// RedactedPlaceholder is what every redacted secret is replaced by. The UI
// special-cases this string to render a "secret" badge.
const RedactedPlaceholder = "***"

func redactArrSlice(in []ArrInstanceConfig) []ArrInstanceConfig {
	if len(in) == 0 {
		return in
	}
	out := make([]ArrInstanceConfig, len(in))
	copy(out, in)
	for i := range out {
		if out[i].APIKey != "" {
			out[i].APIKey = RedactedPlaceholder
		}
	}
	return out
}
