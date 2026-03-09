package cleaning

import (
	"regexp"
	"strings"
)

var (
	urlPattern     = regexp.MustCompile(`https?://[^\s]+`)
	hashtagPattern = regexp.MustCompile(`(?i)(?:^|\s)(#[\p{L}\p{N}_]+)`)
	mentionPattern = regexp.MustCompile(`(?i)(?:^|\s)(@[\p{L}\p{N}_]{4,})`)
)

func ExtractLinks(input string) []string {
	found := urlPattern.FindAllString(input, -1)
	if len(found) == 0 {
		return nil
	}

	seen := make(map[string]struct{}, len(found))
	out := make([]string, 0, len(found))
	for _, item := range found {
		link := sanitizeLink(item)
		if _, exists := seen[link]; exists {
			continue
		}
		seen[link] = struct{}{}
		out = append(out, link)
	}
	return out
}

func ExtractHashtags(input string) []string {
	return extractPatternValues(hashtagPattern, input)
}

func ExtractMentions(input string) []string {
	return extractPatternValues(mentionPattern, input)
}

func RemoveLinks(input string) string {
	if strings.TrimSpace(input) == "" {
		return ""
	}
	return urlPattern.ReplaceAllString(input, "")
}

func extractPatternValues(pattern *regexp.Regexp, input string) []string {
	matches := pattern.FindAllStringSubmatch(input, -1)
	if len(matches) == 0 {
		return nil
	}

	seen := make(map[string]struct{}, len(matches))
	out := make([]string, 0, len(matches))
	for _, match := range matches {
		if len(match) < 2 {
			continue
		}
		value := strings.ToLower(strings.TrimSpace(match[1]))
		if value == "" {
			continue
		}
		if _, exists := seen[value]; exists {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	return out
}

func sanitizeLink(link string) string {
	link = strings.TrimRight(link, ".,!?)]}")
	return link
}
