package youtube

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

type AudioClient struct {
	baseURL    string
	httpClient *http.Client
}

func NewAudioClient(baseURL string) *AudioClient {
	baseURL = strings.TrimSpace(baseURL)
	baseURL = strings.TrimRight(baseURL, "/")
	return &AudioClient{
		baseURL: baseURL,
		httpClient: &http.Client{
			Timeout: 90 * time.Minute,
		},
	}
}

type downloadAudioRequest struct {
	SourceID string `json:"source_id"`
	URL      string `json:"url"`
}

type downloadAudioResponse struct {
	SourceID      string         `json:"source_id"`
	VideoID       string         `json:"video_id"`
	Title         *string        `json:"title"`
	AudioFilePath string         `json:"audio_file_path"`
	Metadata      map[string]any `json:"metadata"`
}

type transcribeAudioRequest struct {
	SourceID      string  `json:"source_id"`
	VideoID       string  `json:"video_id,omitempty"`
	AudioFilePath string  `json:"audio_file_path"`
	Language      *string `json:"language,omitempty"`
}

type transcribeSegmentResponse struct {
	SegmentIndex int      `json:"segment_index"`
	StartSeconds float64  `json:"start_seconds"`
	EndSeconds   float64  `json:"end_seconds"`
	SpeakerLabel string   `json:"speaker_label"`
	RoleLabel    string   `json:"role_label"`
	TextRaw      string   `json:"text_raw"`
	Confidence   *float64 `json:"confidence"`
}

type transcribeAudioResponse struct {
	SourceID     string                      `json:"source_id"`
	VideoID      string                      `json:"video_id"`
	Provider     string                      `json:"provider"`
	Model        string                      `json:"model"`
	Language     *string                     `json:"language"`
	FullTextRaw  string                      `json:"full_text_raw"`
	FullTextRAG  string                      `json:"full_text_rag"`
	SpeakerRoles map[string]string           `json:"speaker_roles"`
	Segments     []transcribeSegmentResponse `json:"segments"`
	Metadata     map[string]any              `json:"metadata"`
}

type providerErrorEnvelope struct {
	Error *struct {
		Code    string         `json:"code"`
		Stage   string         `json:"stage"`
		Message string         `json:"message"`
		Details map[string]any `json:"details"`
	} `json:"error"`
}

func (c *AudioClient) DownloadYouTubeAudio(ctx context.Context, req DownloadAudioRequest) (DownloadAudioResult, error) {
	if c.baseURL == "" {
		return DownloadAudioResult{}, fmt.Errorf("python audio service url is empty")
	}
	payload := downloadAudioRequest{
		SourceID: req.SourceID,
		URL:      req.URL,
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return DownloadAudioResult{}, err
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/api/download-youtube-audio", bytes.NewReader(body))
	if err != nil {
		return DownloadAudioResult{}, err
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return DownloadAudioResult{}, fmt.Errorf("call python download audio service: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return DownloadAudioResult{}, err
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		providerErr := &ProviderError{
			StatusCode: resp.StatusCode,
			Message:    strings.TrimSpace(string(respBody)),
		}
		var envelope providerErrorEnvelope
		if err := json.Unmarshal(respBody, &envelope); err == nil && envelope.Error != nil {
			providerErr.Code = envelope.Error.Code
			providerErr.Stage = envelope.Error.Stage
			providerErr.Message = envelope.Error.Message
			providerErr.Details = envelope.Error.Details
		}
		if providerErr.Message == "" {
			providerErr.Message = fmt.Sprintf("python download service returned %d", resp.StatusCode)
		}
		return DownloadAudioResult{}, providerErr
	}

	var providerResp downloadAudioResponse
	if err := json.Unmarshal(respBody, &providerResp); err != nil {
		return DownloadAudioResult{}, fmt.Errorf("decode python download response: %w", err)
	}

	return DownloadAudioResult{
		SourceID:      providerResp.SourceID,
		VideoID:       providerResp.VideoID,
		Title:         providerResp.Title,
		AudioFilePath: providerResp.AudioFilePath,
		Metadata:      providerResp.Metadata,
	}, nil
}

func (c *AudioClient) TranscribeYouTubeAudio(ctx context.Context, req TranscribeAudioRequest) (TranscribeAudioResult, error) {
	if c.baseURL == "" {
		return TranscribeAudioResult{}, fmt.Errorf("python audio service url is empty")
	}
	payload := transcribeAudioRequest{
		SourceID:      req.SourceID,
		VideoID:       req.VideoID,
		AudioFilePath: req.AudioFilePath,
		Language:      req.Language,
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return TranscribeAudioResult{}, err
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/api/transcribe-youtube-audio", bytes.NewReader(body))
	if err != nil {
		return TranscribeAudioResult{}, err
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return TranscribeAudioResult{}, fmt.Errorf("call python transcribe audio service: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return TranscribeAudioResult{}, err
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		providerErr := &ProviderError{
			StatusCode: resp.StatusCode,
			Message:    strings.TrimSpace(string(respBody)),
		}
		var envelope providerErrorEnvelope
		if err := json.Unmarshal(respBody, &envelope); err == nil && envelope.Error != nil {
			providerErr.Code = envelope.Error.Code
			providerErr.Stage = envelope.Error.Stage
			providerErr.Message = envelope.Error.Message
			providerErr.Details = envelope.Error.Details
		}
		if providerErr.Message == "" {
			providerErr.Message = fmt.Sprintf("python transcribe service returned %d", resp.StatusCode)
		}
		return TranscribeAudioResult{}, providerErr
	}

	var providerResp transcribeAudioResponse
	if err := json.Unmarshal(respBody, &providerResp); err != nil {
		return TranscribeAudioResult{}, fmt.Errorf("decode python transcribe response: %w", err)
	}

	segments := make([]TranscribeSegment, 0, len(providerResp.Segments))
	for _, item := range providerResp.Segments {
		segments = append(segments, TranscribeSegment{
			SegmentIndex: item.SegmentIndex,
			StartSeconds: item.StartSeconds,
			EndSeconds:   item.EndSeconds,
			SpeakerLabel: item.SpeakerLabel,
			RoleLabel:    item.RoleLabel,
			TextRaw:      item.TextRaw,
			Confidence:   item.Confidence,
		})
	}

	return TranscribeAudioResult{
		SourceID:     providerResp.SourceID,
		VideoID:      providerResp.VideoID,
		Provider:     providerResp.Provider,
		Model:        providerResp.Model,
		Language:     providerResp.Language,
		FullTextRaw:  providerResp.FullTextRaw,
		FullTextRAG:  providerResp.FullTextRAG,
		SpeakerRoles: providerResp.SpeakerRoles,
		Segments:     segments,
		Metadata:     providerResp.Metadata,
	}, nil
}
