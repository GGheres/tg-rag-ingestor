package config

import "os"

type Config struct {
	AppPort              string
	PostgresDSN          string
	RedisAddr            string
	QdrantURL            string
	CORSAllowedOrigin    string
	ExportDir            string
	MigrationDir         string
	DefaultSyncBatchSize int
	TelegramMode         string
	TelegramAPIID        string
	TelegramAPIHash      string
	TelegramPhone        string
	TelegramSessionFile  string
	TelegramPassword     string
	TelegramAuthCode     string
}

func Load() Config {
	return Config{
		AppPort:              getEnv("APP_PORT", "8080"),
		PostgresDSN:          getEnv("POSTGRES_DSN", ""),
		RedisAddr:            getEnv("REDIS_ADDR", "localhost:6379"),
		QdrantURL:            getEnv("QDRANT_URL", "http://localhost:6333"),
		CORSAllowedOrigin:    getEnv("CORS_ALLOWED_ORIGIN", "http://localhost:5173"),
		ExportDir:            getEnv("EXPORT_DIR", "./data/exports"),
		MigrationDir:         getEnv("MIGRATION_DIR", "./migrations"),
		DefaultSyncBatchSize: getEnvInt("DEFAULT_SYNC_BATCH_SIZE", 100),
		TelegramMode:         getEnv("TELEGRAM_MODE", "stub"),
		TelegramAPIID:        getEnv("TELEGRAM_API_ID", ""),
		TelegramAPIHash:      getEnv("TELEGRAM_API_HASH", ""),
		TelegramPhone:        getEnv("TELEGRAM_PHONE", ""),
		TelegramSessionFile:  getEnv("TELEGRAM_SESSION_FILE", "./data/tg.session.json"),
		TelegramPassword:     getEnv("TELEGRAM_PASSWORD", ""),
		TelegramAuthCode:     getEnv("TELEGRAM_AUTH_CODE", ""),
	}
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
