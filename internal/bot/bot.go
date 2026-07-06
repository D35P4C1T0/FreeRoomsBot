package bot

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	tele "gopkg.in/telebot.v3"
)

// App owns the Telegram client, persistence layer, room loader, and cached
// department state for the bot process.
//
// Department room data is refreshed by background goroutines and guarded by
// departMu before it is exposed to handlers.
type App struct {
	cfg         Config
	bot         *tele.Bot
	me          *tele.User
	db          *DatabaseService
	rooms       *RoomsService
	departments []*Department
	departMu    sync.RWMutex
	logger      *slog.Logger
	loc         *time.Location
}

// NewApp initializes all external dependencies needed to run the bot.
//
// It validates the Telegram token through telebot construction, opens MongoDB,
// prepares the room service, and loads the Rome time zone. If initialization
// fails after MongoDB is opened, the connection is closed before returning.
func NewApp(ctx context.Context, cfg Config) (*App, error) {
	logger := NewLogger(cfg)
	bot, err := tele.NewBot(tele.Settings{
		Token: cfg.Bot.BotToken,
		Poller: &tele.LongPoller{
			Timeout: 60 * time.Second,
		},
		Client: &http.Client{Timeout: time.Minute},
		OnError: func(err error, c tele.Context) {
			if isIgnoredTelegramError(err) {
				return
			}
			logger.Error("telegram handler failed", "error", err)
		},
	})
	if err != nil {
		return nil, err
	}
	db, err := NewDatabaseService(ctx, cfg.Database.ConnectionString, time.Duration(cfg.Database.LogRetentionDays)*24*time.Hour)
	if err != nil {
		return nil, err
	}
	success := false
	defer func() {
		if !success {
			closeCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			if err := db.Close(closeCtx); err != nil {
				logger.Warn("database close failed after app initialization error", "error", err)
			}
		}
	}()

	rooms, err := NewRoomsService()
	if err != nil {
		return nil, err
	}
	loc, err := time.LoadLocation("Europe/Rome")
	if err != nil {
		return nil, err
	}
	success = true
	return &App{
		cfg:         cfg,
		bot:         bot,
		me:          bot.Me,
		db:          db,
		rooms:       rooms,
		departments: Departments(),
		logger:      logger,
		loc:         loc,
	}, nil
}

// Run refreshes room data, registers handlers, and starts Telegram long
// polling until ctx is canceled or the bot stops.
//
// On context cancellation, Run stops the telebot poller and waits for it to
// exit before returning ctx.Err().
func (a *App) Run(ctx context.Context) error {
	a.logger.Debug("bot authenticated", "username", a.me.Username)
	if err := a.RefreshRooms(ctx); err != nil {
		a.logger.Error("initial refresh failed", "error", err)
	}
	go a.ScheduleRefresh(ctx)

	a.RegisterHandlers(ctx)

	done := make(chan struct{})
	go func() {
		a.bot.Start()
		close(done)
	}()

	select {
	case <-ctx.Done():
		a.bot.Stop()
		<-done
		return ctx.Err()
	case <-done:
		return nil
	}
}

// Close releases resources owned by App.
//
// The supplied context controls the deadline for disconnecting from MongoDB.
func (a *App) Close(ctx context.Context) error {
	return a.db.Close(ctx)
}

// ScheduleRefresh refreshes all departments on the next UTC hour boundary and
// every UTC hour after that until ctx is canceled.
func (a *App) ScheduleRefresh(ctx context.Context) {
	for {
		now := time.Now().UTC()
		next := now.Truncate(time.Hour).Add(time.Hour)
		timer := time.NewTimer(time.Until(next))
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
			if err := a.RefreshRooms(ctx); err != nil {
				a.logger.Error("refresh failed", "error", err)
			}
		}
	}
}

// RefreshRooms reloads room data for every configured department in parallel.
//
// Individual department failures are logged and do not stop other refreshes.
// The method currently returns nil after all refresh goroutines finish.
func (a *App) RefreshRooms(ctx context.Context) error {
	var wg sync.WaitGroup
	for _, dep := range a.departments {
		dep := dep
		wg.Add(1)
		go func() {
			defer wg.Done()
			a.refreshDepartment(ctx, dep)
		}()
	}
	wg.Wait()
	a.logger.Debug("room refresh complete")
	return nil
}

func (a *App) refreshDepartment(ctx context.Context, dep *Department) {
	a.logger.Debug("refreshing department rooms", "id", dep.ID, "name", dep.Name)
	depRequest := *dep
	rooms, err := a.rooms.LoadRooms(ctx, &depRequest)
	if err != nil {
		a.logger.Error("department refresh failed", "id", dep.ID, "name", dep.Name, "error", err)
		return
	}
	// Publish refreshed state atomically so handlers never observe a partially
	// updated department during concurrent refreshes.
	a.departMu.Lock()
	dep.Rooms = rooms
	dep.UpdatedAt = depRequest.UpdatedAt
	a.departMu.Unlock()
}

