package youtube

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"tg-rag-ingestor/backend/internal/chunking"
	"tg-rag-ingestor/backend/internal/cleaning"
	"tg-rag-ingestor/backend/internal/models"
	"tg-rag-ingestor/backend/internal/storage"
)

const (
	YouTubeSourceType   = "youtube_video"
	YouTubeProvider     = "yt_dlp"
	AudioUploadType     = "audio_upload"
	AudioUploadProvider = "deepgram"
)

type Service struct {
	repo     *storage.Repository
	provider AudioProvider
}

func NewService(repo *storage.Repository, provider AudioProvider) *Service {
	return &Service{
		repo:     repo,
		provider: provider,
	}
}

type CreateYouTubeSourceInput struct {
	URL   string
	Title *string
}

func (s *Service) CreateYouTubeSource(ctx context.Context, input CreateYouTubeSourceInput) (models.Source, error) {
	canonicalURL, videoID, err := NormalizeURL(input.URL)
	if err != nil {
		return models.Source{}, err
	}

	existing, err := s.repo.GetSourceByURL(ctx, canonicalURL)
	if err == nil {
		if existing.SourceType != YouTubeSourceType {
			return models.Source{}, fmt.Errorf("source with this url already exists as type %s", existing.SourceType)
		}
		return existing, nil
	}
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return models.Source{}, err
	}

	provider := YouTubeProvider
	source, err := s.repo.CreateSource(ctx, storage.CreateSourceInput{
		SourceType: YouTubeSourceType,
		Provider:   &provider,
		ExternalID: &videoID,
		Title:      sanitizeOptionalTitle(input.Title),
		URL:        canonicalURL,
	})
	if err != nil {
		if isSourceURLConflict(err) {
			existing, lookupErr := s.repo.GetSourceByURL(ctx, canonicalURL)
			if lookupErr == nil {
				if existing.SourceType != YouTubeSourceType {
					return models.Source{}, fmt.Errorf("source with this url already exists as type %s", existing.SourceType)
				}
				return existing, nil
			}
		}
		return models.Source{}, err
	}
	return source, nil
}

func (s *Service) ListYouTubeSources(ctx context.Context) ([]models.Source, error) {
	return s.repo.ListSourcesByType(ctx, YouTubeSourceType)
}

func (s *Service) GetYouTubeSource(ctx context.Context, sourceID string) (models.Source, error) {
	source, err := s.repo.GetSource(ctx, sourceID)
	if err != nil {
		return models.Source{}, err
	}
	if source.SourceType != YouTubeSourceType {
		return models.Source{}, fmt.Errorf("source %s is not a youtube_video source", sourceID)
	}
	return source, nil
}

type DownloadSourceAudioResult struct {
	Source   models.Source               `json:"source"`
	Artifact models.YouTubeAudioArtifact `json:"artifact"`
}

type SourceAudioDetails struct {
	Source   models.Source                `json:"source"`
	Artifact *models.YouTubeAudioArtifact `json:"artifact,omitempty"`
}

type transcribeStoredAudioInput struct {
	Source           models.Source
	VideoID          string
	AudioPath        string
	DownloadMetadata map[string]any
	DownloadTitle    *string
	BaseRawJSON      map[string]any
}

func (s *Service) GetSourceAudio(ctx context.Context, sourceID string) (SourceAudioDetails, error) {
	source, err := s.GetYouTubeSource(ctx, sourceID)
	if err != nil {
		return SourceAudioDetails{}, err
	}
	artifact, err := s.repo.GetYouTubeAudioArtifactBySource(ctx, source.ID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return SourceAudioDetails{Source: source}, nil
		}
		return SourceAudioDetails{}, err
	}
	return SourceAudioDetails{Source: source, Artifact: &artifact}, nil
}

