package bot

import (
	"context"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"free-classrooms-bot-unitn/internal/rooms"

	tele "gopkg.in/telebot.v3"
)

type App struct {
	name        string
	bot         *tele.Bot
	roomClient  *rooms.Client
	departments []*rooms.Department
	departMu    sync.RWMutex
	logger      *slog.Logger
}

func New(token, name string, roomClient *rooms.Client, logger *slog.Logger) (*App, error) {
	client, err := tele.NewBot(tele.Settings{
		Token: token,
		Poller: &tele.LongPoller{
			Timeout:        60 * time.Second,
			AllowedUpdates: []string{"message", "callback_query"},
		},
		Client: &http.Client{Timeout: time.Minute},
		OnError: func(err error, _ tele.Context) {
			if !isIgnoredTelegramError(err) {
				logger.Error("telegram handler failed", "error", err)
			}
		},
	})
	if err != nil {
		return nil, err
	}

	return &App{
		name:        name,
		bot:         client,
		roomClient:  roomClient,
		departments: rooms.Departments(),
		logger:      logger,
	}, nil
}

func (a *App) Run(ctx context.Context, record recordInteraction) {
	a.refreshRooms(ctx)
	if ctx.Err() != nil {
		return
	}
	go a.scheduleRefresh(ctx)

	a.registerHandlers(ctx, record)
	go func() {
		<-ctx.Done()
		a.bot.Stop()
	}()
	a.bot.Start()
}
