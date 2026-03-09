package export

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"tg-rag-ingestor/backend/internal/cleaning"
	"tg-rag-ingestor/backend/internal/storage"
)

type Service struct {
	repo      *storage.Repository
	exportDir string
}

func NewService(repo *storage.Repository, exportDir string) *Service {
	if exportDir == "" {
		exportDir = "./data/exports"
	}
	return &Service{
		repo:      repo,
		exportDir: exportDir,
	}
}

type JSONLRequest struct {
	SourceID          *string `json:"source_id,omitempty"`
	Mode              string  `json:"mode,omitempty"`               // "documents" or "chunks"
	Format            string  `json:"format,omitempty"`             // "jsonl" or "txt_rag"
	IncludeDuplicates bool    `json:"include_duplicates,omitempty"` // include duplicate documents/chunks
	IncludeTrash      bool    `json:"include_trash,omitempty"`      // include trash messages (not processed into documents)
}

type JSONLResult struct {
	ExportID string `json:"export_id"`
	FilePath string `json:"file_path"`
	RowCount int    `json:"row_count"`
}

func (s *Service) ExportJSONL(ctx context.Context, req JSONLRequest) (JSONLResult, error) {
	mode := req.Mode
	if mode == "" {
		mode = "documents"
	}
	if mode != "documents" && mode != "chunks" {
		return JSONLResult{}, fmt.Errorf("unsupported export mode: %s", mode)
	}

	format := strings.ToLower(strings.TrimSpace(req.Format))
	if format == "" {
		format = "jsonl"
	}
	if format != "jsonl" && format != "txt_rag" {
		return JSONLResult{}, fmt.Errorf("unsupported export format: %s", format)
	}
	if format == "txt_rag" && mode != "documents" {
		return JSONLResult{}, fmt.Errorf("txt_rag format supports only documents mode")
	}

	record, err := s.repo.CreateExport(ctx, storage.CreateExportInput{
		ExportType: format + "_" + mode,
		SourceID:   req.SourceID,
		Status:     "running",
	})
	if err != nil {
		return JSONLResult{}, err
	}

	if err := os.MkdirAll(s.exportDir, 0o755); err != nil {
		_ = s.repo.FailExport(ctx, record.ID, err.Error())
		return JSONLResult{}, err
	}

	filePath := filepath.Join(s.exportDir, buildFilename(mode, req.SourceID, format))
	file, err := os.Create(filePath)
	if err != nil {
		_ = s.repo.FailExport(ctx, record.ID, err.Error())
		return JSONLResult{}, err
	}
	defer file.Close()

	writer := bufio.NewWriter(file)
	rows := 0

	switch format {
	case "jsonl":
		if mode == "documents" {
			documentRows, err := s.repo.ListExportDocuments(ctx, req.SourceID, req.IncludeDuplicates)
			if err != nil {
				_ = s.repo.FailExport(ctx, record.ID, err.Error())
				return JSONLResult{}, err
			}
			if req.IncludeTrash {
				trashRows, err := s.repo.ListExportTrashRawMessages(ctx, req.SourceID)
				if err != nil {
					_ = s.repo.FailExport(ctx, record.ID, err.Error())
					return JSONLResult{}, err
				}
				documentRows = append(documentRows, buildTrashDocumentRows(trashRows)...)
			}
			for _, row := range documentRows {
				entry := map[string]any{
					"doc_id": row.DocID,
					"text":   row.Text,
					"metadata": map[string]any{
						"source":           "telegram",
						"source_subtype":   "public_channel_post",
						"channel_username": row.ChannelUsername,
						"message_id":       row.MessageID,
						"url":              row.URL,
						"published_at":     row.PublishedAt,
						"language":         row.LanguageCode,
						"is_forward":       row.IsForward,
						"quality_score":    row.QualityScore,
					},
				}

				for key, value := range row.Metadata {
					entry["metadata"].(map[string]any)[key] = value
				}

				if err := writeJSONLLine(writer, entry); err != nil {
					_ = s.repo.FailExport(ctx, record.ID, err.Error())
					return JSONLResult{}, err
				}
				rows++
			}
		} else {
			chunkRows, err := s.repo.ListExportChunks(ctx, req.SourceID, req.IncludeDuplicates)
			if err != nil {
				_ = s.repo.FailExport(ctx, record.ID, err.Error())
				return JSONLResult{}, err
			}
			if req.IncludeTrash {
				trashRows, err := s.repo.ListExportTrashRawMessages(ctx, req.SourceID)
				if err != nil {
					_ = s.repo.FailExport(ctx, record.ID, err.Error())
					return JSONLResult{}, err
				}
				chunkRows = append(chunkRows, buildTrashChunkRows(buildTrashDocumentRows(trashRows))...)
			}
			for _, row := range chunkRows {
				entry := map[string]any{
					"doc_id": row.DocID,
					"text":   row.Text,
					"metadata": map[string]any{
						"source":           "telegram",
						"source_subtype":   "public_channel_post_chunk",
						"channel_username": row.ChannelUsername,
						"message_id":       row.MessageID,
						"url":              row.URL,
						"published_at":     row.PublishedAt,
						"language":         row.LanguageCode,
						"chunk_index":      row.ChunkIndex,
					},
				}
				if err := writeJSONLLine(writer, entry); err != nil {
					_ = s.repo.FailExport(ctx, record.ID, err.Error())
					return JSONLResult{}, err
				}
				rows++
			}
		}
	case "txt_rag":
		documentRows, err := s.repo.ListExportDocuments(ctx, req.SourceID, req.IncludeDuplicates)
		if err != nil {
			_ = s.repo.FailExport(ctx, record.ID, err.Error())
			return JSONLResult{}, err
		}
		if req.IncludeTrash {
			trashRows, err := s.repo.ListExportTrashRawMessages(ctx, req.SourceID)
			if err != nil {
				_ = s.repo.FailExport(ctx, record.ID, err.Error())
				return JSONLResult{}, err
			}
			documentRows = append(documentRows, buildTrashDocumentRows(trashRows)...)
		}
		for _, row := range documentRows {
			if err := writeRAGTextDocument(writer, row); err != nil {
				_ = s.repo.FailExport(ctx, record.ID, err.Error())
				return JSONLResult{}, err
			}
			rows++
		}
	}

	if err := writer.Flush(); err != nil {
		_ = s.repo.FailExport(ctx, record.ID, err.Error())
		return JSONLResult{}, err
	}

	if err := s.repo.CompleteExport(ctx, record.ID, filePath, rows); err != nil {
		return JSONLResult{}, err
	}

	return JSONLResult{
		ExportID: record.ID,
		FilePath: filePath,
		RowCount: rows,
	}, nil
}