func (s *Service) TranscribeSourceAudio(ctx context.Context, sourceID string) (DownloadSourceAudioResult, error) {
	source, err := s.GetYouTubeSource(ctx, sourceID)
	if err != nil {
		return DownloadSourceAudioResult{}, err
	}
	if s.provider == nil {
		return DownloadSourceAudioResult{}, fmt.Errorf("youtube audio provider is not configured")
	}

	artifact, err := s.repo.GetYouTubeAudioArtifactBySource(ctx, source.ID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return DownloadSourceAudioResult{}, fmt.Errorf("download audio first: artifact not found")
		}
		return DownloadSourceAudioResult{}, err
	}

	if artifact.AudioFilePath == nil || strings.TrimSpace(*artifact.AudioFilePath) == "" {
		return DownloadSourceAudioResult{}, fmt.Errorf("download audio first: audio_file_path is empty")
	}
	audioPath := strings.TrimSpace(*artifact.AudioFilePath)

	videoID := strings.TrimSpace(artifact.VideoID)
	if videoID == "" {
		videoID = externalIDOrEmpty(source.ExternalID)
	}
	if videoID == "" {
		if _, extractedID, parseErr := NormalizeURL(source.URL); parseErr == nil {
			videoID = extractedID
		}
	}
	if videoID == "" {
		return DownloadSourceAudioResult{}, fmt.Errorf("video_id is empty and cannot be derived")
	}

	if err := s.repo.UpdateSourceStatus(ctx, source.ID, "running", nil); err != nil {
		return DownloadSourceAudioResult{}, err
	}

	baseRawJSON := cloneAnyMap(artifact.RawJSON)
	downloadMetadata := mapFromAny(baseRawJSON["download_metadata"])
	downloadTitle := optionalStringFromAny(baseRawJSON["download_title"])

	updatedArtifact, err := s.runTranscriptionForStoredAudio(ctx, transcribeStoredAudioInput{
		Source:           source,
		VideoID:          videoID,
		AudioPath:        audioPath,
		DownloadMetadata: downloadMetadata,
		DownloadTitle:    downloadTitle,
		BaseRawJSON:      baseRawJSON,
	})
	if err != nil {
		return DownloadSourceAudioResult{}, err
	}

	updatedSource, err := s.repo.GetSource(ctx, source.ID)
	if err != nil {
		return DownloadSourceAudioResult{}, err
	}
	return DownloadSourceAudioResult{
		Source:   updatedSource,
		Artifact: updatedArtifact,
	}, nil
}

