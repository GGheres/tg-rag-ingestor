package resumes

import (
	"encoding/json"
	"fmt"

	"tg-rag-ingestor/backend/internal/model"
)

// ADAPT_TO_REAL_API_RESPONSE
func parseResume(data []byte) (*model.Resume, error) {
	var resume model.Resume
	if err := json.Unmarshal(data, &resume); err != nil {
		return nil, fmt.Errorf("parse resume: %w", err)
	}
	return &resume, nil
}

func extractContactValue(raw any) string {
	switch value := raw.(type) {
	case string:
		return value
	case map[string]any:
		if formatted, ok := value["formatted"].(string); ok && formatted != "" {
			return formatted
		}
		country, _ := value["country"].(string)
		city, _ := value["city"].(string)
		number, _ := value["number"].(string)
		if number == "" {
			return ""
		}
		result := "+"
		if country != "" {
			result += country
		}
		if city != "" {
			result += " (" + city + ") "
		}
		result += number
		return result
	default:
		return ""
	}
}

func formatContacts(contacts []model.Contact) []string {
	out := make([]string, 0, len(contacts))
	for _, item := range contacts {
		value := extractContactValue(item.Value)
		if value == "" {
			continue
		}
		entry := item.Type.Name + ": " + value
		if item.Comment != "" {
			entry += " (" + item.Comment + ")"
		}
		out = append(out, entry)
	}
	return out
}
