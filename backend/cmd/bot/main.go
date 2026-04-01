package main

import (
	"context"
	"errors"
	"log"

	"github.com/joho/godotenv"

	"tg-rag-ingestor/backend/internal/app"
	"tg-rag-ingestor/backend/internal/config"
	"tg-rag-ingestor/backend/internal/tgbot"
)

func main() {
	_ = godotenv.Load()

	cfg := config.Load()
	application, err := app.New(context.Background(), cfg)
	if err != nil {
		log.Fatal(err)
	}
	defer application.Close()

	bot, err := tgbot.New(tgbot.Config{
		Token:          cfg.TelegramBotToken,
		AllowedUserIDs: cfg.TelegramBotAllowedUserIDs,
	}, application.Repository, application.IngestionService, application.YouTubeService, application.ExportService, application.FileScanService, application.HHConfig, application.Logger)
	if err != nil {
		log.Fatal(err)
	}

	log.Printf("telegram bot started")
	if err := bot.Run(context.Background()); err != nil && !errors.Is(err, context.Canceled) {
		log.Fatal(err)
	}
}
