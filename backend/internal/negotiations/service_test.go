package negotiations

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
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

func TestFetchCoverLetterPrefersChatAPI(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/common/chats/3490/messages":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"id": "3490",
				"messages": []map[string]any{
					{
						"payload": map[string]any{"text": "Хочу откликнуться на вакансию"},
						"sender_display_info": map[string]any{
							"role": "APPLICANT",
						},
					},
					{
						"payload": map[string]any{"text": "Спасибо, получили"},
						"sender_display_info": map[string]any{
							"role": "EMPLOYER",
						},
					},
				},
			})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
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

	text, err := service.FetchCoverLetter(context.Background(), model.NegotiationCandidate{
		NegotiationID: "neg_1",
		ChatID:        "3490",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if text != "Хочу откликнуться на вакансию" {
		t.Fatalf("unexpected cover letter: %q", text)
	}
}

func TestFetchCoverLetterIgnoresApplicantReplyWhenChatStartedByEmployer(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/common/chats/3490/messages":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"id": "3490",
				"messages": []map[string]any{
					{
						"payload": map[string]any{"text": "Приглашаем обсудить вакансию"},
						"sender_display_info": map[string]any{
							"role": "EMPLOYER",
						},
					},
					{
						"payload": map[string]any{"text": "Спасибо за приглашение"},
						"sender_display_info": map[string]any{
							"role": "APPLICANT",
						},
					},
				},
			})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
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

	text, err := service.FetchCoverLetter(context.Background(), model.NegotiationCandidate{
		NegotiationID: "neg_1",
		ChatID:        "3490",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if text != "" {
		t.Fatalf("expected no cover letter, got %q", text)
	}
}

func TestFetchCoverLetterFallsBackToLegacyMessages(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/common/chats/3490/messages":
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"errors":[{"type":"not_found"}]}`))
		case "/negotiations/neg_1/messages":
			if got := r.URL.Query().Get("with_text_only"); got != "true" {
				t.Fatalf("expected with_text_only=true, got %q", got)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"items": []map[string]any{
					{
						"text": "Есть опыт по профилю вакансии",
						"author": map[string]any{
							"participant_type": "applicant",
						},
					},
				},
			})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
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

	text, err := service.FetchCoverLetter(context.Background(), model.NegotiationCandidate{
		NegotiationID: "neg_1",
		MessagesURL:   server.URL + "/negotiations/neg_1/messages",
		ChatID:        "3490",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if text != "Есть опыт по профилю вакансии" {
		t.Fatalf("unexpected cover letter: %q", text)
	}
}

func TestFetchByVacancyMapsResumeContextAndChatID(t *testing.T) {
	var baseURL string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/negotiations" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"found": 1,
			"pages": 1,
			"page":  0,
			"items": []map[string]any{
				{
					"id":           "neg_1",
					"chat_id":      3490,
					"messages_url": baseURL + "/negotiations/neg_1/messages",
					"resume": map[string]any{
						"id":         "resume_1",
						"url":        baseURL + "/resumes/resume_1?topic_id=neg_1",
						"first_name": "Иван",
						"last_name":  "Иванов",
					},
					"applicant": map[string]any{"id": "cand_1"},
				},
			},
		})
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
	if len(candidates) != 1 {
		t.Fatalf("expected 1 candidate, got %d", len(candidates))
	}
	if candidates[0].ChatID != "3490" {
		t.Fatalf("unexpected chat id: %q", candidates[0].ChatID)
	}
	if !strings.Contains(candidates[0].ResumeAPIURL, "topic_id=neg_1") {
		t.Fatalf("resume api url lost topic context: %q", candidates[0].ResumeAPIURL)
	}
}