func buildFilename(mode string, sourceID *string, format string) string {
	sourcePart := "all"
	if sourceID != nil && *sourceID != "" {
		sourcePart = *sourceID
	}
	ts := time.Now().UTC().Format("20060102_150405")
	extension := ".jsonl"
	if format == "txt_rag" {
		extension = ".txt"
	}
	return fmt.Sprintf("%s_%s_%s%s", mode, sourcePart, ts, extension)
}

func writeJSONLLine(writer *bufio.Writer, value any) error {
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}
	if _, err := writer.Write(data); err != nil {
		return err
	}
	if err := writer.WriteByte('\n'); err != nil {
		return err
	}
	return nil
}

func writeRAGTextDocument(writer *bufio.Writer, row storage.ExportDocumentRow) error {
	text := normalizeRAGText(row.Text)
	sourceURL := strings.TrimSpace(row.URL)
	publishedAt := ""
	if row.PublishedAt != nil {
		publishedAt = row.PublishedAt.UTC().Format(time.RFC3339)
	}

	if _, err := writer.WriteString("<<<RAG_DOCUMENT>>>\n"); err != nil {
		return err
	}
	if publishedAt != "" {
		if _, err := fmt.Fprintf(writer, "published_at: %s\n", publishedAt); err != nil {
			return err
		}
	}
	if sourceURL != "" {
		if _, err := fmt.Fprintf(writer, "source_url: %s\n", sourceURL); err != nil {
			return err
		}
	}
	if _, err := writer.WriteString("---\n"); err != nil {
		return err
	}
	if _, err := writer.WriteString(text); err != nil {
		return err
	}
	if _, err := writer.WriteString("\n<<<END_RAG_DOCUMENT>>>\n\n"); err != nil {
		return err
	}
	return nil
}

