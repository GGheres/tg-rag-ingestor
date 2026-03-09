package main

import (
	"context"
	"log"

	"github.com/joho/godotenv"

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

	log.Println("worker skeleton is ready; sync jobs are currently executed directly via API endpoint")
}
