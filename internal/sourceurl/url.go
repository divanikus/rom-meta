package sourceurl

import (
	"net/url"
	"strings"
)

var sensitiveQueryKeys = map[string]bool{
	"access_token":  true,
	"apikey":        true,
	"api_key":       true,
	"authorization": true,
	"client_secret": true,
	"devid":         true,
	"devpassword":   true,
	"key":           true,
	"password":      true,
	"softname":      true,
	"ssid":          true,
	"sspassword":    true,
	"token":         true,
	"username":      true,
}

// StripSecrets removes credentials from URLs before they are written to
// shareable catalogs, downloader plans, logs, or failure reports.
func StripSecrets(raw string) string {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return raw
	}
	parsed.User = nil
	query := parsed.Query()
	for key := range query {
		if IsSensitiveQueryKey(key) {
			query.Del(key)
		}
	}
	parsed.RawQuery = query.Encode()
	return parsed.String()
}

// AddQuery merges non-empty query values into a URL.
func AddQuery(raw string, values map[string]string) string {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return raw
	}
	query := parsed.Query()
	for key, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		query.Set(key, value)
	}
	parsed.RawQuery = query.Encode()
	return parsed.String()
}

// IsSensitiveQueryKey reports whether a query parameter commonly carries a
// secret or account identifier that must not be stored in shareable metadata.
func IsSensitiveQueryKey(key string) bool {
	key = strings.ToLower(strings.TrimSpace(key))
	if key == "" {
		return false
	}
	if sensitiveQueryKeys[key] {
		return true
	}
	return strings.Contains(key, "password") ||
		strings.Contains(key, "secret") ||
		strings.Contains(key, "token")
}