func (s *Service) DownloadSourceAudio(ctx context.Context, sourceID string) (DownloadSourceAudioResult, error) {
	source, err := s.GetYouTubeSource(ctx, sourceID)
	if err != nil {
		return DownloadSourceAudioResult{}, err
	}
	if s.provider == nil {
		return DownloadSourceAudioResult{}, fmt.Errorf("youtube audio provider is not configured")
	}

	if err := s.repo.UpdateSourceStatus(ctx, source.ID, "running", nil); err != nil {
		return DownloadSourceAudioResult{}, err
	}

	fetchJob, err := s.repo.CreateJob(ctx, storage.CreateJobInput{
		JobType:  "fetch_youtube_audio",
		SourceID: &source.ID,
		Status:   "queued",
		Payload: map[string]any{
			"url": source.URL,
		},
	})
	if err != nil {
		return DownloadSourceAudioResult{}, err
	}
	_ = s.repo.UpdateJobRunning(ctx, fetchJob.ID)

	providerResult, err := s.provider.DownloadYouTubeAudio(ctx, DownloadAudioRequest{
		SourceID: source.ID,
		URL:      source.URL,
	})
	if err != nil {
		if errors.Is(ctx.Err(), context.Canceled) {
			return DownloadSourceAudioResult{}, ctx.Err()
		}
		errorText, errorPayload := audioErrorPayload(err)
		_ = s.repo.FailJob(ctx, fetchJob.ID, errorText, errorPayload)
		_ = s.repo.UpdateSourceStatus(ctx, source.ID, "error", &errorText)

		videoID := externalIDOrEmpty(source.ExternalID)
		if videoID == "" {
			if _, extractedID, parseErr := NormalizeURL(source.URL); parseErr == nil {
				videoID = extractedID
			}
		}

		_, _ = s.repo.SaveYouTubeAudioArtifact(ctx, storage.SaveYouTubeAudioArtifactInput{
			SourceID:    source.ID,
			Provider:    providerOrDefault(source.Provider),
			VideoID:     videoID,
			AudioStatus: "download_failed",
			RawJSON: map[string]any{
				"download_error": errorPayload,
			},
			ErrorText: &errorText,
		})
		return DownloadSourceAudioResult{}, err
	}

	videoID := strings.TrimSpace(providerResult.VideoID)
	if videoID == "" {
		videoID = externalIDOrEmpty(source.ExternalID)
	}
	if videoID == "" {
		if _, extractedID, parseErr := NormalizeURL(source.URL); parseErr == nil {
			videoID = extractedID
		}
	}
	if videoID == "" {
		err = fmt.Errorf("download provider did not return video_id")
		_ = s.repo.FailJob(ctx, fetchJob.ID, err.Error(), map[string]any{"error": err.Error()})
		errorText := err.Error()
		_ = s.repo.UpdateSourceStatus(ctx, source.ID, "error", &errorText)
		return DownloadSourceAudioResult{}, err
	}

	audioPath := strings.TrimSpace(providerResult.AudioFilePath)
	if audioPath == "" {
		err = fmt.Errorf("download provider did not return audio_file_path")
		_ = s.repo.FailJob(ctx, fetchJob.ID, err.Error(), map[string]any{"error": err.Error()})
		errorText := err.Error()
		_ = s.repo.UpdateSourceStatus(ctx, source.ID, "error", &errorText)
		return DownloadSourceAudioResult{}, err
	}

	audioPathPtr := audioPath
	downloadRawJSON := map[string]any{
		"download_metadata": providerResult.Metadata,
		"download_title":    providerResult.Title,
	}
	artifact, err := s.repo.SaveYouTubeAudioArtifact(ctx, storage.SaveYouTubeAudioArtifactInput{
		SourceID:      source.ID,
		Provider:      providerOrDefault(source.Provider),
		VideoID:       videoID,
		AudioFilePath: &audioPathPtr,
		AudioStatus:   "audio_downloaded",
		RawJSON:       downloadRawJSON,
	})
	if err != nil {
		_ = s.repo.FailJob(ctx, fetchJob.ID, err.Error(), map[string]any{"error": err.Error()})
		errorText := err.Error()
		_ = s.repo.UpdateSourceStatus(ctx, source.ID, "error", &errorText)
		return DownloadSourceAudioResult{}, err
	}
	if providerResult.Title != nil {
		_ = s.repo.UpdateSourceTitleIfEmpty(ctx, source.ID, strings.TrimSpace(*providerResult.Title))
		if source.Title == nil || strings.TrimSpace(*source.Title) == "" {
			source.Title = sanitizeOptionalTitle(providerResult.Title)
		}
	}

	_ = s.repo.CompleteJobDone(ctx, fetchJob.ID, map[string]any{
		"stage":           "download_audio",
		"video_id":        videoID,
		"audio_file_path": audioPath,
	})

	artifact, err = s.runTranscriptionForStoredAudio(ctx, transcribeStoredAudioInput{
		Source:           source,
		VideoID:          videoID,
		AudioPath:        audioPath,
		DownloadMetadata: providerResult.Metadata,
		DownloadTitle:    providerResult.Title,
		BaseRawJSON:      downloadRawJSON,
	})
	if err != nil {
		return DownloadSourceAudioResult{}, err
	}

	updatedSource, err := s.repo.GetSource(ctx, source.ID)
	if err != nil {
		return DownloadSourceAudioResult{}, err
	}

	return DownloadSourceAudioResult{
		Source:   updatedSource,
		Artifact: artifact,
	}, nil
}

