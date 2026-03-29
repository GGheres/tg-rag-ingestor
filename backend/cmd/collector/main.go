package main

import (
	"context"
	"fmt"
	"log"
	"strings"

	"github.com/joho/godotenv"

	"tg-rag-ingestor/backend/internal/config"
	"tg-rag-ingestor/backend/internal/telegram"
)

func main() {
	_ = godotenv.Load()
	cfg := config.Load()
	if err := cfg.ValidateTelegramRuntime(); err != nil {
		log.Fatal(err)
	}
	if strings.ToLower(strings.TrimSpace(cfg.TelegramMode)) != "mtproto" {
		log.Fatal("TELEGRAM_MODE must be mtproto for collector command")
	}

	collector := telegram.NewMTProtoCollector(
		cfg.TelegramAPIID,
		cfg.TelegramAPIHash,
		cfg.TelegramPhone,
		cfg.TelegramSessionFile,
		cfg.TelegramPassword,
		cfg.TelegramAuthCode,
	)

	channel, err := collector.ResolveChannel(context.Background(), telegram.ResolveInput{
		Username: "durov",
	})
	if err != nil {
		log.Fatal(err)
	}

	page, err := collector.FetchChannelHistory(context.Background(), channel, telegram.FetchOptions{
		Limit: 5,
	})
	if err != nil {
		log.Fatal(err)
	}

	fmt.Printf("fetched: %d messages\n", len(page.Messages))
	if page.NextCursor != nil {
		fmt.Printf("next_cursor: %s\n", *page.NextCursor)
	}
}
