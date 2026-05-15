package hhclient

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"math"
	"math/rand"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"tg-rag-ingestor/backend/internal/auth"
	"tg-rag-ingestor/backend/internal/model"
)

const managerAccountHeader = "X-Manager-Account-Id"

type APIError struct {
	StatusCode int
	Type       string
	Value      string
	Message    string
	Body       string
}

func (e *APIError) Error() string {
	parts := []string{fmt.Sprintf("status=%d", e.StatusCode)}
	if e.Type != "" {
		parts = append(parts, "type="+e.Type)
	}
	if e.Value != "" {
		parts = append(parts, "value="+e.Value)
	}
	if e.Message != "" {
		parts = append(parts, "message="+e.Message)
	}
	if e.Body != "" && e.Message == "" {
		parts = append(parts, "body="+e.Body)
	}
	return strings.Join(parts, " ")
}

type Client struct {
	cfg     model.HHConfig
	client  *http.Client
	auth    *auth.Manager
	logger  *slog.Logger
	limiter *rateLimiter
}

func New(cfg model.HHConfig, authManager *auth.Manager, logger *slog.Logger) *Client {
	cfg = cfg.WithDefaults()
	return &Client{
		cfg: cfg,
		client: &http.Client{
			Timeout: time.Duration(cfg.RequestTimeoutSec) * time.Second,
		},
		auth:    authManager,
		logger:  logger,
		limiter: newRateLimiter(cfg.RateLimitPerSecond),
	}
}

func (c *Client) GetJSON(ctx context.Context, path string, query url.Values, out any) (int, error) {
	rawURL := c.cfg.BaseURL + path
	body, status, err := c.do(ctx, http.MethodGet, rawURL, query, nil, nil)
	if err != nil {
		return status, err
	}
	if out == nil {
		return status, nil
	}
	if err := json.Unmarshal(body, out); err != nil {
		return status, fmt.Errorf("decode json response: %w", err)
	}
	return status, nil
}

func (c *Client) GetJSONURL(ctx context.Context, rawURL string, query url.Values, out any) (int, error) {
	body, status, err := c.do(ctx, http.MethodGet, rawURL, query, nil, nil)
	if err != nil {
		return status, err
	}
	if out == nil {
		return status, nil
	}
	if err := json.Unmarshal(body, out); err != nil {
		return status, fmt.Errorf("decode json response: %w", err)
	}
	return status, nil
}

func (c *Client) GetJSONWithHeaders(ctx context.Context, path string, query url.Values, headers http.Header, out any) (int, error) {
	rawURL := c.cfg.BaseURL + path
	body, status, err := c.do(ctx, http.MethodGet, rawURL, query, nil, headers)
	if err != nil {
		return status, err
	}
	if out == nil {
		return status, nil
	}
	if err := json.Unmarshal(body, out); err != nil {
		return status, fmt.Errorf("decode json response: %w", err)
	}
	return status, nil
}

func (c *Client) Download(ctx context.Context, rawURL string) ([]byte, int, error) {
	return c.do(ctx, http.MethodGet, rawURL, nil, nil, nil)
}