func (s *Service) runTranscriptionForStoredAudio(ctx context.Context, input transcribeStoredAudioInput) (models.YouTubeAudioArtifact, error) {
	pipelineStartedAt := time.Now()
	source := input.Source
	videoID := strings.TrimSpace(input.VideoID)
	audioPath := strings.TrimSpace(input.AudioPath)
	audioPathPtr := audioPath

	baseRawJSON := cloneAnyMap(input.BaseRawJSON)
	baseRawJSON["download_metadata"] = input.DownloadMetadata
	baseRawJSON["download_title"] = input.DownloadTitle
	baseRawJSON["transcription_started_at"] = pipelineStartedAt.UTC().Format(time.RFC3339)

	transcribeJob, err := s.repo.CreateJob(ctx, storage.CreateJobInput{
		JobType:  "transcribe_youtube_audio",
		SourceID: &source.ID,
		Status:   "queued",
		Payload: map[string]any{
			"audio_file_path": audioPath,
			"video_id":        videoID,
			"existing_audio":  true,
		},
	})
	if err != nil {
		errorText := err.Error()
		_ = s.repo.UpdateSourceStatus(ctx, source.ID, "error", &errorText)
		return models.YouTubeAudioArtifact{}, err
	}
	_ = s.repo.UpdateJobRunning(ctx, transcribeJob.ID)

	_, _ = s.repo.SaveYouTubeAudioArtifact(ctx, storage.SaveYouTubeAudioArtifactInput{
		SourceID:      source.ID,
		Provider:      providerOrDefault(source.Provider),
		VideoID:       videoID,
		AudioFilePath: &audioPathPtr,
		AudioStatus:   "transcribing",
		RawJSON:       baseRawJSON,
	})

	transcribeResult, err := s.provider.TranscribeYouTubeAudio(ctx, TranscribeAudioRequest{
		SourceID:      source.ID,
		VideoID:       videoID,
		AudioFilePath: audioPath,
	})
	if err != nil {
		if errors.Is(ctx.Err(), context.Canceled) {
			return models.YouTubeAudioArtifact{}, ctx.Err()
		}
		errorText, errorPayload := audioErrorPayload(err)
		_ = s.repo.FailJob(ctx, transcribeJob.ID, errorText, errorPayload)
		_ = s.repo.UpdateSourceStatus(ctx, source.ID, "error", &errorText)

		failedRawJSON := cloneAnyMap(baseRawJSON)
		failedRawJSON["transcription_error"] = errorPayload
		failedRawJSON["timing_seconds"] = buildTimingSummary(input.DownloadMetadata, nil, pipelineStartedAt)

		_, _ = s.repo.SaveYouTubeAudioArtifact(ctx, storage.SaveYouTubeAudioArtifactInput{
			SourceID:      source.ID,
			Provider:      providerOrDefault(source.Provider),
			VideoID:       videoID,
			AudioFilePath: &audioPathPtr,
			AudioStatus:   "transcription_failed",
			RawJSON:       failedRawJSON,
			ErrorText:     &errorText,
		})
		return models.YouTubeAudioArtifact{}, err
	}

	docTitle := source.Title
	if (docTitle == nil || strings.TrimSpace(*docTitle) == "") && input.DownloadTitle != nil {
		docTitle = sanitizeOptionalTitle(input.DownloadTitle)
		if docTitle != nil {
			_ = s.repo.UpdateSourceTitleIfEmpty(ctx, source.ID, strings.TrimSpace(*docTitle))
			source.Title = docTitle
		}
	}

	document, chunkCount, err := s.saveTranscriptDocument(
		ctx,
		source,
		videoID,
		docTitle,
		transcribeResult,
	)
	if err != nil {
		errorText := err.Error()
		_ = s.repo.FailJob(ctx, transcribeJob.ID, errorText, map[string]any{"error": errorText})
		_ = s.repo.UpdateSourceStatus(ctx, source.ID, "error", &errorText)

		failedRawJSON := cloneAnyMap(baseRawJSON)
		failedRawJSON["transcription"] = map[string]any{
			"provider": transcribeResult.Provider,
			"model":    transcribeResult.Model,
			"language": transcribeResult.Language,
		}
		failedRawJSON["processing_error"] = errorText
		failedRawJSON["timing_seconds"] = buildTimingSummary(input.DownloadMetadata, transcribeResult.Metadata, pipelineStartedAt)

		_, _ = s.repo.SaveYouTubeAudioArtifact(ctx, storage.SaveYouTubeAudioArtifactInput{
			SourceID:      source.ID,
			Provider:      providerOrDefault(source.Provider),
			VideoID:       videoID,
			AudioFilePath: &audioPathPtr,
			AudioStatus:   "transcription_failed",
			RawJSON:       failedRawJSON,
			ErrorText:     &errorText,
		})
		return models.YouTubeAudioArtifact{}, err
	}

	finalRawJSON := cloneAnyMap(baseRawJSON)
	finalRawJSON["transcription_metadata"] = transcribeResult.Metadata
	finalRawJSON["speaker_roles"] = transcribeResult.SpeakerRoles
	finalRawJSON["segments"] = transcribeResult.Segments
	finalRawJSON["full_text_raw"] = transcribeResult.FullTextRaw
	finalRawJSON["full_text_rag"] = transcribeResult.FullTextRAG
	finalRawJSON["document_id"] = document.ID
	finalRawJSON["chunk_count"] = chunkCount
	finalRawJSON["timing_seconds"] = buildTimingSummary(input.DownloadMetadata, transcribeResult.Metadata, pipelineStartedAt)

	artifact, err := s.repo.SaveYouTubeAudioArtifact(ctx, storage.SaveYouTubeAudioArtifactInput{
		SourceID:      source.ID,
		Provider:      providerOrDefault(source.Provider),
		VideoID:       videoID,
		AudioFilePath: &audioPathPtr,
		AudioStatus:   "transcribed",
		RawJSON:       finalRawJSON,
	})
	if err != nil {
		_ = s.repo.FailJob(ctx, transcribeJob.ID, err.Error(), map[string]any{"error": err.Error()})
		errorText := err.Error()
		_ = s.repo.UpdateSourceStatus(ctx, source.ID, "error", &errorText)
		return models.YouTubeAudioArtifact{}, err
	}

	_ = s.repo.CompleteJobDone(ctx, transcribeJob.ID, map[string]any{
		"stage":           "transcribe_audio",
		"video_id":        videoID,
		"audio_file_path": audioPath,
		"document_id":     document.ID,
		"chunk_count":     chunkCount,
		"timing_seconds":  finalRawJSON["timing_seconds"],
	})

	if err := s.repo.UpdateSourceStatus(ctx, source.ID, "active", nil); err != nil {
		return models.YouTubeAudioArtifact{}, err
	}
	return artifact, nil
}

