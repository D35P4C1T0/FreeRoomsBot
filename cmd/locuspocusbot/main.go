// Package main starts the UNITN free classrooms Telegram bot.
package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"free-classrooms-bot-unitn/internal/bot"

	_ "time/tzdata"
)

func main() {
	if len(os.Args) > 1 && os.Args[1] == "healthcheck" {
		if err := runHealthcheck(); err != nil {
			log.Fatal(err)
		}
		return
	}

	cfg, err := bot.LoadConfig()
	if err != nil {
		log.Fatal(err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	app, err := bot.NewApp(ctx, cfg)
	if err != nil {
		log.Fatal(err)
	}
	if _, err := bot.StartHealthServer(ctx, cfg.Health.Port, bot.NewLogger(cfg)); err != nil {
		log.Fatal(err)
	}
	defer func() {
		closeCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := app.Close(closeCtx); err != nil {
			log.Printf("database close failed: %v", err)
		}
	}()

	if err := app.Run(ctx); err != nil && !errors.Is(err, context.Canceled) {
		log.Fatal(err)
	}
}

func runHealthcheck() error {
	cfg, err := bot.LoadConfig()
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	if err := bot.CheckHealth(ctx, cfg.Health.Port); err != nil {
		return fmt.Errorf("healthcheck failed: %w", err)
	}
	return nil
}
