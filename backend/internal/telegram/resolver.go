package telegram

import (
	"errors"
	"net/url"
	"strings"
)

func ResolveUsername(input ResolveInput) (string, string, error) {
	if strings.TrimSpace(input.Username) != "" {
		username := normalizeUsername(input.Username)
		return username, "https://t.me/" + username, nil
	}

	rawURL := strings.TrimSpace(input.URL)
	if rawURL == "" {
		return "", "", errors.New("either url or username is required")
	}

	parsed, err := url.Parse(rawURL)
	if err != nil {
		return "", "", errors.New("invalid url")
	}

	host := strings.ToLower(parsed.Hostname())
	if host != "t.me" && host != "www.t.me" {
		return "", "", errors.New("only t.me public channel urls are supported")
	}

	parts := strings.Split(strings.Trim(parsed.Path, "/"), "/")
	if len(parts) == 0 || parts[0] == "" {
		return "", "", errors.New("channel username is missing in url")
	}

	username := normalizeUsername(parts[0])
	return username, "https://t.me/" + username, nil
}

func normalizeUsername(s string) string {
	s = strings.TrimSpace(strings.ToLower(s))
	s = strings.TrimPrefix(s, "@")
	return s
}
