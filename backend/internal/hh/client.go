package hh

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	baseURL          = "https://api.hh.ru"
	tokenURL         = "https://hh.ru/oauth/token"
	authURL          = "https://hh.ru/oauth/authorize"
	defaultPerPage   = 50
	maxRetries       = 3
	initialBackoff   = 500 * time.Millisecond
	maxBackoff       = 30 * time.Second
	rateLimitWindow  = time.Second
	rateLimitBurst   = 5 // conservative: HH allows ~7/sec
)

// Client is the HH API HTTP client with auth, rate limiting, and retry.
type Client struct {
	httpClient  *http.Client
	accessToken string
	userAgent   string
	logger      *slog.Logger

	mu          sync.Mutex
	clientID    string
	clientSecret string
	refreshTok  string
	redirectURI string

	// rate limiter state
	rlMu      sync.Mutex
	rlTokens  int
	rlLastRef time.Time
}

func NewClient(cfg Config, logger *slog.Logger) *Client {
	return &Client{
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
		accessToken:  cfg.AccessToken,
		userAgent:    cfg.UserAgent,
		logger:       logger,
		clientID:     cfg.ClientID,
		clientSecret: cfg.ClientSecret,
		refreshTok:   cfg.RefreshToken,
		redirectURI:  cfg.RedirectURI,
		rlTokens:     rateLimitBurst,
		rlLastRef:    time.Now(),
	}
}

// AuthorizationURL returns the URL employer should visit to authorize the app.
func AuthorizationURL(clientID, redirectURI string) string {
	v := url.Values{}
	v.Set("response_type", "code")
	v.Set("client_id", clientID)
	v.Set("redirect_uri", redirectURI)
	return authURL + "?" + v.Encode()
}

// ExchangeCode exchanges authorization code for access+refresh tokens.
func (c *Client) ExchangeCode(ctx context.Context, code string) (*TokenResponse, error) {
	data := url.Values{}
	data.Set("grant_type", "authorization_code")
	data.Set("client_id", c.clientID)
	data.Set("client_secret", c.clientSecret)
	data.Set("redirect_uri", c.redirectURI)
	data.Set("code", code)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, tokenURL, strings.NewReader(data.Encode()))
	if err != nil {
		return nil, fmt.Errorf("build token request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("User-Agent", c.userAgent)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("token request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("token exchange failed: status=%d body=%s", resp.StatusCode, body)
	}

	var tok TokenResponse
	if err := json.NewDecoder(resp.Body).Decode(&tok); err != nil {
		return nil, fmt.Errorf("decode token response: %w", err)
	}

	c.mu.Lock()
	c.accessToken = tok.AccessToken
	c.refreshTok = tok.RefreshToken
	c.mu.Unlock()

	return &tok, nil
}

// RefreshAccessToken uses the refresh token to obtain a new access token.
func (c *Client) RefreshAccessToken(ctx context.Context) error {
	c.mu.Lock()
	rt := c.refreshTok
	c.mu.Unlock()

	if rt == "" {
		return fmt.Errorf("no refresh token available")
	}

	data := url.Values{}
	data.Set("grant_type", "refresh_token")
	data.Set("refresh_token", rt)
	data.Set("client_id", c.clientID)
	data.Set("client_secret", c.clientSecret)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, tokenURL, strings.NewReader(data.Encode()))
	if err != nil {
		return fmt.Errorf("build refresh request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("User-Agent", c.userAgent)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("refresh request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("token refresh failed: status=%d body=%s", resp.StatusCode, body)
	}

	var tok TokenResponse
	if err := json.NewDecoder(resp.Body).Decode(&tok); err != nil {
		return fmt.Errorf("decode refresh response: %w", err)
	}

	c.mu.Lock()
	c.accessToken = tok.AccessToken
	if tok.RefreshToken != "" {
		c.refreshTok = tok.RefreshToken
	}
	c.mu.Unlock()

	c.logger.Info("access token refreshed")
	return nil
}

// Get performs an authenticated GET request with retry and rate limiting.
func (c *Client) Get(ctx context.Context, path string, query url.Values) ([]byte, int, error) {
	u := baseURL + path
	if query != nil {
		u += "?" + query.Encode()
	}
	return c.doWithRetry(ctx, http.MethodGet, u, nil)
}

