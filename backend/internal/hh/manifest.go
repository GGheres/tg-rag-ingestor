package hh

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// WriteManifest writes the processing manifest to manifest.json.
func WriteManifest(m *Manifest, outputDir string) (string, error) {
	m.CreatedAt = time.Now()

	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return "", fmt.Errorf("marshal manifest: %w", err)
	}

	path := filepath.Join(outputDir, "manifest.json")
	if err := os.WriteFile(path, data, 0644); err != nil {
		return "", fmt.Errorf("write manifest: %w", err)
	}

	return path, nil
}