// RegisterHandlers attaches all Telegram handlers to the underlying bot.
//
// Handlers use ctx for database writes and room request handling. Calling
// RegisterHandlers more than once registers duplicate handlers.
func (a *App) RegisterHandlers(ctx context.Context) {
	a.bot.Handle(tele.OnText, func(c tele.Context) error {
		if err := a.logMessageChat(ctx, c); err != nil {
			return err
		}
		return a.HandleTextMessage(ctx, c.Message())
	})

	a.bot.Handle(tele.OnAddedToGroup, func(c tele.Context) error {
		if err := a.logMessageChat(ctx, c); err != nil {
			return err
		}
		return a.HandleAddedToGroup(c.Message())
	})

	a.bot.Handle(tele.OnCallback, func(c tele.Context) error {
		callback := c.Callback()
		if err := a.db.LogUser(ctx, callback.Sender); err != nil {
			a.logger.Warn("log user failed", "error", err)
		}
		return a.HandleCallbackQuery(ctx, callback)
	})

	a.registerLogOnlyMessageHandlers(ctx)
}

func (a *App) registerLogOnlyMessageHandlers(ctx context.Context) {
	logOnlyEndpoints := []interface{}{
		tele.OnPhoto,
		tele.OnAudio,
		tele.OnAnimation,
		tele.OnDocument,
		tele.OnSticker,
		tele.OnVideo,
		tele.OnVoice,
		tele.OnVideoNote,
		tele.OnContact,
		tele.OnLocation,
		tele.OnVenue,
		tele.OnDice,
		tele.OnInvoice,
		tele.OnPayment,
		tele.OnGame,
		tele.OnPoll,
		tele.OnPinned,
		tele.OnTopicCreated,
		tele.OnTopicReopened,
		tele.OnTopicClosed,
		tele.OnTopicEdited,
		tele.OnGeneralTopicHidden,
		tele.OnGeneralTopicUnhidden,
		tele.OnWriteAccessAllowed,
		tele.OnUserJoined,
		tele.OnUserLeft,
		tele.OnUserShared,
		tele.OnChatShared,
		tele.OnNewGroupTitle,
		tele.OnNewGroupPhoto,
		tele.OnGroupPhotoDeleted,
		tele.OnGroupCreated,
		tele.OnSuperGroupCreated,
		tele.OnChannelCreated,
		tele.OnMigration,
		tele.OnVideoChatStarted,
		tele.OnVideoChatEnded,
		tele.OnVideoChatParticipants,
		tele.OnVideoChatScheduled,
		tele.OnWebApp,
		tele.OnProximityAlert,
		tele.OnAutoDeleteTimer,
	}

	for _, endpoint := range logOnlyEndpoints {
		a.bot.Handle(endpoint, func(c tele.Context) error {
			return a.logMessageChat(ctx, c)
		})
	}
}

func (a *App) logMessageChat(ctx context.Context, c tele.Context) error {
	message := c.Message()
	if message == nil {
		return nil
	}
	if err := a.db.LogChat(ctx, message.Chat); err != nil {
		a.logger.Warn("log chat failed", "error", err)
	}
	return nil
}

// HandleAddedToGroup sends the startup message when the bot itself is added to
// a group or supergroup.
func (a *App) HandleAddedToGroup(message *tele.Message) error {
	if message.GroupCreated || message.SuperGroupCreated {
		return a.SendStart(message.Chat.ID)
	}
	for _, user := range message.UsersJoined {
		if user.Username == a.me.Username {
			return a.SendStart(message.Chat.ID)
		}
	}
	return nil
}

// HandleCallbackQuery handles room availability callback buttons.
//
// Unknown callback data, missing messages, and unknown departments are ignored
// without error because Telegram callbacks can outlive local bot state.
func (a *App) HandleCallbackQuery(ctx context.Context, query *tele.Callback) error {
	if query.Message == nil {
		return nil
	}
	a.logger.Debug("callback query", "chat_id", query.Message.Chat.ID, "data", query.Data)

	data := strings.Split(query.Data, ";")
	if len(data) != 3 || data[0] != "free" {
		return nil
	}
	dep := a.findDepartment(data[1])
	if dep == nil {
		return nil
	}

	var availability AvailabilityType
	switch data[2] {
	case "now":
		availability = AvailabilityFree
	case "future":
		availability = AvailabilityOccupied
	case "all":
		availability = AvailabilityAny
	default:
		return nil
	}

	return a.HandleRoomRequest(ctx, query.Message, dep, availability, query)
}

