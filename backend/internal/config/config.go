package config

import (
	"fmt"
	"os"
	"strings"
)

type Config struct {
	AppPort                  string
	PostgresDSN              string
	RedisAddr                string
	QdrantURL                string
	CORSAllowedOrigin        string
	ExportDir                string
	FileScanExtractDir       string
	FileScanMaxFiles         int
	FileScanMaxArchiveDepth  int
	MigrationDir             string
	DefaultSyncBatchSize     int
	PyYouTubeAudioServiceURL string
	TelegramMode             string
	TelegramAPIID            string
	TelegramAPIHash          string
	TelegramPhone            string
	TelegramSessionFile      string
	TelegramPassword         string
	TelegramAuthCode         string
	HHClientID               string
	HHClientSecret           string
	HHAccessToken            string
	HHRefreshToken           string
	HHRedirectURI            string
	HHUserAgent              string
	HHOutputDir              string
	HHBaseURL                string
	HHTokenURL               string
	HHAuthURL                string
	HHRequestTimeoutSec      int
	HHRateLimitPerSecond     int
	HHRetryMax               int
	HHRetryBaseBackoffMS     int
	HHRetryMaxBackoffMS      int
	HHConcurrency            int
	HHSaveOriginalsDefault   bool
}

func Load() Config {
	return Config{
		AppPort:                  getEnv("APP_PORT", "8080"),
		PostgresDSN:              getEnv("POSTGRES_DSN", ""),
		RedisAddr:                getEnv("REDIS_ADDR", "localhost:6379"),
		QdrantURL:                getEnv("QDRANT_URL", "http://localhost:6333"),
		CORSAllowedOrigin:        getEnv("CORS_ALLOWED_ORIGIN", "http://localhost:5173"),
		ExportDir:                getEnv("EXPORT_DIR", "./data/exports"),
		FileScanExtractDir:       getEnv("FILESCAN_EXTRACT_DIR", "./data/filescan_extracts"),
		FileScanMaxFiles:         getEnvInt("FILESCAN_MAX_FILES", 20000),
		FileScanMaxArchiveDepth:  getEnvInt("FILESCAN_MAX_ARCHIVE_DEPTH", 4),
		MigrationDir:             getEnv("MIGRATION_DIR", "./migrations"),
		DefaultSyncBatchSize:     getEnvInt("DEFAULT_SYNC_BATCH_SIZE", 100),
		PyYouTubeAudioServiceURL: getEnv("PY_YOUTUBE_AUDIO_SERVICE_URL", getEnv("PY_TRANSCRIPT_SERVICE_URL", "http://localhost:8090")),
		TelegramMode:             getEnv("TELEGRAM_MODE", ""),
		TelegramAPIID:            getEnv("TELEGRAM_API_ID", ""),
		TelegramAPIHash:          getEnv("TELEGRAM_API_HASH", ""),
		TelegramPhone:            getEnv("TELEGRAM_PHONE", ""),
		TelegramSessionFile:      getEnv("TELEGRAM_SESSION_FILE", "./data/tg.session.json"),
		TelegramPassword:         getEnv("TELEGRAM_PASSWORD", ""),
		TelegramAuthCode:         getEnv("TELEGRAM_AUTH_CODE", ""),
		HHClientID:               getEnv("HH_CLIENT_ID", ""),
		HHClientSecret:           getEnv("HH_CLIENT_SECRET", ""),
		HHAccessToken:            getEnv("HH_ACCESS_TOKEN", ""),
		HHRefreshToken:           getEnv("HH_REFRESH_TOKEN", ""),
		HHRedirectURI:            getEnv("HH_REDIRECT_URI", "https://localhost/callback"),
		HHUserAgent:              getEnv("HH_USER_AGENT", ""),
		HHOutputDir:              getEnv("HH_OUTPUT_DIR", "./data/hh_extractions"),
		HHBaseURL:                getEnv("HH_BASE_URL", "https://api.hh.ru"),
		HHTokenURL:               getEnv("HH_TOKEN_URL", "https://api.hh.ru/token"),
		HHAuthURL:                getEnv("HH_AUTH_URL", "https://hh.ru/oauth/authorize"),
		HHRequestTimeoutSec:      getEnvInt("HH_REQUEST_TIMEOUT_SEC", 30),
		HHRateLimitPerSecond:     getEnvInt("HH_RATE_LIMIT_RPS", 5),
		HHRetryMax:               getEnvInt("HH_RETRY_MAX", 4),
		HHRetryBaseBackoffMS:     getEnvInt("HH_RETRY_BASE_BACKOFF_MS", 400),
		HHRetryMaxBackoffMS:      getEnvInt("HH_RETRY_MAX_BACKOFF_MS", 15000),
		HHConcurrency:            getEnvInt("HH_CONCURRENCY", 4),
		HHSaveOriginalsDefault:   getEnvBool("HH_SAVE_ORIGINALS_DEFAULT", true),
	}
}

// ValidateAppRuntime performs fail-fast validation for API/worker runtime configuration.
func (c Config) ValidateAppRuntime() error {
	missing := make([]string, 0)
	if strings.TrimSpace(c.PostgresDSN) == "" {
		missing = append(missing, "POSTGRES_DSN")
	}
	if len(missing) > 0 {
		return fmt.Errorf("missing required config: %s", strings.Join(missing, ", "))
	}
	return c.ValidateTelegramRuntime()
}

// ValidateTelegramRuntime performs fail-fast validation for Telegram collector integration.
func (c Config) ValidateTelegramRuntime() error {
	missing := make([]string, 0)
	mode := strings.ToLower(strings.TrimSpace(c.TelegramMode))
	if mode == "" {
		missing = append(missing, "TELEGRAM_MODE")
	} else if mode != "mtproto" {
		return fmt.Errorf("unsupported TELEGRAM_MODE=%q: only mtproto is allowed", c.TelegramMode)
	}

	if mode == "mtproto" {
		if strings.TrimSpace(c.TelegramAPIID) == "" {
			missing = append(missing, "TELEGRAM_API_ID")
		}
		if strings.TrimSpace(c.TelegramAPIHash) == "" {
			missing = append(missing, "TELEGRAM_API_HASH")
		}
	}

	if len(missing) > 0 {
		return fmt.Errorf("missing required config: %s", strings.Join(missing, ", "))
	}
	return nil
}

func getEnv(key, fallback string) string {
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}
	return v
}

func getEnvInt(key string, fallback int) int {
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}

	parsed, ok := atoi(v)
	if !ok {
		return fallback
	}
	return parsed
}

func atoi(v string) (int, bool) {
	n := 0
	if v == "" {
		return 0, false
	}
	for _, r := range v {
		if r < '0' || r > '9' {
			return 0, false
		}
		n = (n * 10) + int(r-'0')
	}
	return n, true
}

func getEnvBool(key string, fallback bool) bool {
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}
	switch v {
	case "1", "true", "TRUE", "yes", "YES":
		return true
	case "0", "false", "FALSE", "no", "NO":
		return false
	default:
		return fallback
	}
}
