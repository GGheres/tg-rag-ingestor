package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/joho/godotenv"

	"tg-rag-ingestor/backend/internal/hhtokens"
	"tg-rag-ingestor/backend/internal/model"
	hhservice "tg-rag-ingestor/backend/internal/service"
)

func main() {
	vacancyID := flag.String("vacancy", "", "HH vacancy ID (required)")
	outputDir := flag.String("output", "", "Output directory (default from HH_OUTPUT_DIR)")
	dryRun := flag.Bool("dry-run", false, "Run without downloading resumes")
	exportPDF := flag.Bool("pdf", false, "Export combined PDF (requires Chrome/Chromium)")
	saveOriginals := flag.Bool("save-originals", true, "Download resume original files (pdf/rtf)")
	authCode := flag.String("auth-code", "", "OAuth2 authorization code exchange mode")
	flag.Parse()

	_ = godotenv.Load()
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))

	cfg := model.HHConfig{
		ClientID:     os.Getenv("HH_CLIENT_ID"),
		ClientSecret: os.Getenv("HH_CLIENT_SECRET"),
		AccessToken:  os.Getenv("HH_ACCESS_TOKEN"),
		RefreshToken: os.Getenv("HH_REFRESH_TOKEN"),
		RedirectURI:  envOr("HH_REDIRECT_URI", "https://localhost/callback"),
		UserAgent:    os.Getenv("HH_USER_AGENT"),
		OutputDir:    envOr("HH_OUTPUT_DIR", model.DefaultHHOutputDir),
		BaseURL:      envOr("HH_BASE_URL", model.DefaultHHBaseURL),
		TokenURL:     envOr("HH_TOKEN_URL", model.DefaultHHTokenURL),
		AuthURL:      envOr("HH_AUTH_URL", model.DefaultHHAuthURL),
	}
	cfg.OnTokenRefresh = func(ctx context.Context, token model.TokenResponse) error {
		envPath, err := hhtokens.PersistHHTokens(token.AccessToken, token.RefreshToken)
		if err != nil {
			return err
		}
		logger.Info("hh refreshed oauth tokens persisted", "env_path", envPath)
		return nil
	}

	if cfg.ClientID == "" || cfg.ClientSecret == "" || cfg.UserAgent == "" {
		fatalf("HH_CLIENT_ID, HH_CLIENT_SECRET and HH_USER_AGENT must be set")
	}

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer cancel()

	svc := hhservice.NewHHExtractionService(cfg, model.OSFileSystem{}, logger)
	if *authCode != "" {
		token, err := svc.ExchangeCode(ctx, *authCode)
		if err != nil {
			fatalf("oauth exchange failed: %v", err)
		}
		envPath, err := hhtokens.PersistHHTokens(token.AccessToken, token.RefreshToken)
		if err != nil {
			fmt.Println("Could not save tokens to .env automatically. Add these values manually:")
			fmt.Printf("HH_ACCESS_TOKEN=%s\n", token.AccessToken)
			fmt.Printf("HH_REFRESH_TOKEN=%s\n", token.RefreshToken)
			fatalf("persist oauth tokens failed: %v", err)
		}
		fmt.Printf("HH OAuth tokens saved to %s\n", envPath)
		return
	}

	if cfg.AccessToken == "" && cfg.RefreshToken == "" {
		fmt.Printf("No HH token configured.\nOpen URL and obtain ?code=...\n%s\n", svc.AuthorizationURL())
		fmt.Println("Then run: hhresumes --auth-code=<CODE> --vacancy=<VACANCY_ID>")
		os.Exit(1)
	}

	if *vacancyID == "" {
		fatalf("--vacancy is required")
	}

	targetDir := *outputDir
	if targetDir == "" {
		targetDir = filepath.Join(cfg.OutputDir, fmt.Sprintf("%s_%d", *vacancyID, time.Now().Unix()))
	}
	request := model.ExtractionRequest{
		VacancyID:     *vacancyID,
		DryRun:        *dryRun,
		ExportPDF:     *exportPDF,
		SaveOriginals: *saveOriginals,
		OutputDir:     targetDir,
	}
	manifest, files, err := svc.Run(ctx, request)
	if err != nil {
		fatalf("extraction failed: %v", err)
	}

	fmt.Printf("Extraction completed: vacancy=%s processed=%d success=%d failed=%d dry_run=%v\n",
		manifest.VacancyID, manifest.Processed, manifest.Succeeded, manifest.Failed, manifest.DryRun)
	for _, file := range files {
		fmt.Printf("- %s\n", file)
	}
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func fatalf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, format+"\n", args...)
	os.Exit(1)
}
