package negotiations

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

func TestFetchByVacancyWithCollectionsAndDedupe(t *testing.T) {
	var baseURL string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/negotiations":
			_, _ = w.Write([]byte(`{
				"found": 3,
				"pages": 1,
				"page": 0,
				"collections": [{"id":"invited","name":"Invited","url":"` + baseURL + `/collection/invited","count": 3}],
				"items": []
			}`))
		case "/collection/invited":
			response := map[string]any{
				"found": 3,
				"pages": 1,
				"page":  0,
				"items": []map[string]any{
					{
						"id": "neg_1",
						"resume": map[string]any{
							"id":         "resume_1",
							"first_name": "Иван",
							"last_name":  "Иванов",
						},
						"applicant": map[string]any{"id": "cand_1"},
					},
					{
						"id": "neg_2",
						"resume": map[string]any{
							"id":         "resume_1",
							"first_name": "Иван",
							"last_name":  "Иванов",
						},
						"applicant": map[string]any{"id": "cand_1_dup"},
					},
					{
						"id": "neg_3",
						"resume": map[string]any{
							"id":         "resume_2",
							"first_name": "Мария",
							"last_name":  "Петрова",
						},
						"applicant": map[string]any{"id": "cand_2"},
					},
				},
			}
			_ = json.NewEncoder(w).Encode(response)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()
	baseURL = server.URL

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

	candidates, err := service.FetchByVacancy(context.Background(), "vacancy-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(candidates) != 2 {
		t.Fatalf("expected 2 unique candidates, got %d", len(candidates))
	}
	if candidates[0].ResumeID == candidates[1].ResumeID {
		t.Fatalf("resume ids were not deduplicated: %+v", candidates)
	}
}