type UploadAudioInput struct {
	FileName string
	FileData []byte
	Title    *string
	Language *string
	Speakers *int
}

type UploadAudioResult struct {
	Source       models.Source               `json:"source"`
	Artifact     models.YouTubeAudioArtifact `json:"artifact"`
	DocumentID   string                      `json:"document_id"`
	ChunkCount   int                         `json:"chunk_count"`
	SpeakerRoles map[string]string           `json:"speaker_roles"`
}

func (s *Service) UploadAndTranscribeAudio(ctx context.Context, input UploadAudioInput) (UploadAudioResult, error) {
	if s.provider == nil {
		return UploadAudioResult{}, fmt.Errorf("audio provider is not configured")
	}

	title := sanitizeOptionalTitle(input.Title)
	if title == nil {
		t := strings.TrimSpace(input.FileName)
		title = &t
	}

	provider := AudioUploadProvider
	uniqueID := fmt.Sprintf("%d", time.Now().UnixNano())
	source, err := s.repo.CreateSource(ctx, storage.CreateSourceInput{
		SourceType: AudioUploadType,
		Provider:   &provider,
		ExternalID: &uniqueID,
		Title:      title,
		URL:        fmt.Sprintf("upload://%s/%s", uniqueID, strings.TrimSpace(input.FileName)),
	})
	if err != nil {
		return UploadAudioResult{}, fmt.Errorf("create audio upload source: %w", err)
	}

	if err := s.repo.UpdateSourceStatus(ctx, source.ID, "running", nil); err != nil {
		return UploadAudioResult{}, err
	}

	transcribeResult, err := s.provider.UploadAndTranscribeAudio(ctx, UploadAndTranscribeRequest{
		SourceID: source.ID,
		FileName: input.FileName,
		FileData: input.FileData,
		Language: input.Language,
		Speakers: input.Speakers,
	})
	if err != nil {
		errorText := err.Error()
		_ = s.repo.UpdateSourceStatus(ctx, source.ID, "error", &errorText)
		return UploadAudioResult{}, err
	}

	audioPath := transcribeResult.AudioPath
	rawJSON := map[string]any{
		"filename":               transcribeResult.FileName,
		"audio_file_path":        audioPath,
		"transcription_metadata": transcribeResult.Metadata,
		"speaker_roles":          transcribeResult.SpeakerRoles,
		"segments":               transcribeResult.Segments,
		"full_text_raw":          transcribeResult.FullTextRaw,
		"full_text_rag":          transcribeResult.FullTextRAG,
	}

	artifact, err := s.repo.SaveYouTubeAudioArtifact(ctx, storage.SaveYouTubeAudioArtifactInput{
		SourceID:      source.ID,
		Provider:      AudioUploadProvider,
		VideoID:       source.ID,
		AudioFilePath: &audioPath,
		AudioStatus:   "transcribed",
		RawJSON:       rawJSON,
	})
	if err != nil {
		errorText := err.Error()
		_ = s.repo.UpdateSourceStatus(ctx, source.ID, "error", &errorText)
		return UploadAudioResult{}, err
	}

	transcript := TranscribeAudioResult{
		SourceID:     transcribeResult.SourceID,
		Provider:     transcribeResult.Provider,
		Model:        transcribeResult.Model,
		Language:     transcribeResult.Language,
		FullTextRaw:  transcribeResult.FullTextRaw,
		FullTextRAG:  transcribeResult.FullTextRAG,
		SpeakerRoles: transcribeResult.SpeakerRoles,
		Segments:     transcribeResult.Segments,
		Metadata:     transcribeResult.Metadata,
	}

	document, chunkCount, err := s.saveTranscriptDocument(ctx, source, source.ID, title, transcript)
	if err != nil {
		errorText := err.Error()
		_ = s.repo.UpdateSourceStatus(ctx, source.ID, "error", &errorText)
		return UploadAudioResult{}, err
	}

	rawJSON["document_id"] = document.ID
	rawJSON["chunk_count"] = chunkCount
	artifact, _ = s.repo.SaveYouTubeAudioArtifact(ctx, storage.SaveYouTubeAudioArtifactInput{
		SourceID:      source.ID,
		Provider:      AudioUploadProvider,
		VideoID:       source.ID,
		AudioFilePath: &audioPath,
		AudioStatus:   "transcribed",
		RawJSON:       rawJSON,
	})

	if err := s.repo.UpdateSourceStatus(ctx, source.ID, "active", nil); err != nil {
		return UploadAudioResult{}, err
	}

	updatedSource, _ := s.repo.GetSource(ctx, source.ID)

	return UploadAudioResult{
		Source:       updatedSource,
		Artifact:     artifact,
		DocumentID:   document.ID,
		ChunkCount:   chunkCount,
		SpeakerRoles: transcribeResult.SpeakerRoles,
	}, nil
}

