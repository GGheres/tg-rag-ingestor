package hhclient

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"sync/atomic"
	"testing"

	"tg-rag-ingestor/backend/internal/auth"
	"tg-rag-ingestor/backend/internal/model"
)

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
}

func testConfig(serverURL string) model.HHConfig {
	return model.HHConfig{
		ClientID:           "client-id",
		ClientSecret:       "client-secret",
		AccessToken:        "token-1",
		RefreshToken:       "refresh-1",
		RedirectURI:        "http://localhost/callback",
		UserAgent:          "tg-rag-ingestor-tests/1.0 (tests@example.com)",
		BaseURL:            serverURL,
		TokenURL:           serverURL + "/oauth/token",
		AuthURL:            serverURL + "/oauth/authorize",
		RequestTimeoutSec:  5,
		RateLimitPerSecond: 1000,
		RetryMax:           3,
		RetryBaseBackoffMS: 1,
		RetryMaxBackoffMS:  5,
	}.WithDefaults()
}

func TestClientGetJSONSuccess(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer token-1" {
			t.Fatalf("unexpected auth header: %s", r.Header.Get("Authorization"))
		}
		if r.Header.Get("HH-User-Agent") == "" {
			t.Fatal("missing HH-User-Agent header")
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"value":"ok"}`))
	}))
	defer server.Close()

	cfg := testConfig(server.URL)
	client := New(cfg, auth.NewManager(cfg, testLogger()), testLogger())

	var out map[string]string
	status, err := client.GetJSON(context.Background(), "/", nil, &out)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if status != http.StatusOK {
		t.Fatalf("unexpected status: %d", status)
	}
	if out["value"] != "ok" {
		t.Fatalf("unexpected payload: %+v", out)
	}
}

func TestClientRetries429(t *testing.T) {
	var attempts int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := atomic.AddInt32(&attempts, 1)
		if n < 3 {
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = w.Write([]byte(`{"errors":[{"type":"too_many_requests"}]}`))
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer server.Close()

	cfg := testConfig(server.URL)
	client := New(cfg, auth.NewManager(cfg, testLogger()), testLogger())

	var out map[string]any
	status, err := client.GetJSON(context.Background(), "/", nil, &out)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if status != http.StatusOK {
		t.Fatalf("unexpected status: %d", status)
	}
	if atomic.LoadInt32(&attempts) != 3 {
		t.Fatalf("expected 3 attempts, got %d", attempts)
	}
}

func TestClientRefreshOn401(t *testing.T) {
	var resumeCalls int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/oauth/token" {
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(model.TokenResponse{
				AccessToken:  "token-2",
				RefreshToken: "refresh-2",
				TokenType:    "bearer",
				ExpiresIn:    3600,
			})
			return
		}
		call := atomic.AddInt32(&resumeCalls, 1)
		if call == 1 {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"errors":[{"type":"oauth","value":"token_expired","message":"expired"}]}`))
			return
		}
		if r.Header.Get("Authorization") != "Bearer token-2" {
			t.Fatalf("token was not refreshed, got %s", r.Header.Get("Authorization"))
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer server.Close()

	cfg := testConfig(server.URL)
	manager := auth.NewManager(cfg, testLogger())
	client := New(cfg, manager, testLogger())

	var out map[string]any
	status, err := client.GetJSON(context.Background(), "/", nil, &out)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if status != http.StatusOK {
		t.Fatalf("unexpected status: %d", status)
	}
	if atomic.LoadInt32(&resumeCalls) != 2 {
		t.Fatalf("expected 2 calls after refresh, got %d", resumeCalls)
	}
}

func TestClientRefreshOn403TokenExpired(t *testing.T) {
	var resumeCalls int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/oauth/token" {
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(model.TokenResponse{
				AccessToken:  "token-2",
				RefreshToken: "refresh-2",
				TokenType:    "bearer",
				ExpiresIn:    3600,
			})
			return
		}
		call := atomic.AddInt32(&resumeCalls, 1)
		if call == 1 {
			w.WriteHeader(http.StatusForbidden)
			_, _ = w.Write([]byte(`{"errors":[{"type":"oauth","value":"token_expired","message":"expired"}]}`))
			return
		}
		if r.Header.Get("Authorization") != "Bearer token-2" {
			t.Fatalf("token was not refreshed, got %s", r.Header.Get("Authorization"))
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer server.Close()

	cfg := testConfig(server.URL)
	manager := auth.NewManager(cfg, testLogger())
	client := New(cfg, manager, testLogger())

	var out map[string]any
	status, err := client.GetJSON(context.Background(), "/", nil, &out)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if status != http.StatusOK {
		t.Fatalf("unexpected status: %d", status)
	}
	if atomic.LoadInt32(&resumeCalls) != 2 {
		t.Fatalf("expected 2 calls after refresh, got %d", resumeCalls)
	}
}

func TestClientRefreshWhenAccessTokenMissing(t *testing.T) {
	var tokenCalls int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/oauth/token" {
			atomic.AddInt32(&tokenCalls, 1)
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(model.TokenResponse{
				AccessToken:  "token-from-refresh",
				RefreshToken: "refresh-2",
				TokenType:    "bearer",
				ExpiresIn:    3600,
			})
			return
		}
		if r.Header.Get("Authorization") != "Bearer token-from-refresh" {
			t.Fatalf("expected refreshed auth header, got %s", r.Header.Get("Authorization"))
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer server.Close()

	cfg := testConfig(server.URL)
	cfg.AccessToken = ""
	manager := auth.NewManager(cfg, testLogger())
	client := New(cfg, manager, testLogger())

	var out map[string]any
	status, err := client.GetJSON(context.Background(), "/", nil, &out)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if status != http.StatusOK {
		t.Fatalf("unexpected status: %d", status)
	}
	if atomic.LoadInt32(&tokenCalls) != 1 {
		t.Fatalf("expected one token refresh, got %d", tokenCalls)
	}
}

func TestClientMapsAPIError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"errors":[{"type":"cant_view_contacts","value":"contacts","message":"hidden"}]}`))
	}))
	defer server.Close()

	cfg := testConfig(server.URL)
	client := New(cfg, auth.NewManager(cfg, testLogger()), testLogger())

	_, err := client.GetJSON(context.Background(), "/", nil, nil)
	if err == nil {
		t.Fatal("expected api error")
	}
	apiErr, ok := err.(*APIError)
	if !ok {
		t.Fatalf("unexpected error type: %T", err)
	}
	if apiErr.Type != "cant_view_contacts" {
		t.Fatalf("unexpected error type: %s", apiErr.Type)
	}
}
