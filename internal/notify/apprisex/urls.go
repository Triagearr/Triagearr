package apprisex

import (
	"fmt"
	"net/url"
	"regexp"
	"strconv"
	"strings"
)

// The builders below assemble Apprise service URLs from structured operator
// input, following the upstream Apprise URL schema. Free-form segments
// (passwords, ntfy topics, email addresses) are escaped via net/url; the
// fixed-charset token segments (bot tokens, webhook IDs) are placed in the path
// where Apprise expects them literally. The resulting URL carries credentials
// and must never be logged or returned to the UI.

// telegramToken is the Telegram bot-token shape: "<numeric id>:<secret>". The
// secret uses an URL-safe charset, so the whole token sits in the path with its
// colon intact (the Apprise tgram:// scheme reads it that way).
var telegramToken = regexp.MustCompile(`^[0-9]+:[A-Za-z0-9_-]+$`)

// TelegramURL builds tgram://{token}/{chatID}/.
func TelegramURL(token, chatID string) (string, error) {
	if token == "" {
		return "", fmt.Errorf("telegram: bot token required")
	}
	if chatID == "" {
		return "", fmt.Errorf("telegram: chat id required")
	}
	if !telegramToken.MatchString(token) {
		return "", fmt.Errorf("telegram: bot token must be <id>:<secret>")
	}
	return fmt.Sprintf("tgram://%s/%s/", token, url.PathEscape(chatID)), nil
}

// DiscordURL builds discord://{webhookID}/{webhookToken} from a Discord webhook
// URL of the form https://discord.com/api/webhooks/{webhookID}/{token}.
func DiscordURL(webhookURL string) (string, error) {
	parsed, err := url.Parse(strings.TrimSpace(webhookURL))
	if err != nil {
		return "", fmt.Errorf("discord: parsing webhook URL: %w", err)
	}
	id, token, err := segmentsAfter(parsed.Path, "webhooks")
	if err != nil {
		return "", fmt.Errorf("discord: webhook URL must look like .../webhooks/{id}/{token}: %w", err)
	}
	return fmt.Sprintf("discord://%s/%s", url.PathEscape(id), url.PathEscape(token)), nil
}

// NtfyURL builds ntfy(s)://[{user}:{pass}@]{host}/{topic}. server may include a
// scheme ("https://ntfy.sh") or be a bare host ("ntfy.sh"); https selects the
// ntfys scheme. Severity is conveyed out of band by the provider (NotifyType),
// so it is not encoded here.
func NtfyURL(server, topic, username, password string) (string, error) {
	if topic == "" {
		return "", fmt.Errorf("ntfy: topic required")
	}
	host := strings.TrimSpace(server)
	scheme := "ntfys" // default to TLS (ntfy.sh)
	if host == "" {
		host = "ntfy.sh"
	} else if after, ok := strings.CutPrefix(host, "http://"); ok {
		host, scheme = after, "ntfy"
	} else {
		host = strings.TrimPrefix(host, "https://")
	}
	host = strings.TrimSuffix(host, "/")
	u := url.URL{
		Scheme: scheme,
		Host:   host,
		Path:   "/" + topic,
	}
	if username != "" || password != "" {
		u.User = url.UserPassword(username, password)
		u.RawQuery = url.Values{"mode": {"private"}}.Encode()
	}
	return u.String(), nil
}

// EmailURL builds mailto(s)://{user}:{pass}@{host}:{port}/?to=&from=. mailtos
// selects TLS/StartTLS; mailto sends insecure.
func EmailURL(host string, port int, username, password, from string, to []string, useStartTLS bool) (string, error) {
	if host == "" {
		return "", fmt.Errorf("email: host required")
	}
	if from == "" {
		return "", fmt.Errorf("email: from address required")
	}
	if len(to) == 0 {
		return "", fmt.Errorf("email: at least one recipient required")
	}
	if port == 0 {
		port = 587
	}
	scheme := "mailtos"
	if !useStartTLS {
		scheme = "mailto"
	}
	u := url.URL{
		Scheme: scheme,
		Host:   host + ":" + strconv.Itoa(port),
		Path:   "/",
	}
	if username != "" || password != "" {
		u.User = url.UserPassword(username, password)
	}
	u.RawQuery = url.Values{
		"to":   {strings.Join(to, ",")},
		"from": {from},
	}.Encode()
	return u.String(), nil
}

// SlackURL builds slack://{T}/{B}/{X}?mode=hook from a Slack incoming-webhook
// URL of the form https://hooks.slack.com/services/{T}/{B}/{X}.
func SlackURL(webhookURL string) (string, error) {
	parsed, err := url.Parse(strings.TrimSpace(webhookURL))
	if err != nil {
		return "", fmt.Errorf("slack: parsing webhook URL: %w", err)
	}
	parts := strings.Split(strings.Trim(parsed.Path, "/"), "/")
	tok := tokensAfter(parts, "services", 3)
	if tok == nil {
		return "", fmt.Errorf("slack: webhook URL must look like .../services/{T}/{B}/{X}")
	}
	return fmt.Sprintf("slack://%s/%s/%s?mode=hook",
		url.PathEscape(tok[0]), url.PathEscape(tok[1]), url.PathEscape(tok[2])), nil
}

// segmentsAfter returns the two path segments immediately following the named
// anchor segment, erroring if the structure doesn't match.
func segmentsAfter(path, anchor string) (a, b string, err error) {
	parts := strings.Split(strings.Trim(path, "/"), "/")
	for i, p := range parts {
		if p == anchor && i+2 < len(parts) && parts[i+1] != "" && parts[i+2] != "" {
			return parts[i+1], parts[i+2], nil
		}
	}
	return "", "", fmt.Errorf("missing %q/{a}/{b} segments", anchor)
}

// tokensAfter returns exactly n non-empty path segments immediately following
// the named anchor, or nil if the structure doesn't match.
func tokensAfter(parts []string, anchor string, n int) []string {
	for i, p := range parts {
		if p != anchor || i+n >= len(parts) {
			continue
		}
		out := parts[i+1 : i+1+n]
		for _, s := range out {
			if s == "" {
				return nil
			}
		}
		return out
	}
	return nil
}