func (s *Service) saveTranscriptDocument(
	ctx context.Context,
	source models.Source,
	videoID string,
	title *string,
	transcript TranscribeAudioResult,
) (models.Document, int, error) {
	textForRAG := strings.TrimSpace(transcript.FullTextRAG)
	if textForRAG == "" {
		textForRAG = strings.TrimSpace(transcript.FullTextRaw)
	}
	if textForRAG == "" {
		return models.Document{}, 0, fmt.Errorf("transcription returned empty text")
	}

	contentHash := cleaning.ContentHash(textForRAG)
	simhash := cleaning.SimhashPlaceholder(textForRAG)
	quality := 0.92
	externalDocID := fmt.Sprintf("yt:%s:%s", source.ID, videoID)

	metadata := map[string]any{
		"source":              "youtube",
		"source_subtype":      "pre_recorded_audio_transcript",
		"url":                 source.URL,
		"video_id":            videoID,
		"provider":            transcript.Provider,
		"transcription_model": transcript.Model,
		"speaker_roles":       transcript.SpeakerRoles,
		"segments_count":      len(transcript.Segments),
		"language":            transcript.Language,
	}

	languageCode := transcript.Language
	document, err := s.repo.SaveDocument(ctx, storage.SaveDocumentInput{
		SourceID:      source.ID,
		RawMessageID:  nil,
		ExternalDocID: externalDocID,
		Title:         title,
		TextClean:     textForRAG,
		TextOriginal:  ptrIfNotEmpty(transcript.FullTextRaw),
		LanguageCode:  languageCode,
		ContentHash:   contentHash,
		Simhash:       &simhash,
		DedupeGroup:   ptrIfNotEmpty(contentHash[:12]),
		QualityScore:  &quality,
		IsDuplicate:   false,
		DuplicateOf:   nil,
		IsForward:     false,
		ForwardSource: nil,
		Tags:          []string{"youtube", "audio", "transcript", "deepgram"},
		Links:         []string{source.URL},
		Metadata:      metadata,
		PublishedAt:   nil,
	})
	if err != nil {
		return models.Document{}, 0, err
	}

	if err := s.repo.DeleteChunksByDocumentID(ctx, document.ID); err != nil {
		return models.Document{}, 0, err
	}

	chunks := chunking.SplitWithConfig(textForRAG, chunking.DefaultConfig())
	if err := s.repo.InsertChunks(ctx, document.ID, source.ID, nil, chunks); err != nil {
		return models.Document{}, 0, err
	}

	return document, len(chunks), nil
}

