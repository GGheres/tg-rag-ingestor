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
	"tg-rag-ingestor/backend/internal/ingestion"
	"tg-rag-ingestor/backend/internal/processing"
	"tg-rag-ingestor/backend/internal/storage"
	"tg-rag-ingestor/backend/internal/telegram"
)

type App struct {
	Config           config.Config
	Repository       *storage.Repository
	IngestionService *ingestion.Service
	ExportService    *export.Service
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
	exportService := export.NewService(repo, cfg.ExportDir)

	return &App{
		Config:           cfg,
		Repository:       repo,
		IngestionService: ingestionService,
		ExportService:    exportService,
		Logger:           logger,
		Close:            pool.Close,
	}, nil
}
