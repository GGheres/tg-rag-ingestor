package naming

import (
	"fmt"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"tg-rag-ingestor/backend/internal/models"
)

func ExportFilename(source *models.Source, mode string, format string, now time.Time) string {
	base := "all_sources"
	if source != nil {
		if stem := SourceFileStem(*source); stem != "" {
			base = stem
		}
	}

	extension := ".jsonl"
	if format == "txt_rag" {
		extension = ".txt"
	}

	mode = strings.TrimSpace(mode)
	if mode == "" {
		mode = "documents"
	}

	return fmt.Sprintf("%s_%s_%s%s", base, mode, now.UTC().Format("20060102_150405"), extension)
}

func DocumentFilename(source models.Source, doc models.Document) string {
	base := SourceFileStem(source)
	switch source.SourceType {
	case "telegram_public_channel", "json_upload":
		if messageID, ok := int64FromAny(doc.Metadata["message_id"]); ok && messageID > 0 {
			if base == "" {
				return strconv.FormatInt(messageID, 10)
			}
			return fmt.Sprintf("%s_%d", base, messageID)
		}
	case "telegram_message_links":
		if base != "" {
			return base
		}
	case "telegram_channel_document":
		if base != "" {
			return base
		}
	case "youtube_video":
		if videoID := SanitizeFilename(stringFromAny(doc.Metadata["video_id"])); videoID != "" {
			if base == "" || base == videoID {
				return videoID
			}
			return base + "_" + videoID
		}
		if base != "" {
			return base
		}
	}

	if externalDocID := SanitizeFilename(doc.ExternalDocID); externalDocID != "" {
		return externalDocID
	}
	if base != "" {
		return base
	}
	if documentID := SanitizeFilename(doc.ID); documentID != "" {
		return "document_" + documentID
	}
	return "document"
}

func SourceFileStem(source models.Source) string {
	for _, candidate := range []string{
		deref(source.Title),
		deref(source.Username),
		deref(source.ExternalID),
		deriveURLStem(source.URL),
		source.ID,
	} {
		if stem := SanitizeFilename(candidate); stem != "" {
			return stem
		}
	}
	return "source"
}

func SanitizeFilename(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}

	var builder strings.Builder
	for _, r := range raw {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_' || r == '-' || r == '.' {
			builder.WriteRune(r)
			continue
		}
		builder.WriteRune('_')
	}

	out := strings.Trim(builder.String(), "._")
	for strings.Contains(out, "__") {
		out = strings.ReplaceAll(out, "__", "_")
	}
	return out
}

func deriveURLStem(rawURL string) string {
	rawURL = strings.TrimSpace(rawURL)
	if rawURL == "" {
		return ""
	}

	parsed, err := url.Parse(rawURL)
	if err != nil {
		return rawURL
	}

	for _, candidate := range []string{
		strings.TrimSpace(filepath.Base(parsed.Path)),
		strings.TrimPrefix(parsed.Hostname(), "www."),
	} {
		if candidate != "" && candidate != "." && candidate != "/" {
			return candidate
		}
	}

	return rawURL
}

func stringFromAny(value any) string {
	switch typed := value.(type) {
	case string:
		return strings.TrimSpace(typed)
	default:
		return ""
	}
}

func int64FromAny(value any) (int64, bool) {
	switch typed := value.(type) {
	case int:
		return int64(typed), true
	case int32:
		return int64(typed), true
	case int64:
		return typed, true
	case float32:
		return int64(typed), true
	case float64:
		return int64(typed), true
	case string:
		parsed, err := strconv.ParseInt(strings.TrimSpace(typed), 10, 64)
		if err == nil {
			return parsed, true
		}
	}
	return 0, false
}

func deref(value *string) string {
	if value == nil {
		return ""
	}
	return strings.TrimSpace(*value)
}