// HandleTextMessage handles commands and department-name requests from text
// messages.
func (a *App) HandleTextMessage(ctx context.Context, message *tele.Message) error {
	text := a.normalizeMessageText(message)
	a.logger.Debug("text message", "chat_id", message.Chat.ID, "text", text)

	if isCommand(text, "start", a.me.Username) {
		return a.SendStart(message.Chat.ID)
	}
	if isCommand(text, "aiuto", a.me.Username) {
		return a.SendHelp(message.Chat.ID)
	}

	for _, dep := range a.departments {
		if strings.Contains(strings.ToLower(text), dep.Slug) {
			return a.HandleRoomRequest(ctx, message, dep, AvailabilityFree, nil)
		}
	}
	return a.SendHelp(message.Chat.ID)
}

func (a *App) normalizeMessageText(message *tele.Message) string {
	text := message.Text
	if message.FromGroup() && a.me != nil {
		text = strings.ReplaceAll(text, "@"+a.me.Username, "")
	}
	return strings.TrimSpace(text)
}

func isCommand(text, command, botUsername string) bool {
	base := "/" + command
	if text == base || strings.HasPrefix(text, base+" ") {
		return true
	}
	if botUsername == "" {
		return false
	}
	mentioned := base + "@" + botUsername
	return text == mentioned || strings.HasPrefix(text, mentioned+" ")
}

// HandleRoomRequest records usage and sends or edits the room availability
// response for dep.
//
// Logging failures are reported to the application logger but do not prevent a
// user-facing response.
func (a *App) HandleRoomRequest(ctx context.Context, message *tele.Message, dep *Department, availability AvailabilityType, query *tele.Callback) error {
	chatID := message.Chat.ID
	requestType := RequestMessage
	if query != nil {
		requestType = RequestCallbackQuery
	}
	if err := a.db.LogUsage(ctx, chatID, requestType, availability, dep); err != nil {
		a.logger.Warn("log usage failed", "error", err)
	}

	depSnapshot := a.departmentSnapshot(dep)
	return a.SendRooms(message, depSnapshot, availability, query)
}

func (a *App) departmentSnapshot(dep *Department) *Department {
	a.departMu.RLock()
	defer a.departMu.RUnlock()

	// The Room values themselves are immutable after publication, so a shallow
	// slice copy is enough to decouple handlers from future slice replacement.
	rooms := make([]*Room, len(dep.Rooms))
	copy(rooms, dep.Rooms)
	return &Department{
		ID:        dep.ID,
		Name:      dep.Name,
		Slug:      dep.Slug,
		Rooms:     rooms,
		UpdatedAt: dep.UpdatedAt,
	}
}

func (a *App) findDepartment(slug string) *Department {
	for _, dep := range a.departments {
		if dep.Slug == slug {
			return dep
		}
	}
	return nil
}

// SendStart sends the introductory Markdown message to chatID.
func (a *App) SendStart(chatID int64) error {
	msg := renderStartMessage(a.cfg.Bot.BotName, a.departments)
	_, err := a.bot.Send(tele.ChatID(chatID), msg, tele.ModeMarkdown)
	return err
}

func renderStartMessage(botName string, departments []*Department) string {
	var msg strings.Builder
	msg.WriteString("Ciao! 🤓\n\n")
	msg.WriteString(fmt.Sprintf("Sono *%s* e ti posso aiutare a trovare le aule libere presso i poli dell'Università di Trento 🎓\n\n", botName))
	msg.WriteString("Usa uno di questi comandi per ottenere la lista delle aule libere:\n\n")
	for _, dep := range departments {
		msg.WriteString("/")
		msg.WriteString(dep.Slug)
		msg.WriteString("\n")
	}
	msg.WriteString("\nAltre info in /aiuto\n")
	return msg.String()
}

// SendHelp sends the help Markdown message to chatID.
func (a *App) SendHelp(chatID int64) error {
	msg := renderHelpMessage(a.cfg.Bot.BotName, a.departments)
	_, err := a.bot.Send(tele.ChatID(chatID), msg, tele.ModeMarkdown, tele.NoPreview)
	return err
}

func renderHelpMessage(botName string, departments []*Department) string {
	var msg strings.Builder
	msg.WriteString(fmt.Sprintf("*%s* è il bot per controllare la disponibilità delle aule presso i poli dell'Università di Trento 🎓\n\n", botName))
	msg.WriteString("👉 *Usa uno di questi comandi per ottenere la lista delle aule libere*\n\n")
	for _, dep := range departments {
		msg.WriteString("/")
		msg.WriteString(dep.Slug)
		msg.WriteString("\n")
	}
	msg.WriteString("\n🤫 Autore: @kirbychan\n")
	return msg.String()
}
