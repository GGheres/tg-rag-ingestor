package app

import (
	"context"
	"fmt"
	"log/slog"
	"os"

	"tg-rag-ingestor/backend/internal/chunking"
	"tg-rag-ingestor/backend/internal/config"
	"tg-rag-ingestor/backend/internal/db"
	"tg-rag-ingestor/backend/internal/export"
	"tg-rag-ingestor/backend/internal/filescan"
	"tg-rag-ingestor/backend/internal/ingestion"
	"tg-rag-ingestor/backend/internal/processing"
	"tg-rag-ingestor/backend/internal/storage"
	"tg-rag-ingestor/backend/internal/telegram"
	"tg-rag-ingestor/backend/internal/youtube"
)

type App struct {
	Config           config.Config
	Repository       *storage.Repository
	IngestionService *ingestion.Service
	YouTubeService   *youtube.Service
	ExportService    *export.Service
	FileScanService  *filescan.Service
	Logger           *slog.Logger
	Close            func()
}

func New(ctx context.Context, cfg config.Config) (*App, error) {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))

	pool, err := db.NewPool(cfg.PostgresDSN)
	if err != nil {
		return nil, fmt.Errorf("create postgres pool: %w", err)
	}

	if err := db.RunMigrations(ctx, pool, cfg.MigrationDir); err != nil {
		pool.Close()
		return nil, fmt.Errorf("run migrations: %w", err)
	}

	repo := storage.NewRepository(pool)

	var collector telegram.Collector
	if cfg.TelegramMode == "mtproto" {
		collector = telegram.NewMTProtoCollector(
			cfg.TelegramAPIID,
			cfg.TelegramAPIHash,
			cfg.TelegramPhone,
			cfg.TelegramSessionFile,
			cfg.TelegramPassword,
			cfg.TelegramAuthCode,
		)
	} else {
		collector = telegram.NewStubCollector()
	}

	processingService := processing.NewService(repo, chunking.DefaultConfig())
	ingestionService := ingestion.NewService(repo, collector, processingService, cfg.DefaultSyncBatchSize)
	audioProvider := youtube.NewAudioClient(cfg.PyYouTubeAudioServiceURL)
	youtubeService := youtube.NewService(repo, audioProvider)
	exportService := export.NewService(repo, cfg.ExportDir)
	fileScanService, err := filescan.NewService(filescan.Config{
		ExtractDir:      cfg.FileScanExtractDir,
		MaxFiles:        cfg.FileScanMaxFiles,
		MaxArchiveDepth: cfg.FileScanMaxArchiveDepth,
	})
	if err != nil {
		pool.Close()
		return nil, fmt.Errorf("create filesystem scan service: %w", err)
	}

	return &App{
		Config:           cfg,
		Repository:       repo,
		IngestionService: ingestionService,
		YouTubeService:   youtubeService,
		ExportService:    exportService,
		FileScanService:  fileScanService,
		Logger:           logger,
		Close:            pool.Close,
	}, nil
}
