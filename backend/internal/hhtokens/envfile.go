package hhtokens

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func FindEnvFilePath() (string, error) {
	if explicit := strings.TrimSpace(os.Getenv("APP_ENV_FILE")); explicit != "" {
		info, err := os.Stat(explicit)
		if err != nil {
			return "", fmt.Errorf("stat APP_ENV_FILE: %w", err)
		}
		if info.IsDir() {
			return "", fmt.Errorf("APP_ENV_FILE points to a directory: %s", explicit)
		}
		return explicit, nil
	}

	wd, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("get working directory: %w", err)
	}

	for dir := wd; ; dir = filepath.Dir(dir) {
		candidate := filepath.Join(dir, ".env")
		info, statErr := os.Stat(candidate)
		if statErr == nil && !info.IsDir() {
			return candidate, nil
		}
		next := filepath.Dir(dir)
		if next == dir {
			break
		}
	}

	return "", fmt.Errorf(".env file not found from %s upwards", wd)
}

func PersistHHTokens(accessToken, refreshToken string) (string, error) {
	envPath, err := FindEnvFilePath()
	if err != nil {
		return "", err
	}
	if err := PersistEnvValues(envPath, map[string]string{
		"HH_ACCESS_TOKEN":  accessToken,
		"HH_REFRESH_TOKEN": refreshToken,
	}); err != nil {
		return "", err
	}
	return envPath, nil
}

func PersistEnvValues(envPath string, updates map[string]string) error {
	data, err := os.ReadFile(envPath)
	if err != nil {
		return fmt.Errorf("read env file: %w", err)
	}

	content := strings.ReplaceAll(string(data), "\r\n", "\n")
	lines := strings.Split(content, "\n")
	seen := make(map[string]bool, len(updates))

	for idx, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		key, _, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		value, shouldUpdate := updates[key]
		if !shouldUpdate {
			continue
		}
		lines[idx] = key + "=" + sanitizeEnvValue(value)
		seen[key] = true
	}

	for key, value := range updates {
		if seen[key] {
			continue
		}
		lines = append(lines, key+"="+sanitizeEnvValue(value))
	}

	output := strings.Join(lines, "\n")
	if !strings.HasSuffix(output, "\n") {
		output += "\n"
	}

	if err := os.WriteFile(envPath, []byte(output), 0o600); err != nil {
		return fmt.Errorf("write env file: %w", err)
	}
	return nil
}

func sanitizeEnvValue(value string) string {
	return strings.ReplaceAll(value, "\n", "")
}
