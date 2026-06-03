// Package main starts the UNITN free classrooms Telegram bot.
package main

import (
	"context"
	"errors"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"free-classrooms-bot-unitn/internal/bot"

	_ "time/tzdata"
)

func main() {
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