func ptrIfNotEmpty(value string) *string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return nil
	}
	return &trimmed
}

func sanitizeOptionalTitle(title *string) *string {
	if title == nil {
		return nil
	}
	trimmed := strings.TrimSpace(*title)
	if trimmed == "" {
		return nil
	}
	return &trimmed
}

func providerOrDefault(value *string) string {
	if value == nil || strings.TrimSpace(*value) == "" {
		return YouTubeProvider
	}
	return strings.TrimSpace(*value)
}

func externalIDOrEmpty(value *string) string {
	if value == nil {
		return ""
	}
	return strings.TrimSpace(*value)
}

func mapFromAny(value any) map[string]any {
	data, ok := value.(map[string]any)
	if !ok || data == nil {
		return map[string]any{}
	}
	return cloneAnyMap(data)
}

func optionalStringFromAny(value any) *string {
	typed, ok := value.(string)
	if !ok {
		return nil
	}
	return ptrIfNotEmpty(typed)
}

func cloneAnyMap(input map[string]any) map[string]any {
	if input == nil {
		return map[string]any{}
	}
	out := make(map[string]any, len(input))
	for key, value := range input {
		out[key] = value
	}
	return out
}

func floatFromAny(value any) (float64, bool) {
	switch typed := value.(type) {
	case float64:
		return typed, true
	case float32:
		return float64(typed), true
	case int:
		return float64(typed), true
	case int64:
		return float64(typed), true
	case int32:
		return float64(typed), true
	case uint:
		return float64(typed), true
	case uint64:
		return float64(typed), true
	case uint32:
		return float64(typed), true
	default:
		return 0, false
	}
}

func buildTimingSummary(downloadMetadata map[string]any, transcribeMetadata map[string]any, startedAt time.Time) map[string]any {
	out := map[string]any{
		"backend_pipeline_elapsed_seconds": roundFloat(time.Since(startedAt).Seconds(), 3),
	}

	downloadElapsed, hasDownload := floatFromAny(downloadMetadata["elapsed_seconds"])
	transcribeElapsed, hasTranscribe := floatFromAny(transcribeMetadata["elapsed_seconds"])
	if hasDownload {
		out["download_elapsed_seconds"] = roundFloat(downloadElapsed, 3)
	}
	if hasTranscribe {
		out["transcription_elapsed_seconds"] = roundFloat(transcribeElapsed, 3)
	}
	switch {
	case hasDownload && hasTranscribe:
		out["total_elapsed_seconds"] = roundFloat(downloadElapsed+transcribeElapsed, 3)
	case hasTranscribe:
		out["total_elapsed_seconds"] = roundFloat(transcribeElapsed, 3)
	case hasDownload:
		out["total_elapsed_seconds"] = roundFloat(downloadElapsed, 3)
	}
	return out
}

func roundFloat(value float64, precision int) float64 {
	if precision < 0 {
		return value
	}
	pow := 1.0
	for i := 0; i < precision; i++ {
		pow *= 10
	}
	if pow == 0 {
		return value
	}
	if value >= 0 {
		return float64(int64(value*pow+0.5)) / pow
	}
	return float64(int64(value*pow-0.5)) / pow
}

func audioErrorPayload(err error) (string, map[string]any) {
	message := strings.TrimSpace(err.Error())
	if message == "" {
		message = "unknown error"
	}

	payload := map[string]any{
		"message": message,
	}

	if providerErr, ok := IsProviderError(err); ok && providerErr != nil {
		providerMessage := strings.TrimSpace(providerErr.Message)
		if providerMessage == "" {
			providerMessage = message
		}
		details := map[string]any{
			"status_code": providerErr.StatusCode,
			"stage":       providerErr.Stage,
			"code":        providerErr.Code,
			"message":     providerMessage,
			"details":     providerErr.Details,
		}
		payload["provider_error"] = details

		prefix := strings.Trim(strings.Join([]string{strings.TrimSpace(providerErr.Stage), strings.TrimSpace(providerErr.Code)}, "/"), "/")
		if prefix != "" {
			message = prefix + ": " + providerMessage
		} else {
			message = providerMessage
		}
	}

	return message, payload
}

func isSourceURLConflict(err error) bool {
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) {
		return false
	}
	return pgErr.Code == "23505" && pgErr.ConstraintName == "sources_url_key"
}
