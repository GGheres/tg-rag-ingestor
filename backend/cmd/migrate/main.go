package main

import (
	"context"
	"log"

	"github.com/joho/godotenv"

	"tg-rag-ingestor/backend/internal/config"
	"tg-rag-ingestor/backend/internal/db"
)

func main() {
	_ = godotenv.Load()
	cfg := config.Load()

	pool, err := db.NewPool(cfg.PostgresDSN)
	if err != nil {
		log.Fatal(err)
	}
	defer pool.Close()

	if err := db.RunMigrations(context.Background(), pool, cfg.MigrationDir); err != nil {
		log.Fatal(err)
	}

	log.Printf("migrations applied from %s", cfg.MigrationDir)
}