func (c *Client) do(ctx context.Context, method, rawURL string, query url.Values, body io.Reader, headers http.Header) ([]byte, int, error) {
	maxAttempts := c.cfg.RetryMax + 1
	baseBackoff := time.Duration(c.cfg.RetryBaseBackoffMS) * time.Millisecond
	maxBackoff := time.Duration(c.cfg.RetryMaxBackoffMS) * time.Millisecond

	var lastStatus int
	for attempt := 0; attempt < maxAttempts; attempt++ {
		if err := c.limiter.Wait(ctx); err != nil {
			return nil, 0, err
		}

		if err := c.auth.EnsureAccessToken(ctx); err != nil {
			return nil, 0, err
		}

		reqURL := rawURL
		if query != nil {
			parsed, err := url.Parse(rawURL)
			if err != nil {
				return nil, 0, fmt.Errorf("parse request url: %w", err)
			}
			q := parsed.Query()
			for k, values := range query {
				for _, v := range values {
					q.Set(k, v)
				}
			}
			parsed.RawQuery = q.Encode()
			reqURL = parsed.String()
		}

		req, err := http.NewRequestWithContext(ctx, method, reqURL, body)
		if err != nil {
			return nil, 0, fmt.Errorf("build request: %w", err)
		}
		accessToken := strings.TrimSpace(c.auth.AccessToken())
		if accessToken != "" {
			req.Header.Set("Authorization", "Bearer "+accessToken)
		}
		req.Header.Set("User-Agent", c.cfg.UserAgent)
		req.Header.Set("HH-User-Agent", c.cfg.UserAgent)
		if managerAccountID := strings.TrimSpace(c.cfg.ManagerAccountID); managerAccountID != "" {
			req.Header.Set(managerAccountHeader, managerAccountID)
		}
		for key, values := range headers {
			for _, value := range values {
				req.Header.Add(key, value)
			}
		}

		resp, err := c.client.Do(req)
		if err != nil {
			if attempt == maxAttempts-1 {
				return nil, lastStatus, fmt.Errorf("request failed after retries: %w", err)
			}
			if !sleepWithContext(ctx, jitterBackoff(baseBackoff, maxBackoff, attempt)) {
				return nil, 0, ctx.Err()
			}
			continue
		}

		respBody, readErr := io.ReadAll(resp.Body)
		resp.Body.Close()
		if readErr != nil {
			return nil, resp.StatusCode, fmt.Errorf("read response body: %w", readErr)
		}
		lastStatus = resp.StatusCode

		if shouldRefreshAuth(resp.StatusCode, respBody) && attempt < maxAttempts-1 {
			if refreshErr := c.auth.RefreshIfCurrent(ctx, accessToken); refreshErr != nil {
				return respBody, resp.StatusCode, parseAPIError(resp.StatusCode, respBody)
			}
			continue
		}

		if shouldRetryStatus(resp.StatusCode) && attempt < maxAttempts-1 {
			wait := jitterBackoff(baseBackoff, maxBackoff, attempt)
			if resp.StatusCode == http.StatusTooManyRequests {
				if v := strings.TrimSpace(resp.Header.Get("Retry-After")); v != "" {
					if sec, err := strconv.Atoi(v); err == nil && sec > 0 {
						wait = time.Duration(sec) * time.Second
					}
				}
			}
			c.logger.Warn("hh request retry",
				"status", resp.StatusCode,
				"attempt", attempt+1,
				"url", reqURL,
				"wait_ms", wait.Milliseconds(),
			)
			if !sleepWithContext(ctx, wait) {
				return nil, resp.StatusCode, ctx.Err()
			}
			continue
		}

		if resp.StatusCode >= 400 {
			return respBody, resp.StatusCode, parseAPIError(resp.StatusCode, respBody)
		}
		return respBody, resp.StatusCode, nil
	}

	return nil, lastStatus, fmt.Errorf("request failed after retries")
}

func parseAPIError(status int, body []byte) error {
	trimmedBody := strings.TrimSpace(string(body))
	if trimmedBody == "" {
		return &APIError{StatusCode: status}
	}

	var resp model.HHErrorResponse
	if err := json.Unmarshal(body, &resp); err != nil || len(resp.Errors) == 0 {
		return &APIError{
			StatusCode: status,
			Body:       trimmedBody,
		}
	}

	first := resp.Errors[0]
	return &APIError{
		StatusCode: status,
		Type:       first.Type,
		Value:      first.Value,
		Message:    first.Message,
		Body:       trimmedBody,
	}
}

func shouldRetryStatus(status int) bool {
	return status == http.StatusTooManyRequests || status >= 500
}

func shouldRefreshAuth(status int, body []byte) bool {
	if status == http.StatusUnauthorized {
		return true
	}
	if status != http.StatusForbidden {
		return false
	}

	var resp model.HHErrorResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return false
	}
	for _, item := range resp.Errors {
		if item.Type == "oauth" && item.Value == "token_expired" {
			return true
		}
	}
	return false
}

func jitterBackoff(base, max time.Duration, attempt int) time.Duration {
	if base <= 0 {
		base = 300 * time.Millisecond
	}
	multiplier := math.Pow(2, float64(attempt))
	wait := time.Duration(float64(base) * multiplier)
	if wait > max && max > 0 {
		wait = max
	}
	jitter := 0.2 + (rand.Float64() * 0.6)
	return time.Duration(float64(wait) * jitter)
}

func sleepWithContext(ctx context.Context, d time.Duration) bool {
	if d <= 0 {
		return true
	}
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}

type rateLimiter struct {
	interval time.Duration
	mu       sync.Mutex
	next     time.Time
}

func newRateLimiter(rps int) *rateLimiter {
	if rps <= 0 {
		rps = model.DefaultHHRPS
	}
	return &rateLimiter{
		interval: time.Second / time.Duration(rps),
	}
}

func (l *rateLimiter) Wait(ctx context.Context) error {
	l.mu.Lock()
	defer l.mu.Unlock()

	now := time.Now()
	if l.next.IsZero() || now.After(l.next) {
		l.next = now
	}
	wait := l.next.Sub(now)
	l.next = l.next.Add(l.interval)
	if wait <= 0 {
		return nil
	}
	timer := time.NewTimer(wait)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
