package hh

// adapter.go contains adapter functions for mapping HH API responses
// to internal structures. When the actual API response shape differs
// from what's documented, modifications should be isolated here.

import (
	"encoding/json"
	"fmt"
)

// ADAPT_TO_REAL_API_RESPONSE
// ParseNegotiationsResponse parses the top-level negotiations response.
// The actual shape of items/collections may differ. Adapt here.
func ParseNegotiationsResponse(data []byte) (*NegotiationsResponse, error) {
	var resp NegotiationsResponse
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, fmt.Errorf("parse negotiations response: %w (raw: %.500s)", err, data)
	}
	return &resp, nil
}

// ADAPT_TO_REAL_API_RESPONSE
// ParseCollectionResponse parses a collection page response.
// HH API may return the same structure as NegotiationsResponse
// but without the collections field.
func ParseCollectionResponse(data []byte) (*CollectionResponse, error) {
	var resp CollectionResponse
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, fmt.Errorf("parse collection response: %w (raw: %.500s)", err, data)
	}
	return &resp, nil
}

// ADAPT_TO_REAL_API_RESPONSE
// ParseResume parses a full resume response.
func ParseResume(data []byte) (*Resume, error) {
	var r Resume
	if err := json.Unmarshal(data, &r); err != nil {
		return nil, fmt.Errorf("parse resume: %w (raw: %.500s)", err, data)
	}
	return &r, nil
}

// ADAPT_TO_REAL_API_RESPONSE
// ExtractContactValue extracts a displayable string from contact.value.
// HH API returns different shapes for phone vs email vs other:
//   - email: string
//   - phone: {"country": "7", "city": "495", "number": "1234567", "formatted": "+7 (495) 123-45-67"}
func ExtractContactValue(c Contact) string {
	if c.Value == nil {
		return ""
	}

	switch v := c.Value.(type) {
	case string:
		return v
	case map[string]any:
		if formatted, ok := v["formatted"].(string); ok && formatted != "" {
			return formatted
		}
		// Fallback: try to build from parts.
		country, _ := v["country"].(string)
		city, _ := v["city"].(string)
		number, _ := v["number"].(string)
		if number != "" {
			result := "+"
			if country != "" {
				result += country
			}
			if city != "" {
				result += " (" + city + ") "
			}
			result += number
			return result
		}
	}

	// Last resort: marshal back to JSON.
	b, err := json.Marshal(c.Value)
	if err != nil {
		return fmt.Sprintf("%v", c.Value)
	}
	return string(b)
}

// ExtractAllContacts formats all contacts from a resume into readable strings.
func ExtractAllContacts(contacts []Contact) []string {
	var result []string
	for _, c := range contacts {
		val := ExtractContactValue(c)
		if val == "" {
			continue
		}
		entry := c.Type.Name + ": " + val
		if c.Comment != "" {
			entry += " (" + c.Comment + ")"
		}
		result = append(result, entry)
	}
	return result
}
