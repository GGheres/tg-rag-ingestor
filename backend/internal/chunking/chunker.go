package chunking

import "strings"

type Chunk struct {
	Index      int            `json:"index"`
	Text       string         `json:"text"`
	CharCount  int            `json:"char_count"`
	TokenCount int            `json:"token_count"`
	Metadata   map[string]any `json:"metadata,omitempty"`
}

type Config struct {
	TargetChars   int
	OverlapChars  int
	MinChunkChars int
}

func DefaultConfig() Config {
	return Config{
		TargetChars:   1000,
		OverlapChars:  100,
		MinChunkChars: 200,
	}
}

func Split(text string, targetChars int, overlapChars int) []Chunk {
	cfg := DefaultConfig()
	if targetChars > 0 {
		cfg.TargetChars = targetChars
	}
	if overlapChars >= 0 {
		cfg.OverlapChars = overlapChars
	}
	return SplitWithConfig(text, cfg)
}

func SplitWithConfig(text string, cfg Config) []Chunk {
	text = strings.TrimSpace(text)
	if text == "" {
		return nil
	}

	if cfg.TargetChars <= 0 {
		cfg.TargetChars = 1000
	}
	if cfg.MinChunkChars <= 0 {
		cfg.MinChunkChars = 200
	}
	if cfg.OverlapChars < 0 {
		cfg.OverlapChars = 0
	}

	paragraphs := splitParagraphs(text)
	if len(paragraphs) == 0 {
		return nil
	}

	var chunks []Chunk
	var buf strings.Builder
	index := 0

	flush := func(force bool) {
		content := strings.TrimSpace(buf.String())
		if content == "" {
			return
		}
		if !force && len(content) < cfg.MinChunkChars && len(chunks) > 0 {
			prev := chunks[len(chunks)-1]
			merged := prev.Text + "\n\n" + content
			chunks[len(chunks)-1] = newChunk(prev.Index, merged)
			buf.Reset()
			return
		}

		chunks = append(chunks, newChunk(index, content))
		index++
		buf.Reset()
	}

	for _, p := range paragraphs {
		if buf.Len() == 0 {
			buf.WriteString(p)
			continue
		}

		candidateLen := buf.Len() + 2 + len(p)
		if candidateLen <= cfg.TargetChars {
			buf.WriteString("\n\n")
			buf.WriteString(p)
			continue
		}

		prevText := strings.TrimSpace(buf.String())
		flush(false)
		overlap := tail(prevText, cfg.OverlapChars)
		if overlap != "" {
			buf.WriteString(overlap)
			buf.WriteString("\n\n")
		}
		buf.WriteString(p)
	}

	flush(true)
	return chunks
}

func splitParagraphs(text string) []string {
	parts := strings.Split(text, "\n\n")
	if len(parts) == 1 {
		parts = strings.Split(text, "\n")
	}

	out := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		out = append(out, part)
	}
	return out
}

func tail(s string, max int) string {
	if max <= 0 || len(s) <= max {
		return s
	}
	return s[len(s)-max:]
}

func newChunk(index int, text string) Chunk {
	text = strings.TrimSpace(text)
	charCount := len(text)
	return Chunk{
		Index:      index,
		Text:       text,
		CharCount:  charCount,
		TokenCount: estimateTokens(text),
	}
}

func estimateTokens(text string) int {
	if text == "" {
		return 0
	}
	return len(strings.Fields(text))
}

func SplitLegacy(text string, targetChars int, overlapChars int) []Chunk {
	if len(text) <= targetChars {
		return []Chunk{{Index: 0, Text: text, CharCount: len(text), TokenCount: estimateTokens(text)}}
	}

	paragraphs := strings.Split(text, "\n")
	var chunks []Chunk
	var buf strings.Builder
	index := 0

	for _, p := range paragraphs {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}

		if buf.Len()+len(p)+1 > targetChars && buf.Len() > 0 {
			textChunk := strings.TrimSpace(buf.String())
			chunks = append(chunks, Chunk{
				Index:      index,
				Text:       textChunk,
				CharCount:  len(textChunk),
				TokenCount: estimateTokens(textChunk),
			})
			index++

			prev := buf.String()
			if overlapChars > 0 && len(prev) > overlapChars {
				prev = prev[len(prev)-overlapChars:]
			}
			buf.Reset()
			if prev != "" {
				buf.WriteString(prev)
				buf.WriteString("\n")
			}
		}

		buf.WriteString(p)
		buf.WriteString("\n")
	}

	if strings.TrimSpace(buf.String()) != "" {
		textChunk := strings.TrimSpace(buf.String())
		chunks = append(chunks, Chunk{
			Index:      index,
			Text:       textChunk,
			CharCount:  len(textChunk),
			TokenCount: estimateTokens(textChunk),
		})
	}

	return chunks
}
