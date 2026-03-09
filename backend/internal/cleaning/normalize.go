package cleaning

import (
	"regexp"
	"strings"
)

var (
	multiSpace       = regexp.MustCompile(`[ \t]+`)
	multiNewline     = regexp.MustCompile(`\n{3,}`)
	decorativeEmojis = regexp.MustCompile(`[🔥⭐️🚀✨💥🎉]{3,}`)
)

func NormalizeTextBody(textRaw, captionRaw string) string {
	parts := make([]string, 0, 2)
	if strings.TrimSpace(textRaw) != "" {
		parts = append(parts, textRaw)
	}
	if strings.TrimSpace(captionRaw) != "" {
		parts = append(parts, captionRaw)
	}
	return NormalizeText(strings.Join(parts, "\n\n"))
}

func NormalizeText(input string) string {
	s := strings.ReplaceAll(input, "\r\n", "\n")
	s = strings.ReplaceAll(s, "\r", "\n")
	s = strings.ReplaceAll(s, "\u00A0", " ")
	s = strings.TrimSpace(s)
	s = decorativeEmojis.ReplaceAllString(s, " ")
	s = strings.ReplaceAll(s, "\u00A0", " ")
	lines := strings.Split(s, "\n")
	for i, line := range lines {
		lines[i] = strings.TrimSpace(multiSpace.ReplaceAllString(line, " "))
	}
	s = strings.Join(lines, "\n")
	s = multiNewline.ReplaceAllString(s, "\n\n")
	return strings.TrimSpace(s)
}
