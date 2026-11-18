// Package main starts the UNITN free classrooms Telegram bot.
package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"
	_ "time/tzdata"

	"free-classrooms-bot-unitn/internal/bot"
	"free-classrooms-bot-unitn/internal/rooms"
	"free-classrooms-bot-unitn/internal/usage"
)

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}

func run() error {
	cfg, err := loadConfig()
	if err != nil {
		return err
	}
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "healthcheck":
			return runHealthcheck(cfg.Health.Port)
		case "usage":
			query := ""
			if len(os.Args) > 2 {
				query = os.Args[2]
			}
			return usage.Export(context.Background(), cfg.Health.Port, query, os.Stdout)
		}
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	logger := newLogger(cfg)
	roomClient, err := rooms.NewClient()
	if err != nil {
		return err
	}
	app, err := bot.New(cfg.Bot.BotToken, cfg.Bot.BotName, roomClient, logger)
	if err != nil {
		return err
	}
	retention := time.Duration(cfg.Database.LogRetentionDays) * 24 * time.Hour
	store, err := usage.Open(ctx, cfg.Database.ConnectionString, retention)
	if err != nil {
		return err
	}
	defer func() {
		closeCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := store.Close(closeCtx); err != nil {
			log.Printf("database close failed: %v", err)
		}
	}()
	if err := startHealthServer(ctx, cfg.Health.Port, logger, store.Handler()); err != nil {
		return err
	}
	app.Run(ctx, store.RecordInteraction)
	return nil
}

func runHealthcheck(port int) error {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	if err := checkHealth(ctx, port); err != nil {
		return fmt.Errorf("healthcheck failed: %w", err)
	}
	return nil
}
