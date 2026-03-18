package youtube

import (
	"context"
	"fmt"
)

type DownloadAudioRequest struct {
	SourceID string
	URL      string
}

type DownloadAudioResult struct {
	SourceID      string
	VideoID       string
	Title         *string
	AudioFilePath string
	Metadata      map[string]any
}

type TranscribeAudioRequest struct {
	SourceID      string
	VideoID       string
	AudioFilePath string
	Language      *string
}

type TranscribeSegment struct {
	SegmentIndex int      `json:"segment_index"`
	StartSeconds float64  `json:"start_seconds"`
	EndSeconds   float64  `json:"end_seconds"`
	SpeakerLabel string   `json:"speaker_label"`
	RoleLabel    string   `json:"role_label"`
	TextRaw      string   `json:"text_raw"`
	Confidence   *float64 `json:"confidence,omitempty"`
}

type TranscribeAudioResult struct {
	SourceID     string              `json:"source_id"`
	VideoID      string              `json:"video_id"`
	Provider     string              `json:"provider"`
	Model        string              `json:"model"`
	Language     *string             `json:"language,omitempty"`
	FullTextRaw  string              `json:"full_text_raw"`
	FullTextRAG  string              `json:"full_text_rag"`
	SpeakerRoles map[string]string   `json:"speaker_roles"`
	Segments     []TranscribeSegment `json:"segments"`
	Metadata     map[string]any      `json:"metadata"`
}

type AudioProvider interface {
	DownloadYouTubeAudio(ctx context.Context, req DownloadAudioRequest) (DownloadAudioResult, error)
	TranscribeYouTubeAudio(ctx context.Context, req TranscribeAudioRequest) (TranscribeAudioResult, error)
}

type ProviderError struct {
	StatusCode int
	Code       string
	Stage      string
	Message    string
	Details    map[string]any
}

func (e *ProviderError) Error() string {
	if e == nil {
		return ""
	}
	if e.Stage != "" {
		return fmt.Sprintf("provider %s error: %s", e.Stage, e.Message)
	}
	if e.Code != "" {
		return fmt.Sprintf("provider %s: %s", e.Code, e.Message)
	}
	return e.Message
}

func IsProviderError(err error) (*ProviderError, bool) {
	if err == nil {
		return nil, false
	}
	providerErr, ok := err.(*ProviderError)
	return providerErr, ok
}