func normalizeRAGText(input string) string {
	text := stripEmoji(input)
	lines := strings.Split(text, "\n")
	for i, line := range lines {
		lines[i] = strings.Join(strings.Fields(line), " ")
	}
	text = strings.TrimSpace(strings.Join(lines, "\n"))
	for strings.Contains(text, "\n\n\n") {
		text = strings.ReplaceAll(text, "\n\n\n", "\n\n")
	}
	return text
}

func stripEmoji(input string) string {
	if input == "" {
		return input
	}
	var builder strings.Builder
	builder.Grow(len(input))
	for _, r := range input {
		if isEmojiRune(r) {
			continue
		}
		builder.WriteRune(r)
	}
	return builder.String()
}

func isEmojiRune(r rune) bool {
	switch {
	case r >= 0x1F300 && r <= 0x1FAFF:
		return true
	case r >= 0x1F1E6 && r <= 0x1F1FF:
		return true
	case r >= 0x2600 && r <= 0x27BF:
		return true
	case r >= 0x1F3FB && r <= 0x1F3FF:
		return true
	case r >= 0xFE00 && r <= 0xFE0F:
		return true
	case r >= 0xE0020 && r <= 0xE007F:
		return true
	case r == 0x200D:
		return true
	case r == 0x20E3:
		return true
	default:
		return false
	}
}

func buildTrashDocumentRows(rows []storage.ExportTrashRawRow) []storage.ExportDocumentRow {
	out := make([]storage.ExportDocumentRow, 0, len(rows))
	for _, row := range rows {
		text := strings.TrimSpace(cleaning.NormalizeTextBody(row.TextRaw, row.CaptionRaw))
		if text == "" {
			continue
		}

		url := firstNonEmptyStringFromMap(row.RawJSON, "url", "link", "source_url", "href")
		if url == "" {
			url = strings.TrimSpace(row.SourceURL)
		}
		language := detectLanguage(text)

		out = append(out, storage.ExportDocumentRow{
			DocID:           buildTrashDocID(row),
			Text:            text,
			ChannelUsername: row.ChannelUsername,
			MessageID:       row.MessageID,
			URL:             url,
			PublishedAt:     row.PublishedAt,
			LanguageCode:    &language,
			IsForward:       row.IsForward,
			Metadata: map[string]any{
				"is_trash": true,
			},
		})
	}
	return out
}

func buildTrashChunkRows(rows []storage.ExportDocumentRow) []storage.ExportChunkRow {
	out := make([]storage.ExportChunkRow, 0, len(rows))
	for _, row := range rows {
		text := strings.TrimSpace(row.Text)
		if text == "" {
			continue
		}
		out = append(out, storage.ExportChunkRow{
			DocID:           row.DocID,
			ChunkIndex:      0,
			Text:            text,
			ChannelUsername: row.ChannelUsername,
			MessageID:       row.MessageID,
			URL:             row.URL,
			PublishedAt:     row.PublishedAt,
			LanguageCode:    row.LanguageCode,
		})
	}
	return out
}

func buildTrashDocID(row storage.ExportTrashRawRow) string {
	owner := strings.TrimSpace(row.ChannelUsername)
	if owner == "" {
		owner = strings.TrimSpace(row.SourceID)
	}
	if owner == "" {
		owner = "unknown"
	}
	return fmt.Sprintf("trash:%s:%d", owner, row.MessageID)
}

func firstNonEmptyStringFromMap(input map[string]any, keys ...string) string {
	for _, key := range keys {
		raw, ok := input[key]
		if !ok || raw == nil {
			continue
		}
		value, ok := raw.(string)
		if !ok {
			continue
		}
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func detectLanguage(input string) string {
	for _, r := range input {
		if (r >= 'а' && r <= 'я') || (r >= 'А' && r <= 'Я') {
			return "ru"
		}
	}
	return "en"
}
