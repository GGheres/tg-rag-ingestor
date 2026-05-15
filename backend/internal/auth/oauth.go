package auth

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"tg-rag-ingestor/backend/internal/model"
)

type Manager struct {
	cfg    model.HHConfig
	client *http.Client
	logger *slog.Logger

	mu           sync.RWMutex
	refreshMu    sync.Mutex
	accessToken  string
	refreshToken string
}

func NewManager(cfg model.HHConfig, logger *slog.Logger) *Manager {
	cfg = cfg.WithDefaults()
	return &Manager{
		cfg: cfg,
		client: &http.Client{
			Timeout: time.Duration(cfg.RequestTimeoutSec) * time.Second,
		},
		logger:       logger,
		accessToken:  cfg.AccessToken,
		refreshToken: cfg.RefreshToken,
	}
}

func (m *Manager) AuthorizationURL() string {
	query := url.Values{}
	query.Set("response_type", "code")
	query.Set("client_id", m.cfg.ClientID)
	query.Set("redirect_uri", m.cfg.RedirectURI)
	return m.cfg.AuthURL + "?" + query.Encode()
}

func (m *Manager) ExchangeCode(ctx context.Context, code string) (*model.TokenResponse, error) {
	form := url.Values{}
	form.Set("grant_type", "authorization_code")
	form.Set("client_id", m.cfg.ClientID)
	form.Set("client_secret", m.cfg.ClientSecret)
	form.Set("redirect_uri", m.cfg.RedirectURI)
	form.Set("code", strings.TrimSpace(code))

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, m.cfg.TokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, fmt.Errorf("build oauth exchange request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("User-Agent", m.cfg.UserAgent)
	req.Header.Set("HH-User-Agent", m.cfg.UserAgent)

	resp, err := m.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("oauth exchange request failed: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("oauth exchange failed: status=%d body=%s", resp.StatusCode, string(body))
	}

	var token model.TokenResponse
	if err := json.Unmarshal(body, &token); err != nil {
		return nil, fmt.Errorf("decode oauth exchange response: %w", err)
	}

	m.SetTokens(token.AccessToken, token.RefreshToken)
	return &token, nil
}

func (m *Manager) Refresh(ctx context.Context) error {
	return m.refresh(ctx, "", true)
}

func (m *Manager) EnsureAccessToken(ctx context.Context) error {
	m.mu.RLock()
	hasAccess := strings.TrimSpace(m.accessToken) != ""
	hasRefresh := strings.TrimSpace(m.refreshToken) != ""
	m.mu.RUnlock()
	if hasAccess || !hasRefresh {
		return nil
	}
	return m.refresh(ctx, "", false)
}

func (m *Manager) RefreshIfCurrent(ctx context.Context, failedAccessToken string) error {
	return m.refresh(ctx, failedAccessToken, false)
}

func (m *Manager) refresh(ctx context.Context, failedAccessToken string, force bool) error {
	m.refreshMu.Lock()
	defer m.refreshMu.Unlock()

	m.mu.RLock()
	currentAccess := strings.TrimSpace(m.accessToken)
	refresh := m.refreshToken
	m.mu.RUnlock()

	failedAccessToken = strings.TrimSpace(failedAccessToken)
	if !force {
		switch {
		case failedAccessToken == "" && currentAccess != "":
			return nil
		case failedAccessToken != "" && currentAccess != "" && currentAccess != failedAccessToken:
			return nil
		}
	}
	if strings.TrimSpace(refresh) == "" {
		return fmt.Errorf("no refresh token configured")
	}

	form := url.Values{}
	form.Set("grant_type", "refresh_token")
	form.Set("refresh_token", refresh)
	form.Set("client_id", m.cfg.ClientID)
	form.Set("client_secret", m.cfg.ClientSecret)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, m.cfg.TokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return fmt.Errorf("build oauth refresh request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("User-Agent", m.cfg.UserAgent)
	req.Header.Set("HH-User-Agent", m.cfg.UserAgent)

	resp, err := m.client.Do(req)
	if err != nil {
		return fmt.Errorf("oauth refresh request failed: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("oauth refresh failed: status=%d body=%s", resp.StatusCode, string(body))
	}

	var token model.TokenResponse
	if err := json.Unmarshal(body, &token); err != nil {
		return fmt.Errorf("decode oauth refresh response: %w", err)
	}

	updated := m.setTokenResponse(token)

	if m.cfg.OnTokenRefresh != nil {
		if err := m.cfg.OnTokenRefresh(ctx, updated); err != nil {
			return fmt.Errorf("persist refreshed hh oauth tokens: %w", err)
		}
	}

	m.logger.Info("hh oauth token refreshed")
	return nil
}

func (m *Manager) AccessToken() string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.accessToken
}

func (m *Manager) RefreshToken() string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.refreshToken
}

func (m *Manager) SetTokens(accessToken, refreshToken string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if strings.TrimSpace(accessToken) != "" {
		m.accessToken = strings.TrimSpace(accessToken)
	}
	if strings.TrimSpace(refreshToken) != "" {
		m.refreshToken = strings.TrimSpace(refreshToken)
	}
}

func (m *Manager) setTokenResponse(token model.TokenResponse) model.TokenResponse {
	m.mu.Lock()
	defer m.mu.Unlock()
	if strings.TrimSpace(token.AccessToken) != "" {
		m.accessToken = strings.TrimSpace(token.AccessToken)
	}
	if strings.TrimSpace(token.RefreshToken) != "" {
		m.refreshToken = strings.TrimSpace(token.RefreshToken)
	}
	return model.TokenResponse{
		AccessToken:  m.accessToken,
		RefreshToken: m.refreshToken,
		TokenType:    token.TokenType,
		ExpiresIn:    token.ExpiresIn,
	}
}