// GetURL performs an authenticated GET on a full URL (for collection URLs).
func (c *Client) GetURL(ctx context.Context, fullURL string, extraQuery url.Values) ([]byte, int, error) {
	if extraQuery != nil {
		parsed, err := url.Parse(fullURL)
		if err != nil {
			return nil, 0, fmt.Errorf("parse url: %w", err)
		}
		q := parsed.Query()
		for k, vs := range extraQuery {
			for _, v := range vs {
				q.Set(k, v)
			}
		}
		parsed.RawQuery = q.Encode()
		fullURL = parsed.String()
	}
	return c.doWithRetry(ctx, http.MethodGet, fullURL, nil)
}

// DownloadFile downloads a file from the given URL, returns body bytes.
func (c *Client) DownloadFile(ctx context.Context, fileURL string) ([]byte, error) {
	body, status, err := c.doWithRetry(ctx, http.MethodGet, fileURL, nil)
	if err != nil {
		return nil, err
	}
	if status != http.StatusOK {
		return nil, fmt.Errorf("download failed: status=%d", status)
	}
	return body, nil
}

func (c *Client) doWithRetry(ctx context.Context, method, u string, body io.Reader) ([]byte, int, error) {
	backoff := initialBackoff

	for attempt := 0; attempt <= maxRetries; attempt++ {
		if attempt > 0 {
			c.logger.Debug("retry", "attempt", attempt, "backoff", backoff, "url", u)
			select {
			case <-ctx.Done():
				return nil, 0, ctx.Err()
			case <-time.After(backoff):
			}
			backoff *= 2
			if backoff > maxBackoff {
				backoff = maxBackoff
			}
		}

		c.waitRateLimit()

		req, err := http.NewRequestWithContext(ctx, method, u, body)
		if err != nil {
			return nil, 0, fmt.Errorf("build request: %w", err)
		}

		c.mu.Lock()
		token := c.accessToken
		c.mu.Unlock()

		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("User-Agent", c.userAgent)
		req.Header.Set("HH-User-Agent", c.userAgent)

		resp, err := c.httpClient.Do(req)
		if err != nil {
			if attempt == maxRetries {
				return nil, 0, fmt.Errorf("request failed after %d retries: %w", maxRetries, err)
			}
			continue
		}

		respBody, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			return nil, 0, fmt.Errorf("read response body: %w", err)
		}

		switch {
		case resp.StatusCode == http.StatusTooManyRequests:
			retryAfter := resp.Header.Get("Retry-After")
			if retryAfter != "" {
				if secs, err := strconv.Atoi(retryAfter); err == nil {
					backoff = time.Duration(secs) * time.Second
				}
			}
			c.logger.Warn("rate limited", "retry_after", retryAfter, "url", u)
			continue

		case resp.StatusCode == http.StatusUnauthorized && attempt == 0:
			c.logger.Info("got 401, attempting token refresh")
			if err := c.RefreshAccessToken(ctx); err != nil {
				c.logger.Error("token refresh failed", "error", err)
				return respBody, resp.StatusCode, nil
			}
			continue

		case resp.StatusCode >= 500 && attempt < maxRetries:
			c.logger.Warn("server error, retrying", "status", resp.StatusCode, "url", u)
			continue

		default:
			return respBody, resp.StatusCode, nil
		}
	}

	return nil, 0, fmt.Errorf("request failed after %d retries: %s", maxRetries, u)
}

// waitRateLimit implements a simple token-bucket rate limiter.
func (c *Client) waitRateLimit() {
	c.rlMu.Lock()
	defer c.rlMu.Unlock()

	now := time.Now()
	elapsed := now.Sub(c.rlLastRef)
	c.rlTokens += int(elapsed / (rateLimitWindow / time.Duration(rateLimitBurst)))
	if c.rlTokens > rateLimitBurst {
		c.rlTokens = rateLimitBurst
	}
	c.rlLastRef = now

	if c.rlTokens > 0 {
		c.rlTokens--
		return
	}

	// Wait until next token is available.
	wait := rateLimitWindow / time.Duration(rateLimitBurst)
	c.rlMu.Unlock()
	time.Sleep(wait)
	c.rlMu.Lock()
	c.rlLastRef = time.Now()
}

// ParseAPIError attempts to parse an HH API error response.
func ParseAPIError(body []byte) *APIError {
	var apiErr APIError
	if err := json.Unmarshal(body, &apiErr); err != nil {
		return nil
	}
	if len(apiErr.Errors) == 0 {
		return nil
	}
	return &apiErr
}

// HasErrorType checks if the API error contains a specific error type.
func (e *APIError) HasErrorType(errType string) bool {
	if e == nil {
		return false
	}
	for _, detail := range e.Errors {
		if detail.Type == errType {
			return true
		}
	}
	return false
}
