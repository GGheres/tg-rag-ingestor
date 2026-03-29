package main

import (
	"context"
	"log"
	"net/http"

	"github.com/joho/godotenv"

	"tg-rag-ingestor/backend/internal/api"
	"tg-rag-ingestor/backend/internal/app"
	"tg-rag-ingestor/backend/internal/config"
)

func main() {
	_ = godotenv.Load()
	cfg := config.Load()
	application, err := app.New(context.Background(), cfg)
	if err != nil {
		log.Fatal(err)
	}
	defer application.Close()

	router := api.NewRouter(
		application.Repository,
		application.IngestionService,
		application.YouTubeService,
		application.ExportService,
		application.FileScanService,
		application.HHConfig,
		application.Logger,
		cfg.CORSAllowedOrigin,
	)

	log.Printf("api listening on :%s", cfg.AppPort)
	log.Fatal(http.ListenAndServe(":"+cfg.AppPort, router))
}
