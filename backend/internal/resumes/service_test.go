package resumes

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"tg-rag-ingestor/backend/internal/auth"
	"tg-rag-ingestor/backend/internal/hhclient"
	"tg-rag-ingestor/backend/internal/model"
)

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
}

func TestFetchFullUsesResumeAPIURL(t *testing.T) {
	var requestedPath string
	var requestedTopicID string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestedPath = r.URL.Path
		requestedTopicID = r.URL.Query().Get("topic_id")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id":         "resume_1",
			"first_name": "Иван",
			"last_name":  "Иванов",
		})
	}))
	defer server.Close()

	cfg := model.HHConfig{
		ClientID:           "test-client",
		ClientSecret:       "test-secret",
		AccessToken:        "token",
		RefreshToken:       "refresh",
		UserAgent:          "tests/1.0 (tests@example.com)",
		BaseURL:            server.URL,
		TokenURL:           server.URL + "/oauth/token",
		AuthURL:            server.URL + "/oauth/authorize",
		RequestTimeoutSec:  5,
		RateLimitPerSecond: 1000,
		RetryMax:           1,
		RetryBaseBackoffMS: 1,
		RetryMaxBackoffMS:  2,
	}.WithDefaults()

	authManager := auth.NewManager(cfg, testLogger())
	client := hhclient.New(cfg, authManager, testLogger())
	service := New(client, testLogger())

	resume, err := service.FetchFull(context.Background(), model.NegotiationCandidate{
		ResumeID:     "resume_1",
		ResumeAPIURL: server.URL + "/resumes/resume_1?topic_id=neg_1",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resume.ID != "resume_1" {
		t.Fatalf("unexpected resume id: %q", resume.ID)
	}
	if requestedPath != "/resumes/resume_1" {
		t.Fatalf("unexpected path: %q", requestedPath)
	}
	if requestedTopicID != "neg_1" {
		t.Fatalf("expected topic_id=neg_1, got %q", requestedTopicID)
	}
}
