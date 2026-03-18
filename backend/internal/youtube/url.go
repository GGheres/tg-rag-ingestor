package youtube

import (
	"fmt"
	"net/url"
	"strings"
)

func NormalizeURL(raw string) (string, string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", "", fmt.Errorf("youtube url is required")
	}
	if !strings.Contains(raw, "://") {
		raw = "https://" + raw
	}

	parsed, err := url.Parse(raw)
	if err != nil {
		return "", "", fmt.Errorf("invalid youtube url: %w", err)
	}

	host := strings.ToLower(strings.TrimPrefix(parsed.Hostname(), "www."))
	var videoID string

	switch host {
	case "youtube.com", "m.youtube.com", "music.youtube.com":
		videoID = extractYouTubeComVideoID(parsed)
	case "youtu.be":
		videoID = strings.Trim(parsed.Path, "/")
		if strings.Contains(videoID, "/") {
			videoID = strings.SplitN(videoID, "/", 2)[0]
		}
	default:
		return "", "", fmt.Errorf("unsupported host %q: expected youtube.com or youtu.be", parsed.Hostname())
	}

	videoID = sanitizeVideoID(videoID)
	if !isLikelyVideoID(videoID) {
		return "", "", fmt.Errorf("youtube url does not contain a valid video id")
	}

	canonical := "https://www.youtube.com/watch?v=" + videoID
	return canonical, videoID, nil
}

func extractYouTubeComVideoID(parsed *url.URL) string {
	path := strings.Trim(parsed.Path, "/")
	parts := strings.Split(path, "/")

	if len(parts) == 0 {
		return ""
	}

	switch parts[0] {
	case "watch":
		return parsed.Query().Get("v")
	case "shorts", "embed", "live", "v":
		if len(parts) > 1 {
			return parts[1]
		}
	}

	if v := parsed.Query().Get("v"); strings.TrimSpace(v) != "" {
		return v
	}
	return ""
}

func sanitizeVideoID(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	for _, sep := range []string{"?", "&", "#", "/"} {
		if idx := strings.Index(raw, sep); idx >= 0 {
			raw = raw[:idx]
		}
	}
	return raw
}

func isLikelyVideoID(videoID string) bool {
	if len(videoID) < 6 || len(videoID) > 32 {
		return false
	}
	for _, r := range videoID {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_' {
			continue
		}
		return false
	}
	return true
}
